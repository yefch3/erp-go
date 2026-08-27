// Package pgdb owns PostgreSQL connection setup and the one transaction
// helper every use case goes through. Use-case code never calls Begin or
// Commit directly: the transaction boundary lives in InTx, so it cannot be
// forgotten or half-closed on an error path.
package pgdb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultMaxConns is the pool ceiling when the DSN does not name one.
//
// pgxpool's own default is max(4, NumCPU), which on the two-core hosts this
// deploys to is four. Four is too few for a service that answers requests and
// runs background workers out of the same pool: the mail service polls
// mailboxes on eight workers while employees are reading their inbox, and at
// four connections the two starve each other - the symptom is not an error
// but page loads that stall behind a sync.
//
// Eight is chosen against the *server's* budget, not this process's appetite.
// Every service shares one Postgres, whose max_connections is 100, and there
// are 11 services: 11 x 8 = 88 leaves room for psql and pg_dump. Raising this
// number means raising max_connections with it, or the eleventh service to
// need its ninth connection is the one that fails.
const DefaultMaxConns = 8

// DefaultBusinessTimeZone 是「今天是几号」按哪个时区算。
//
// **服务器跑在 UTC，人在中国。** 不设的话，`current_date` 在北京时间
// 00:00–08:00 这八个小时里返回的是昨天——每天三分之一的时间，系统和用它的人
// 对「今天」的理解差一天。查生产时正好撞在窗口里：数据库说 08-27，北京是 08-28。
//
// 它错在哪不是抽象的：
//
//   - 应收提醒写进记录里的「逾期 N 天」少一天，而且是**存下来**的
//   - 凌晨开的询价单编号带昨天的日期（RFQ-20260827-...）
//   - 凌晨停用的员工，离职日期记成昨天
//   - 今天生效的客户关系，要到早上八点才生效
//
// 为什么在连接上设而不是改每一条 SQL：业务日期的判断几乎全在 SQL 里
// （Go 那边的 time.Now() 基本都是「时间点」——锁过期、超时、耗时——和时区
// 无关）。在连接上设一次，所有 current_date 一起对。
//
// **已存的数据不受影响**：timestamptz 存的是绝对时间，会话时区只影响
// 「怎么把它读成日期」和「怎么打印」。date 类型的列（到期日、付款日）压根
// 没有时区概念。
const DefaultBusinessTimeZone = "Asia/Shanghai"

// New opens a pool and verifies connectivity before returning it.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pgdb: parse dsn: %w", err)
	}
	// 业务时区。BUSINESS_TZ 可以改（换个国家的分公司会需要），DSN 里自己
	// 写了 timezone 的话以 DSN 为准——deployment 比这里的默认值更知道自己
	// 在哪，和 pool_max_conns 一个道理。
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if _, fromDSN := cfg.ConnConfig.RuntimeParams["timezone"]; !fromDSN {
		tz := strings.TrimSpace(os.Getenv("BUSINESS_TZ"))
		if tz == "" {
			tz = DefaultBusinessTimeZone
		}
		cfg.ConnConfig.RuntimeParams["timezone"] = tz
	}
	// Only when the DSN is silent: a deployment that has measured its own
	// service knows better than this default and must be allowed to say so.
	if !strings.Contains(dsn, "pool_max_conns") {
		cfg.MaxConns = DefaultMaxConns
	}
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgdb: create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgdb: ping: %w", err)
	}
	return pool, nil
}

// InTx runs fn inside a transaction: commit on nil, rollback on error or
// panic. The panic is re-raised after rollback so recovery interceptors
// still see it.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgdb: begin: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
	}()
	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgdb: commit: %w", err)
	}
	return nil
}
