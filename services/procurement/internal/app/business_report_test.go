package app

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type businessReportAccess struct{}

func (businessReportAccess) Allowed(_ context.Context, employeeID int64, code string) (bool, error) {
	return employeeID == 7 && code == "report:business:read", nil
}

func TestBusinessReportProcurementUsesVerifiedContractCosts(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 7})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
CREATE TEMP TABLE sourcing_cases(tenant_id bigint,owner_id bigint,owner_name text,deleted_at timestamptz,status text,inquiry_submitted_at timestamptz);
CREATE TEMP TABLE purchase_requirements(tenant_id bigint,id bigint,contract_id bigint,contract_item_id bigint);
CREATE TEMP TABLE purchase_order_items(tenant_id bigint,id bigint,po_id bigint,requirement_id bigint,amount numeric(18,2),qty numeric(18,4));
CREATE TEMP TABLE purchase_orders(tenant_id bigint,id bigint,po_no text,business_type text,currency text,total_amount numeric(18,2),business_document_no text,transport_batch_no int,signed_contract_key text,contract_verified_at timestamptz,source_business_id bigint,export_contract_no text,status text,ordered_at timestamptz,created_at timestamptz);
CREATE TEMP TABLE purchase_order_payment_closures(tenant_id bigint,po_id bigint,category text,open_amount numeric(18,2),created_at timestamptz,revoked_at timestamptz);
INSERT INTO sourcing_cases VALUES
 (1,7,'Sales A',NULL,'REVIEWING','2025-05-01 12:00+08'),
 (1,7,'Sales A',now(),'REVIEWING','2025-05-01 12:00+08'),
 (1,7,'Sales A',NULL,'REVIEWING','2025-07-01 12:00+08'),
 (2,8,'Sales B',NULL,'REVIEWING','2025-05-01 12:00+08');
INSERT INTO purchase_requirements VALUES (1,1,10,100),(1,2,20,200),(2,1,10,100);
INSERT INTO purchase_order_items VALUES (1,1,1,1,40,2),(1,2,1,2,90,9),(2,1,1,1,999,2);
INSERT INTO purchase_orders VALUES
 (1,1,'PO-1','PROCUREMENT','USD',130,'PC-1',0,'signed','2025-04-12',0,'','ORDERED','2025-04-11',now()),
 (1,2,'LOG-1','LOGISTICS','USD',10,'FL-1',1,'signed','2025-04-12',3,'CT-1','ORDERED','2025-04-11',now()),
 (2,1,'PO-1','PROCUREMENT','USD',999,'PC-2',0,'signed','2025-04-12',0,'','ORDERED','2025-04-11',now());
INSERT INTO purchase_order_payment_closures VALUES (1,1,'SETTLED',0,'2025-06-05',NULL),(1,2,'SETTLED',0,'2025-06-05',NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{pool: pool, permissions: businessReportAccess{}}
	inquiries, err := svc.ReportInquiries(ctx, &prv1.GetReportInquiriesRequest{StartDate: "2025-04-01", CutoffDate: "2025-06-30"})
	if err != nil || len(inquiries.GetEmployees()) != 1 || inquiries.GetEmployees()[0].Count != 1 {
		t.Fatalf("unexpected inquiry counts: %v %v", inquiries, err)
	}
	req := &prv1.GetReportContractCostsRequest{ContractId: 10, InternalContractNo: "CT-1", CutoffDate: "2025-06-30",
		ItemQuantities: map[int64]string{100: "2"}, FreightBatches: []int32{1}}
	costs, err := svc.ReportContractCosts(ctx, req)
	if err != nil || !costs.GetCovered() || len(costs.GetCosts()) != 2 || costs.GetCosts()[0].Amount != "40.00" || costs.GetCosts()[1].Amount != "10.00" {
		t.Fatalf("unexpected verified costs: %v %v", costs, err)
	}
	req.FreightBatches = []int32{1, 2}
	costs, err = svc.ReportContractCosts(ctx, req)
	if err != nil || costs.GetCovered() || len(costs.GetCosts()) != 2 {
		t.Fatalf("partial cost coverage: %v %v", costs, err)
	}
	denied := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 1, EmployeeID: 8})
	if _, err = svc.ReportContractCosts(denied, req); err == nil {
		t.Fatal("unauthorized user received costs")
	}
}
