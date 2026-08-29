package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestSourcingParticipantsAndPrimaryBuyerChange(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed participant test")
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
	buyerA := Operator{ID: 20, Name: "采购甲"}
	buyerB := Operator{ID: 21, Name: "采购乙"}
	manager := Operator{ID: 30, Name: "采购经理"}
	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "T3 多人采购测试", Lines: []SourcingLineInput{{Product: "钢卷", Quantity: "10", QuantityUnit: "MT"}},
	}, sales)
	if err != nil {
		t.Fatal(err)
	}
	caseID := created.Head.ID
	if _, err = svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{created.Lines[0].ID}, "", sales); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.JoinSourcingCase(ctx, tenantID, caseID, buyerA); err != nil {
		t.Fatal(err)
	}
	participants, err := svc.JoinSourcingCase(ctx, tenantID, caseID, buyerB)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 2 {
		t.Fatalf("participants = %d, want 2", len(participants))
	}
	beforePrimary, err := svc.GetSourcingCase(ctx, tenantID, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if beforePrimary.Head.AcceptedBy != nil || beforePrimary.Head.HandoffStatus != "WAITING_ACCEPTANCE" {
		t.Fatalf("participation must not claim or lock case: %+v", beforePrimary.Head)
	}

	participants, claimed, err := svc.RequestPrimarySourcingCase(ctx, tenantID, caseID, buyerA)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Head.AcceptedBy == nil || *claimed.Head.AcceptedBy != buyerA.ID || claimed.Head.HandoffStatus != "IN_PROGRESS" {
		t.Fatalf("primary claim = %+v", claimed.Head)
	}
	if _, _, err = svc.RequestPrimarySourcingCase(ctx, tenantID, caseID, buyerB); err != nil {
		t.Fatal(err)
	}
	participants, changed, err := svc.AssignPrimarySourcingCase(ctx, tenantID, caseID, buyerB.ID, "供应商分工调整", manager)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Head.AcceptedBy == nil || *changed.Head.AcceptedBy != buyerB.ID {
		t.Fatalf("changed primary = %+v", changed.Head)
	}
	roles := map[int64]string{}
	for _, item := range participants {
		roles[item.EmployeeID] = item.Role
	}
	if roles[buyerA.ID] != "COLLABORATOR" || roles[buyerB.ID] != "PRIMARY" {
		t.Fatalf("roles after change = %#v", roles)
	}
	var reason string
	if err = pool.QueryRow(ctx, `SELECT reason FROM sourcing_case_changes
WHERE tenant_id=$1 AND case_id=$2 AND action='PRIMARY_CHANGED' ORDER BY id DESC LIMIT 1`, tenantID, caseID).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "供应商分工调整" {
		t.Fatalf("change reason = %q", reason)
	}
}
