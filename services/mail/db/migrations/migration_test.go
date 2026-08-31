package migrations

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestUpDownUp 把邮件库整个拆到版本 0 再装回去。
//
// 这个服务一直没有这条测试（iam / masterdata / shipping / procurement 都有），
// 而它的迁移恰恰是全仓最容易把 Down 写错的：mail_thread_view 那一套是
// 表 + 函数 + 三个触发器，函数改签名必须 DROP 再 CREATE——CREATE OR REPLACE
// 改不了参数列表，只会多出一个重载，然后触发器继续调老的那个，**迁移和
// 测试全绿，表却还按老口径刷新**。Down 里 DROP FUNCTION 又必须写全参数
// 类型，写错了要等到真需要回滚那天才知道。
//
// 必须用独立的一次性库（MAIL_MIGRATION_TEST_DSN）：这里会 DownTo(0)，和
// 邮件业务集成测试共用一个库的话，并发跑时会把对方正在读的表拆掉。
func TestUpDownUp(t *testing.T) {
	dsn := os.Getenv("MAIL_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set MAIL_MIGRATION_TEST_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("up: %v", err)
	}
	assertTable(t, db, "mail_accounts", true)
	assertTable(t, db, "email_inbound", true)
	assertTable(t, db, "mail_thread_view", true)
	assertColumn(t, db, "mail_accounts", "is_default", true)
	assertColumn(t, db, "mail_accounts", "imap_host", true)
	assertColumn(t, db, "mail_thread_view", "account_id", true)
	// 函数的签名，不只是名字。改签名忘了 DROP 的那种错，只有问参数类型才
	// 看得出来——名字对得上，参数是老的。
	assertFunctionArgs(t, db, "mail_thread_view_refresh", "bigint, bigint, bigint, text")

	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertTable(t, db, "mail_accounts", false)
	assertTable(t, db, "email_inbound", false)
	assertTable(t, db, "mail_thread_view", false)

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("second up: %v", err)
	}
	assertTable(t, db, "mail_thread_view", true)
	assertColumn(t, db, "mail_thread_view", "account_id", true)
	assertColumn(t, db, "mail_accounts", "is_default", true)
	assertFunctionArgs(t, db, "mail_thread_view_refresh", "bigint, bigint, bigint, text")
}

func assertColumn(t *testing.T, db *sql.DB, table, column string, want bool) {
	t.Helper()
	var exists bool
	err := db.QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if exists != want {
		t.Fatalf("column %s.%s exists = %v, want %v", table, column, exists, want)
	}
}

func assertTable(t *testing.T, db *sql.DB, name string, want bool) {
	t.Helper()
	var exists bool
	err := db.QueryRowContext(context.Background(),
		"SELECT to_regclass('public.' || $1) IS NOT NULL", name).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if exists != want {
		t.Fatalf("table %s exists = %v, want %v", name, exists, want)
	}
}

// assertFunctionArgs 断言这个名字下**只有一个**函数，且参数正是这一串。
//
// 「只有一个」是重点：CREATE OR REPLACE 改不了参数列表，会安静地多留一个
// 老重载在库里，而触发器按参数个数挑到的正是老的那个。只查"新签名存在吗"
// 的话，那种错查不出来。
func assertFunctionArgs(t *testing.T, db *sql.DB, name, want string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT pg_get_function_arguments(p.oid)
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.proname = $1
		 ORDER BY 1`, name)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var args string
		if err := rows.Scan(&args); err != nil {
			t.Fatal(err)
		}
		got = append(got, args)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("函数 %s 有 %d 个重载 %v，应该正好一个——多出来的那个是"+
			"改签名时忘了 DROP 的老版本，触发器会调到它", name, len(got), got)
	}
	// pg_get_function_arguments 会带上形参名（"p_tenant bigint"），这里只
	// 比类型：形参改名不该让测试变红。
	gotTypes := argTypes(got[0])
	if gotTypes != want {
		t.Fatalf("函数 %s 的参数是 (%s)，应该是 (%s)", name, gotTypes, want)
	}
}

// argTypes 把 "p_tenant bigint, p_group text" 削成 "bigint, text"。
// 形参改名不该让测试变红，类型变了才该。
func argTypes(args string) string {
	parts := strings.Split(args, ",")
	for i, a := range parts {
		a = strings.TrimSpace(a)
		if sp := strings.LastIndex(a, " "); sp >= 0 {
			a = a[sp+1:]
		}
		parts[i] = a
	}
	return strings.Join(parts, ", ")
}
