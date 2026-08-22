package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// The workbench's two new numbers (B4): "approved but not yet sent" rides
// the order list's own fence, and the overdue-RFQ chase list rides the
// sourcing case fence — the workbench is another door into the lists, not
// another permission system.
func TestWorkbenchQueries(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed workbench test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"factory_rfqs", "sourcing_cases", "purchase_orders"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Buyer"}

	// Three orders: ORDERED unsent, ORDERED sent, PENDING_APPROVAL.
	seedOrder := func(no, status, sendStatus string) {
		if _, err := pool.Exec(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,expected_date,status,send_status,buyer_id,buyer_name) VALUES ($1,$2,9,'Mill','USD',100,current_date+10,$3,$4,77,'Buyer')`,
			tenantID, no, status, sendStatus); err != nil {
			t.Fatal(err)
		}
	}
	seedOrder("PO-WB-1", "ORDERED", "NOT_SENT")
	seedOrder("PO-WB-2", "ORDERED", "SENT")
	seedOrder("PO-WB-3", "PENDING_APPROVAL", "NOT_SENT")

	unsent, total, err := svc.ListOrders(ctx, tenantID, OrderFilter{Unsent: true}, 1, 20, op)
	if err != nil || total != 1 || unsent[0].PoNo != "PO-WB-1" {
		t.Fatalf("unsent = approved and not sent, nothing else: %v total=%d", err, total)
	}

	// Two RFQs on a case: one overdue, one still inside its deadline.
	var caseID int64
	if err := pool.QueryRow(ctx, `INSERT INTO sourcing_cases (tenant_id,case_no,title,status,owner_id,owner_name) VALUES ($1,'SC-WB-1','逾期测试','SOURCING',77,'Buyer') RETURNING id`,
		tenantID).Scan(&caseID); err != nil {
		t.Fatal(err)
	}
	seedRFQ := func(no string, due string, status string) {
		if _, err := pool.Exec(ctx, `INSERT INTO factory_rfqs (tenant_id,case_id,rfq_no,supplier_id,supplier_name,status,response_due_at) VALUES ($1,$2,$3,9,'Slow Mill',$4,$5::date)`,
			tenantID, caseID, no, status, due); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	future := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	seedRFQ("RFQ-WB-LATE", past, "SENT")
	seedRFQ("RFQ-WB-OK", future, "SENT")
	seedRFQ("RFQ-WB-DONE", past, "QUOTED") // answered late is not overdue work

	overdue, err := svc.ListOverdueFactoryRFQs(ctx, tenantID, 10, op)
	if err != nil || len(overdue) != 1 {
		t.Fatalf("only the silent, past-deadline RFQ is overdue: %v n=%d", err, len(overdue))
	}
	if overdue[0].RfqNo != "RFQ-WB-LATE" || overdue[0].OverdueDays != 3 {
		t.Fatalf("overdue row: %+v", overdue[0])
	}

	// The fence: a caller whose sourcing scope excludes the owner sees none.
	fenced := New(pool, Deps{Scopes: fixedScope{Visibility{All: false, EmployeeIDs: []int64{12345}, ScopeType: "SELF"}}})
	fencedRows, err := fenced.ListOverdueFactoryRFQs(ctx, tenantID, 10, Operator{ID: 12345})
	if err != nil || len(fencedRows) != 0 {
		t.Fatalf("out-of-scope caller must see nothing: %v n=%d", err, len(fencedRows))
	}
	_ = strings.TrimSpace
}
