// Package idempotency provides consumer-side event dedupe backed by the
// processed_events table each consuming service owns.
//
// 这里的核心是**认领与干活写在同一个事务里**（transactional inbox）。
//
// 它原来不是这样：认领先单独提交，再去干活。那样有个关不掉的窗口——认领已
// 经落库、业务事务还没提交，这期间进程被硬杀（OOM / kill -9 / 宕机），事件
// 就丢了：重投时认领在，于是跳过一件从没干完的活。窗口是整个处理时长，而
// 丢掉的是一笔收货、一份生效的合同。
//
// 写进同一个事务之后，两件事同生共死：崩溃就一起回滚，重投时看不到认领，
// 从头再来；提交成功则两者都在，重投时看到认领，安全跳过。既不丢也不重。
package idempotency

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAlreadyClaimed 表示这条事件在本次事务开始之后被别人认领了。
//
// 只可能发生在消费组重平衡这类两个进程同时处理同一条事件的瞬间。调用方
// 应当让事务回滚——干完的那一方已经把活做了，这一方再做就是记两遍。
var ErrAlreadyClaimed = errors.New("idempotency: event already claimed by another consumer")

// Store satisfies kafkax.Deduper for one consumer group.
type Store struct {
	pool  *pgxpool.Pool
	group string
}

func New(pool *pgxpool.Pool, consumerGroup string) *Store {
	return &Store{pool: pool, group: consumerGroup}
}

// AlreadyProcessed 只读地问一句「这条干过没有」。
//
// 它是快路径：干过就直接提交偏移量，连事务都不用开。真正的把关不在这里，
// 而在 ClaimInTx 的唯一约束上——两个进程同时读到「没干过」时，只有一个
// 的插入能成功。
func (s *Store) AlreadyProcessed(ctx context.Context, dedupeKey string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM processed_events
			WHERE event_id = $1 AND consumer_group = $2
		)`, dedupeKey, s.group).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("idempotency: probe: %w", err)
	}
	return exists, nil
}

// ClaimInTx 在**调用方的事务里**记下认领。
//
// 必须由 handler 在它自己那个干活的事务里调用，这正是这套机制的全部要点：
// 认领和业务写入要么一起提交，要么一起消失。
func (s *Store) ClaimInTx(ctx context.Context, tx pgx.Tx, dedupeKey string) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id, consumer_group)
		VALUES ($1, $2)
		ON CONFLICT (event_id, consumer_group) DO NOTHING`, dedupeKey, s.group)
	if err != nil {
		return fmt.Errorf("idempotency: claim: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyClaimed
	}
	return nil
}

// DDL is embedded verbatim by every consuming service's migrations.
const DDL = `
CREATE TABLE IF NOT EXISTS processed_events (
    event_id       VARCHAR(200) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer_group)
);
`
