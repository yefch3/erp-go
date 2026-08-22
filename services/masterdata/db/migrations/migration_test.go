package migrations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestUpDownUp 仅使用可丢弃的 masterdata 迁移测试库，验证整条迁移链可以
// 从零建起、完整回滚、再次建起。
//
// 这个测试因一次真实事故而生：00016 引用了 number_rules 里不存在的列名，
// CI 全绿，第一次真正执行是在生产部署里。迁移 SQL 和别的代码一样，
// 没跑过就等于没写对。
func TestUpDownUp(t *testing.T) {
	dsn := os.Getenv("MD_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set MD_MIGRATION_TEST_DSN to a disposable PostgreSQL database")
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
	assertNumberRule(t, db, "SUPPLIER_PAYMENT", true)
	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertMasterdataTable(t, db, "number_rules", false)
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("second up: %v", err)
	}
	assertNumberRule(t, db, "SUPPLIER_PAYMENT", true)
}

func assertMasterdataTable(t *testing.T, db *sql.DB, name string, want bool) {
	t.Helper()
	var got bool
	if err := db.QueryRowContext(context.Background(), "SELECT to_regclass('public.' || $1) IS NOT NULL", name).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s exists=%v want=%v", name, got, want)
	}
}

func assertNumberRule(t *testing.T, db *sql.DB, bizType string, want bool) {
	t.Helper()
	var got bool
	err := db.QueryRowContext(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM number_rules WHERE biz_type=$1)", bizType).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("number rule %s exists=%v want=%v", bizType, got, want)
	}
}
