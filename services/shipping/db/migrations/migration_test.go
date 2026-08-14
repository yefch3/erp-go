package migrations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestUpDownUp 只使用独立的迁移测试库。该测试会把数据库完整回滚到版本 0，
// 不能与船期业务集成测试共用 SHIPPING_TEST_DSN，否则并发执行时会临时拆掉
// 业务测试正在访问的表或字段。
func TestUpDownUp(t *testing.T) {
	dsn := os.Getenv("SHIPPING_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set SHIPPING_MIGRATION_TEST_DSN to a disposable PostgreSQL database")
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
	assertTable(t, db, "shipping_schedules", true)
	assertTable(t, db, "outbox_events", true)
	assertTable(t, db, "shipping_schedule_changes", true)
	assertTable(t, db, "shipping_route_nodes", true)
	assertTable(t, db, "shipping_delay_events", true)
	assertTable(t, db, "shipping_arrival_reminders", true)
	assertTable(t, db, "shipping_arrival_reminder_rules", true)
	assertColumn(t, db, "shipping_arrival_reminders", "read_at", true)
	assertColumn(t, db, "shipping_arrival_reminders", "next_retry_at", true)
	assertTable(t, db, "shipping_documents", true)
	assertColumn(t, db, "shipping_schedules", "loading_port_id", true)
	assertColumn(t, db, "shipping_schedules", "discharge_port_timezone", true)
	assertColumn(t, db, "shipping_route_nodes", "port_id", true)

	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertTable(t, db, "shipping_schedules", false)
	assertTable(t, db, "outbox_events", false)
	assertTable(t, db, "shipping_schedule_changes", false)
	assertTable(t, db, "shipping_route_nodes", false)
	assertTable(t, db, "shipping_delay_events", false)
	assertTable(t, db, "shipping_arrival_reminders", false)
	assertTable(t, db, "shipping_arrival_reminder_rules", false)
	assertTable(t, db, "shipping_documents", false)

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("second up: %v", err)
	}
	assertTable(t, db, "shipping_schedules", true)
	assertTable(t, db, "shipping_schedule_changes", true)
	assertTable(t, db, "shipping_route_nodes", true)
	assertTable(t, db, "shipping_documents", true)
	assertColumn(t, db, "shipping_schedules", "loading_port_id", true)
	assertColumn(t, db, "shipping_route_nodes", "port_id", true)
	assertColumn(t, db, "shipping_arrival_reminders", "read_at", true)
	assertTable(t, db, "shipping_arrival_reminder_rules", true)
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
