package pgdb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 数据库按**业务时区**算「今天」，不是按服务器的 UTC。
//
// 生产上服务器跑 UTC，人在中国。不设的话，北京时间 00:00–08:00 这八个小时里
// `current_date` 返回的是昨天——每天三分之一的时间，系统和用它的人对「今天」
// 的理解差一天。查生产时正好撞在窗口里：数据库说 08-27，北京是 08-28。
func TestPoolUsesBusinessTimeZone(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()

	// 默认：Asia/Shanghai。
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var tz string
	if err := pool.QueryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != pgdb.DefaultBusinessTimeZone {
		t.Fatalf("会话时区该是 %s，实际 %q", pgdb.DefaultBusinessTimeZone, tz)
	}

	// 数据库的「今天」要和业务时区的今天一致。
	//
	// 这一条才是这个测试的理由：**它在 UTC 的服务器上跑，就是在重现那个窗口**。
	// 服务器的 UTC 日期在北京 00:00–08:00 落后一天，所以只要下面这个断言成立，
	// 就说明 current_date 走的是业务时区而不是服务器时区。
	var dbToday time.Time
	if err := pool.QueryRow(ctx, `SELECT current_date`).Scan(&dbToday); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation(pgdb.DefaultBusinessTimeZone)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().In(loc).Format("2006-01-02")
	if got := dbToday.Format("2006-01-02"); got != want {
		t.Fatalf("数据库认为今天是 %s，业务时区的今天是 %s——差一天的那个 bug 又回来了", got, want)
	}
}

// BUSINESS_TZ 能改。换个国家的分公司会需要，而写死就意味着要改代码。
func TestBusinessTimeZoneIsConfigurable(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	t.Setenv("BUSINESS_TZ", "America/Sao_Paulo")
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var tz string
	if err := pool.QueryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "America/Sao_Paulo" {
		t.Fatalf("BUSINESS_TZ 该被认，实际 %q", tz)
	}
}

// DSN 自己写了时区的话以 DSN 为准——部署比这里的默认值更知道自己在哪，
// 和 pool_max_conns 一个道理。
func TestDSNTimeZoneWins(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	t.Setenv("BUSINESS_TZ", "America/Sao_Paulo")
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn+"&timezone=Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var tz string
	if err := pool.QueryRow(ctx, `SELECT current_setting('TimeZone')`).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "Europe/Berlin" {
		t.Fatalf("DSN 里写的时区该压过 BUSINESS_TZ，实际 %q", tz)
	}
}
