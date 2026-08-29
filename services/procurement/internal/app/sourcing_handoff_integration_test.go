package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestSourcingHandoffAcceptReturnAndResubmitVersion(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed SP3 handoff test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"sourcing_case_changes", "sourcing_procurement_participants", "sourcing_lines", "sourcing_cases"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	sales := Operator{ID: 10, Name: "销售甲"}
	buyer := Operator{ID: 20, Name: "采购乙"}
	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "SP3 交接测试", Lines: []SourcingLineInput{{Product: "钢卷", Quantity: "10", QuantityUnit: "MT"}},
	}, sales)
	if err != nil {
		t.Fatal(err)
	}
	caseID := created.Head.ID
	submitted, err := svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{created.Lines[0].ID}, "", sales)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Head.HandoffStatus != "WAITING_ACCEPTANCE" || submitted.Head.RequirementVersionNo != 1 {
		t.Fatalf("first submission = %+v", submitted.Head)
	}

	accepted, err := svc.AcceptSourcingCase(ctx, tenantID, caseID, buyer)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Head.HandoffStatus != "IN_PROGRESS" || accepted.Head.AcceptedByName != buyer.Name {
		t.Fatalf("accepted handoff = %+v", accepted.Head)
	}

	returned, err := svc.ReturnSourcingCase(ctx, tenantID, caseID, []string{"交期", "目的港"}, "客户尚未确认", buyer)
	if err != nil {
		t.Fatal(err)
	}
	if returned.Head.HandoffStatus != "RETURNED_FOR_SUPPLEMENT" || returned.Head.Status != "INTAKE_PENDING" || len(returned.Head.ReturnFields) != 2 {
		t.Fatalf("returned handoff = %+v", returned.Head)
	}
	if returned.Lines[0].Decision != "PENDING" || returned.Lines[0].ProductID != 0 || returned.Lines[0].UomID != 0 {
		t.Fatalf("returned line = %+v, want pending without stale product mapping", returned.Lines[0])
	}

	// 重新提交会产生下一需求版本，历史接单和退回记录不被覆盖。
	resubmitReason := "补充目的港并确认交期"
	resubmitted, err := svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{created.Lines[0].ID}, resubmitReason, sales)
	if err != nil {
		t.Fatal(err)
	}
	if resubmitted.Head.RequirementVersionNo != 2 {
		t.Fatalf("requirement version = %d, want 2", resubmitted.Head.RequirementVersionNo)
	}
	var submittedReason string
	if err = pool.QueryRow(ctx, `SELECT reason FROM sourcing_case_changes WHERE tenant_id=$1 AND case_id=$2 AND action='SUBMITTED_TO_PROCUREMENT' ORDER BY id DESC LIMIT 1`, tenantID, caseID).Scan(&submittedReason); err != nil {
		t.Fatal(err)
	}
	if submittedReason != resubmitReason {
		t.Fatalf("resubmit reason = %q, want %q", submittedReason, resubmitReason)
	}
	var changes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM sourcing_case_changes WHERE tenant_id=$1 AND case_id=$2 AND action IN ('PRIMARY_ASSIGNED','RETURNED_FOR_SUPPLEMENT')`, tenantID, caseID).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if changes != 2 {
		t.Fatalf("handoff history rows = %d, want 2", changes)
	}
}
