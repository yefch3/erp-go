package migrations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestUpDownUp runs only when SHIPPING_TEST_DSN names a disposable shipping
// database. CI and local verification set it explicitly; ordinary unit tests
// do not risk altering a developer database by guessing a target.
func TestUpDownUp(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("set SHIPPING_TEST_DSN to a disposable PostgreSQL database")
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
	assertTable(t, db, "shipping_documents", true)

	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertTable(t, db, "shipping_schedules", false)
	assertTable(t, db, "outbox_events", false)
	assertTable(t, db, "shipping_schedule_changes", false)
	assertTable(t, db, "shipping_route_nodes", false)
	assertTable(t, db, "shipping_delay_events", false)
	assertTable(t, db, "shipping_arrival_reminders", false)
	assertTable(t, db, "shipping_documents", false)

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("second up: %v", err)
	}
	assertTable(t, db, "shipping_schedules", true)
	assertTable(t, db, "shipping_schedule_changes", true)
	assertTable(t, db, "shipping_route_nodes", true)
	assertTable(t, db, "shipping_documents", true)
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
