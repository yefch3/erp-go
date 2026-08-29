package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// T2 的数据围栏：询价案件归创建销售。普通销售只能列出和打开自己的案件，
// 采购共享池或 Super 使用 ALL 范围时可以看到全部案件。
func TestSourcingCaseOwnerAndSalesScope(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed sourcing scope test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sourcing_cases WHERE tenant_id=$1`, tenantID)
	}()

	const salesA, salesB, sharedViewer = 8101, 8102, 8199
	scopes := &scopeStub{views: map[int64]Visibility{
		salesA:       {EmployeeIDs: []int64{salesA}, ScopeType: "SELF"},
		salesB:       {EmployeeIDs: []int64{salesB}, ScopeType: "SELF"},
		sharedViewer: {All: true, ScopeType: "ALL"},
	}}
	svc := New(pool, Deps{Scopes: scopes})

	create := func(title string, op Operator) SourcingCaseView {
		view, createErr := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
			Title: title,
			Lines: []SourcingLineInput{{Product: "Steel coil", Quantity: "10", QuantityUnit: "TON"}},
		}, op)
		if createErr != nil {
			t.Fatal(createErr)
		}
		return view
	}
	caseA := create("Sales A inquiry", Operator{ID: salesA, Name: "Sales A"})
	caseB := create("Sales B inquiry", Operator{ID: salesB, Name: "Sales B"})

	if caseA.Head.OwnerID != salesA || caseA.Head.OwnerName != "Sales A" {
		t.Fatalf("created case owner=%d/%q", caseA.Head.OwnerID, caseA.Head.OwnerName)
	}

	rows, total, err := svc.ListSourcingCases(ctx, tenantID, SourcingFilter{Status: "INTAKE_PENDING"}, 1, 20, Operator{ID: salesA})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != caseA.Head.ID {
		t.Fatalf("sales A list rows=%d total=%d data=%+v", len(rows), total, rows)
	}
	if err = svc.AuthorizeSourcingCase(ctx, tenantID, caseA.Head.ID, Operator{ID: salesA}); err != nil {
		t.Fatalf("owner must open own case: %v", err)
	}
	if err = svc.AuthorizeSourcingCase(ctx, tenantID, caseB.Head.ID, Operator{ID: salesA}); code(err) != "SC_CASE_NOT_FOUND" {
		t.Fatalf("foreign case must look not-found, got %v", err)
	}

	rows, total, err = svc.ListSourcingCases(ctx, tenantID, SourcingFilter{Status: "INTAKE_PENDING"}, 1, 20, Operator{ID: sharedViewer})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("ALL scope rows=%d total=%d", len(rows), total)
	}
	if err = svc.AuthorizeSourcingCase(ctx, tenantID, caseB.Head.ID, Operator{ID: sharedViewer}); err != nil {
		t.Fatalf("ALL scope must open every case: %v", err)
	}
}
