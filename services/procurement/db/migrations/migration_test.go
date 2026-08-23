package migrations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestUpDownUp 仅使用可丢弃的采购迁移测试库，验证 P6 迁移可以完整回滚后再次应用。
func TestUpDownUp(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set PROCUREMENT_MIGRATION_TEST_DSN to a disposable PostgreSQL database")
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
	assertProcurementTable(t, db, "sourcing_case_changes", true)
	assertProcurementColumn(t, db, "factory_rfqs", "factory_id", true)
	assertProcurementColumn(t, db, "sourcing_cases", "source_file_data", true)
	assertProcurementTable(t, db, "inquiry_templates", true)
	assertProcurementTable(t, db, "inquiry_template_fields", true)
	assertProcurementColumn(t, db, "sourcing_cases", "inquiry_template_version", true)
	assertProcurementColumn(t, db, "sourcing_lines", "custom_fields", true)
	assertProcurementTable(t, db, "purchase_inspections", true)
	assertProcurementColumn(t, db, "purchase_orders", "closed_at", true)
	assertProcurementColumn(t, db, "purchase_requirements", "owner_id", true)
	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertProcurementTable(t, db, "sourcing_cases", false)
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("second up: %v", err)
	}
	assertProcurementTable(t, db, "sourcing_case_changes", true)
	assertProcurementColumn(t, db, "sourcing_lines", "revision_no", true)
	assertProcurementTable(t, db, "inquiry_templates", true)
}

func assertProcurementTable(t *testing.T, db *sql.DB, name string, want bool) {
	t.Helper()
	var got bool
	if err := db.QueryRowContext(context.Background(), "SELECT to_regclass('public.' || $1) IS NOT NULL", name).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s exists=%v want=%v", name, got, want)
	}
}

func assertProcurementColumn(t *testing.T, db *sql.DB, table, column string, want bool) {
	t.Helper()
	var got bool
	err := db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, table, column).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("column %s.%s exists=%v want=%v", table, column, got, want)
	}
}
