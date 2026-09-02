package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestSalesWithdrawalClearsRoundAndKeepsCaseNumber(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed sales withdrawal test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"sourcing_case_changes", "sourcing_shipping_requests", "sourcing_procurement_participants", "factory_rfqs", "sourcing_lines", "sourcing_cases"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	sales := Operator{ID: 101, Name: "销售甲"}
	otherSales := Operator{ID: 102, Name: "销售乙"}
	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "销售撤回测试", Lines: []SourcingLineInput{{Product: "钢卷", Quantity: "10", QuantityUnit: "MT"}},
	}, sales)
	if err != nil {
		t.Fatal(err)
	}
	caseID, caseNo := created.Head.ID, created.Head.CaseNo
	if _, err = svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{created.Lines[0].ID}, "", sales); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO factory_rfqs(tenant_id,case_id,rfq_no,supplier_id,supplier_name,created_by,created_by_name)
VALUES($1,$2,$3,9001,'测试供应商',201,'采购员')`, tenantID, caseID, "RFQ-WITHDRAW-"+caseNo); err != nil {
		t.Fatal(err)
	}

	if _, err = svc.WithdrawSourcingCase(ctx, tenantID, caseID, "客户数量填错", otherSales); apierr.CodeFromError(err) != "SC_WITHDRAW_OWNER_ONLY" {
		t.Fatalf("non-owner error = %v", err)
	}
	withdrawn, err := svc.WithdrawSourcingCase(ctx, tenantID, caseID, "客户数量填错", sales)
	if err != nil {
		t.Fatal(err)
	}
	if withdrawn.Head.CaseNo != caseNo || withdrawn.Head.Status != "INTAKE_PENDING" || withdrawn.Head.HandoffStatus != "SALES_WITHDRAWN" {
		t.Fatalf("withdrawn head = %+v", withdrawn.Head)
	}
	if withdrawn.Head.ReturnReason != "客户数量填错" || withdrawn.Lines[0].Decision != "PENDING" {
		t.Fatalf("withdrawn detail = %+v / %+v", withdrawn.Head, withdrawn.Lines[0])
	}
	for _, table := range []string{"factory_rfqs", "sourcing_shipping_requests"} {
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1 AND case_id=$2`, tenantID, caseID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v, want 0", table, count, err)
		}
	}

	resubmitted, err := svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{created.Lines[0].ID}, "", sales)
	if err != nil {
		t.Fatal(err)
	}
	if resubmitted.Head.CaseNo != caseNo || resubmitted.Head.RequirementVersionNo != 1 || resubmitted.Head.HandoffStatus != "WAITING_ACCEPTANCE" {
		t.Fatalf("resubmitted head = %+v", resubmitted.Head)
	}
	var changes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM sourcing_case_changes WHERE tenant_id=$1 AND case_id=$2 AND action='SALES_WITHDRAWN'`, tenantID, caseID).Scan(&changes); err != nil || changes != 1 {
		t.Fatalf("withdrawal changes=%d err=%v", changes, err)
	}
}
