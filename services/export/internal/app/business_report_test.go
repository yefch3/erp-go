package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
)

func TestBusinessReportQuarterAndTenantScopedDrilldown(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
CREATE TEMP TABLE contracts(id bigint,tenant_id bigint,contract_no text,external_contract_no text DEFAULT '',customer_id bigint,customer_name text,sales_employee_id bigint,sales_employee text,signed_at timestamptz,effective_at timestamptz,created_at timestamptz DEFAULT now(),receivable_due_date date,opening_received_amount numeric(18,2) DEFAULT 0,entry_source text DEFAULT 'STANDARD',status text DEFAULT 'EFFECTIVE',current_version_id bigint,completed_at timestamptz);
CREATE TEMP TABLE contract_versions(id bigint,tenant_id bigint,contract_id bigint,currency text,total_amount numeric(18,2),version_no int);
CREATE TEMP TABLE receipt_allocations(id bigint,tenant_id bigint,contract_id bigint,amount numeric(18,2),fee_amount numeric(18,2),currency text,received_at date,allocated_at timestamptz,reversal_of bigint);
CREATE TEMP TABLE contract_receivable_closures(tenant_id bigint,contract_id bigint,created_at timestamptz,revoked_at timestamptz);
CREATE TEMP TABLE contract_handoff_plans(tenant_id bigint,contract_version_id bigint,batches jsonb);
CREATE TEMP TABLE contract_items(id bigint,tenant_id bigint,contract_version_id bigint,qty numeric(18,4));
CREATE TEMP TABLE quotations(tenant_id bigint,sales_employee_id bigint,sales_employee text,status text,sent_at timestamptz);
INSERT INTO contracts (id,tenant_id,contract_no,customer_id,customer_name,sales_employee_id,sales_employee,signed_at,effective_at,receivable_due_date,current_version_id)
VALUES (10,1,'CT-1',1,'Test A',2,'Sales A','2025-04-01 00:00+08','2025-04-01 00:00+08','2025-04-10',11),
       (20,2,'CT-2',2,'Test B',3,'Sales B','2025-04-05 00:00+08','2025-04-05 00:00+08','2025-04-10',20),
       (30,1,'CT-3',3,'Test C',2,'Sales A','2025-03-05 00:00+08','2025-03-05 00:00+08',NULL,30);
UPDATE contracts SET status='COMPLETED', completed_at='2025-06-10 12:00+08' WHERE id=10;
INSERT INTO contract_handoff_plans VALUES (1,11,'[{"batchNo":1}]');
INSERT INTO contract_items VALUES (101,1,11,2);
INSERT INTO contract_versions VALUES (10,1,10,'USD',100,1),(11,1,10,'USD',120,2),(20,2,20,'EUR',999,1),(30,1,30,'USD',40,1);
INSERT INTO receipt_allocations VALUES
 (1,1,10,25,5,'USD','2025-04-11','2025-04-11 12:00+08',NULL),
 (2,1,10,-25,-5,'USD',NULL,'2025-04-15 12:00+08',1),
 (3,1,10,40,0,'USD','2025-05-02','2025-05-02 12:00+08',NULL),
 (4,1,10,5,0,'USD',NULL,'2025-05-21 12:00+08',NULL),
 (5,2,20,900,0,'EUR','2025-04-12','2025-04-12 12:00+08',NULL),
 (6,1,10,75,0,'USD','2025-07-01','2025-07-01 12:00+08',NULL);
INSERT INTO contract_receivable_closures VALUES (1,10,'2025-07-02 12:00+08',NULL);
INSERT INTO quotations VALUES (1,2,'Sales A','SENT','2025-04-04 12:00+08'),(2,2,'Sales A','SENT','2025-04-04 12:00+08');`)
	if err != nil {
		t.Fatal(err)
	}
	start, cutoff, err := reportPeriod(2025, 2, time.Date(2025, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	out := &exv1.GetBusinessReportResponse{StartDate: start.Format(time.DateOnly), CutoffDate: cutoff.Format(time.DateOnly)}
	if err := loadReportQuarter(ctx, tx, 1, start, cutoff, out); err != nil {
		t.Fatal(err)
	}
	if out.SignedCount != 1 || len(out.Signed) != 1 || out.Signed[0].Amount != "100.00" ||
		len(out.Received) != 1 || out.Received[0].Amount != "40.00" ||
		len(out.Overdue) != 1 || out.Overdue[0].Amount != "75.00" || out.Total != 1 ||
		out.MissingDueDateCount != 1 || out.UndatedReceiptCount != 1 ||
		len(out.Months) != 3 || out.Months[0].Received[0].Amount != "0.00" {
		t.Fatalf("incorrect quarter: %v", out)
	}
	if err := loadReportContracts(ctx, tx, 1, out.CutoffDate, "overdue", 1, 0, out); err != nil {
		t.Fatal(err)
	}
	if len(out.Contracts) != 1 || out.Contracts[0].ContractId != 10 || out.Contracts[0].Balance != "75.00" {
		t.Fatalf("incorrect overdue rows: %v", out.Contracts)
	}
	if err := loadReportContracts(ctx, tx, 1, out.CutoffDate, "contract", 1, 20, out); err == nil {
		t.Fatal("other tenant's contract returned")
	}
	if err := loadReportSales(ctx, tx, 1, start, cutoff, out); err != nil {
		t.Fatal(err)
	}
	if len(out.Sales) != 1 || out.Sales[0].QuoteCount != 1 || out.Sales[0].SignedCount != 1 {
		t.Fatalf("incorrect sales rows: %v", out.Sales)
	}
	if err := loadReportContracts(ctx, tx, 1, out.CutoffDate, "contract", 1, 10, out); err != nil {
		t.Fatal(err)
	}
	if err := loadReportCostBasis(ctx, tx, 1, 10, out); err != nil {
		t.Fatal(err)
	}
	if out.InternalContractNo != "CT-1" || out.ContractItemQuantities[101] != "2.0000" || len(out.FreightBatches) != 1 || out.FreightBatches[0] != 1 {
		t.Fatalf("incorrect cost basis: %v", out)
	}

}
