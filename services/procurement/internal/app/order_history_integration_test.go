package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestPurchaseOrderWorkAndHistoryLists 验证当前工作列表不会混入已结案采购单，
// 同时已结案与已作废单据仍完整保留在历史记录中。
func TestPurchaseOrderWorkAndHistoryLists(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed order history test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID) }()

	seed := func(no, status string, closed bool) {
		var closedAt any
		if closed {
			closedAt = time.Now().UTC()
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO purchase_orders
			(tenant_id, po_no, supplier_id, supplier_name, currency, total_amount,
			 expected_date, status, buyer_id, buyer_name, closed_at)
			VALUES ($1,$2,9,'Mill','USD',100,current_date+10,$3,77,'Buyer',$4)`,
			tenantID, no, status, closedAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("PO-OPEN-RECEIVED", "RECEIVED", false)
	seed("PO-CLOSED-RECEIVED", "RECEIVED", true)
	seed("PO-OPEN-PARTIAL", "PARTIALLY_RECEIVED", false)
	seed("PO-CLOSED-PARTIAL", "PARTIALLY_RECEIVED", true)
	seed("PO-VOID", "CANCELLED", false)

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Buyer"}
	rows, total, err := svc.ListOrders(ctx, tenantID, OrderFilter{Status: "RECEIVED_OPEN"}, 1, 20, op)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].PoNo != "PO-OPEN-RECEIVED" {
		t.Fatalf("received work list must contain only unclosed received order: err=%v total=%d rows=%v", err, total, rows)
	}
	rows, total, err = svc.ListOrders(ctx, tenantID, OrderFilter{Status: "PARTIALLY_RECEIVED"}, 1, 20, op)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].PoNo != "PO-OPEN-PARTIAL" {
		t.Fatalf("partial work list must exclude closed order: err=%v total=%d rows=%v", err, total, rows)
	}
	rows, total, err = svc.ListOrders(ctx, tenantID, OrderFilter{Status: "HISTORY"}, 1, 20, op)
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("history must contain closed and voided orders: err=%v total=%d rows=%v", err, total, rows)
	}
}
