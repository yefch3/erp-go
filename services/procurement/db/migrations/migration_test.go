package migrations

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestUpDownUp 仅使用可丢弃的采购迁移测试库，验证全部采购迁移可以完整回滚后再次应用。
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
	// This DSN must target the disposable migration database. Start clean so
	// the legacy row below really exists before migration 89, even on re-runs.
	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("reset disposable migration database: %v", err)
	}
	if err := goose.UpTo(db, ".", 88); err != nil {
		t.Fatalf("up: %v", err)
	}
	var legacyID int64
	if err := db.QueryRow(`INSERT INTO travel_reimbursements(tenant_id,claim_no,claimant_id,claimant_name,trip_start,trip_end,origin,destination,purpose,amount,currency,payment_account,status)
 VALUES(642,'MIGRATION-642-LEGACY',642,'Synthetic','2026-11-16','2026-11-17','Shanghai','Beijing','Legacy trip',100,'CNY','Synthetic','PAID') RETURNING id`).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("expense migration: %v", err)
	}
	var preserved bool
	if err := db.QueryRow(`SELECT expense_type='TRAVEL' AND expense_date IS NULL AND trip_start='2026-11-16'::date AND trip_end='2026-11-17'::date AND status='PAID' AND amount=100 AND origin='Shanghai' AND destination='Beijing' FROM travel_reimbursements WHERE id=$1`, legacyID).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("legacy travel changed: %v %v", preserved, err)
	}
	var expenseID int64
	if err := db.QueryRow(`INSERT INTO travel_reimbursements(tenant_id,claim_no,claimant_id,claimant_name,origin,destination,purpose,amount,currency,payment_account,expense_type,expense_date)
 VALUES(642,'MIGRATION-642-OFFICE',642,'Synthetic','','','Supplies',23.45,'CNY','Synthetic','OFFICE_SUPPLIES','2026-11-17') RETURNING id`).Scan(&expenseID); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(db, ".", 88); err != nil {
		t.Fatalf("expense down: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("expense re-up: %v", err)
	}
	if err := db.QueryRow(`SELECT expense_type='OFFICE_SUPPLIES' AND expense_date='2026-11-17'::date AND trip_start IS NULL AND trip_end IS NULL AND amount=23.45 FROM travel_reimbursements WHERE id=$1`, expenseID).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("expense lost on rollback/re-up: %v %v", preserved, err)
	}
	assertProcurementTable(t, db, "sourcing_case_changes", true)
	assertProcurementColumn(t, db, "factory_rfqs", "factory_id", true)
	assertProcurementColumn(t, db, "sourcing_cases", "source_file_data", true)
	assertProcurementTable(t, db, "inquiry_templates", true)
	assertProcurementTable(t, db, "inquiry_template_fields", true)
	assertProcurementTable(t, db, "sourcing_procurement_participants", true)
	assertProcurementColumn(t, db, "sourcing_cases", "inquiry_template_version", true)
	assertProcurementColumn(t, db, "sourcing_lines", "custom_fields", true)
	assertProcurementTable(t, db, "purchase_inspections", true)
	assertProcurementColumn(t, db, "purchase_orders", "closed_at", true)
	assertProcurementColumn(t, db, "purchase_requirements", "owner_id", true)
	assertProcurementColumn(t, db, "cost_scenarios", "version_no", true)
	assertProcurementTable(t, db, "sourcing_customer_selections", true)
	assertProcurementTable(t, db, "sourcing_customer_selection_items", true)
	assertProcurementTable(t, db, "sourcing_final_recheck_tasks", true)
	assertProcurementTable(t, db, "sourcing_sales_shipping_options", true)
	assertProcurementTable(t, db, "sourcing_customer_selection_shipments", true)
	assertProcurementColumn(t, db, "sourcing_customer_selection_items", "product_spec", true)
	assertProcurementColumn(t, db, "purchase_requirements", "source_payment_terms", true)
	assertProcurementTable(t, db, "quality_inspection_tasks", true)
	assertProcurementTable(t, db, "quality_inspection_rounds", true)
	assertProcurementTable(t, db, "quality_inspection_files", true)
	assertProcurementTable(t, db, "travel_reimbursements", true)
	assertProcurementColumn(t, db, "travel_reimbursements", "expense_type", true)
	assertProcurementColumn(t, db, "travel_reimbursements", "expense_date", true)
	assertProcurementTable(t, db, "travel_reimbursement_files", true)
	assertProcurementTable(t, db, "travel_reimbursement_history", true)
	assertProcurementTable(t, db, "daily_price_dimensions", true)
	assertProcurementTable(t, db, "daily_base_prices", true)
	assertProcurementTable(t, db, "daily_basis_spreads", true)
	assertProcurementColumnType(t, db, "sourcing_lines", "packaging", "text")
	assertProcurementColumn(t, db, "sourcing_cases", "display_inquiry_no", true)
	assertProcurementColumn(t, db, "sourcing_cases", "deleted_at", true)
	assertProcurementColumn(t, db, "purchase_execution_supplier_quotes", "selected", true)
	assertProcurementColumn(t, db, "purchase_execution_supplier_quotes", "quote_category", true)
	assertProcurementColumn(t, db, "purchase_execution_supplier_quotes", "calculated_unit_price", true)
	assertProcurementTable(t, db, "purchase_execution_inquiry_files", true)
	assertProcurementTable(t, db, "purchase_order_execution_files", true)
	assertProcurementTable(t, db, "purchase_order_draft_files", true)
	assertPaymentTermsText(t, db)
	assertProcurementColumn(t, db, "historical_purchase_orders", "export_contract_no", true)
	assertProcurementColumn(t, db, "historical_purchase_orders", "destination_port", true)
	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	assertProcurementTable(t, db, "sourcing_cases", false)
	assertProcurementTable(t, db, "daily_base_prices", false)
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("second up: %v", err)
	}
	assertProcurementTable(t, db, "sourcing_case_changes", true)
	assertProcurementColumn(t, db, "sourcing_lines", "revision_no", true)
	assertProcurementTable(t, db, "inquiry_templates", true)
	assertProcurementTable(t, db, "sourcing_procurement_participants", true)
	assertProcurementColumn(t, db, "cost_scenarios", "version_no", true)
	assertProcurementTable(t, db, "sourcing_customer_selections", true)
	assertProcurementTable(t, db, "sourcing_customer_selection_items", true)
	assertProcurementTable(t, db, "sourcing_final_recheck_tasks", true)
	assertProcurementTable(t, db, "sourcing_sales_shipping_options", true)
	assertProcurementTable(t, db, "sourcing_customer_selection_shipments", true)
	assertProcurementColumn(t, db, "sourcing_customer_selection_items", "product_spec", true)
	assertProcurementColumn(t, db, "purchase_requirements", "source_payment_terms", true)
	assertProcurementTable(t, db, "quality_inspection_tasks", true)
	assertProcurementColumnType(t, db, "sourcing_lines", "packaging", "text")
	assertProcurementColumn(t, db, "sourcing_cases", "display_inquiry_no", true)
	assertProcurementColumn(t, db, "purchase_execution_supplier_quotes", "selected", true)
	assertProcurementColumn(t, db, "purchase_execution_supplier_quotes", "quote_category", true)
	assertProcurementTable(t, db, "purchase_execution_inquiry_files", true)
	assertProcurementTable(t, db, "purchase_order_draft_files", true)
	assertProcurementTable(t, db, "daily_price_dimensions", true)
	assertProcurementTable(t, db, "daily_base_prices", true)
	assertProcurementTable(t, db, "daily_basis_spreads", true)
	assertPaymentTermsText(t, db)
}

func assertPaymentTermsText(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, field := range []struct{ table, column string }{
		{"sourcing_lines", "payment_terms"},
		{"supplier_quotes", "payment_terms"},
		{"procurement_plan_items", "payment_terms"},
		{"sourcing_final_recheck_tasks", "final_payment_terms"},
		{"sourcing_customer_selection_items", "final_customer_payment_terms"},
		{"purchase_requirements", "source_payment_terms"},
		{"purchase_orders", "payment_terms"},
		{"purchase_execution_supplier_quotes", "payment_terms"},
	} {
		assertProcurementColumnType(t, db, field.table, field.column, "text")
	}
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

func assertProcurementColumnType(t *testing.T, db *sql.DB, table, column, want string) {
	t.Helper()
	var got string
	err := db.QueryRowContext(context.Background(), `SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, column).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("column %s.%s type=%q want=%q", table, column, got, want)
	}
}
