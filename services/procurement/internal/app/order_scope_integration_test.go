package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// scopeStub plays IAM: each employee sees exactly the ids the test assigns.
type scopeStub struct{ views map[int64]Visibility }

func (s *scopeStub) VisibleEmployees(_ context.Context, employeeID int64, _ string) (Visibility, error) {
	if v, ok := s.views[employeeID]; ok {
		return v, nil
	}
	return Visibility{EmployeeIDs: []int64{employeeID}, ScopeType: "SELF"}, nil
}

// What this pins down: anyone with purchase-order read permission shares the
// tenant-wide order list, while mutations remain fenced by buyer scope. A
// cross-buyer mutation still answers not-found because confirming that the
// order exists is itself information.
func TestPurchaseOrderDataScope(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed scope test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()

	const buyerA, buyerB = 71, 72
	mkOrder := func(no string, buyer int64) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,$2,9,'SUP-9','Mill','USD',100,current_date+10,'ORDERED',$3,'Buyer',now()) RETURNING id`, tenantID, no, buyer).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	orderA := mkOrder("PO-SCOPE-A", buyerA)
	orderB := mkOrder("PO-SCOPE-B", buyerB)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_invoice_lines WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_invoices WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
	}()

	stub := &scopeStub{views: map[int64]Visibility{
		buyerA: {EmployeeIDs: []int64{buyerA}, ScopeType: "SELF"},
		buyerB: {All: true, ScopeType: "ALL"},
	}}
	svc := New(pool, Deps{Scopes: stub})
	opA := Operator{ID: buyerA, Name: "A"}
	opB := Operator{ID: buyerB, Name: "B"}

	// The purchase-order list is shared tenant-wide, even when IAM reports SELF.
	rows, total, err := svc.ListOrders(ctx, tenantID, OrderFilter{}, 1, 20, opA)
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("shared list: want 2 orders, got total=%d rows=%d (%v)", total, len(rows), err)
	}
	seen := map[int64]bool{}
	for _, row := range rows {
		seen[row.ID] = true
	}
	if !seen[orderA] || !seen[orderB] {
		t.Fatalf("shared list: want order ids %d and %d, got %+v", orderA, orderB, seen)
	}
	if _, total, err = svc.ListOrders(ctx, tenantID, OrderFilter{}, 1, 20, opB); err != nil || total != 2 {
		t.Fatalf("ALL list: want 2, got %d (%v)", total, err)
	}

	// The other buyer's order answers not-found, indistinguishable from absent.
	if err := svc.AuthorizeOrder(ctx, tenantID, orderB, opA); err == nil ||
		!strings.Contains(err.Error(), "PO_ORDER_NOT_FOUND") {
		t.Fatalf("authorize across buyers must read as not-found, got %v", err)
	}
	if err := svc.AuthorizeOrder(ctx, tenantID, orderA, opA); err != nil {
		t.Fatalf("own order must authorize: %v", err)
	}

	// Invoices fence on who entered them, same module.
	inv, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-SCOPE-1",
		Currency: "USD", TotalAmount: "100", InvoiceDate: "2026-08-21",
	}, opA)
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := svc.ListSupplierInvoices(ctx, tenantID, SupplierInvoiceFilter{}, 1, 20, opB)
	if err != nil || len(items) != 1 {
		t.Fatalf("ALL sees the invoice: %d (%v)", len(items), err)
	}
	opC := Operator{ID: 73, Name: "C"} // stub default: SELF, sees only own
	if items, _, err = svc.ListSupplierInvoices(ctx, tenantID, SupplierInvoiceFilter{}, 1, 20, opC); err != nil || len(items) != 0 {
		t.Fatalf("stranger must see no invoices, got %d (%v)", len(items), err)
	}
	if err := svc.AuthorizeSupplierInvoice(ctx, tenantID, inv.ID, opC); err == nil ||
		!strings.Contains(err.Error(), "INV_NOT_FOUND") {
		t.Fatalf("stranger's authorize must read as not-found, got %v", err)
	}

	// Void goes through the same fence: a stranger cannot void what they
	// cannot see, and the refusal must not reveal that it exists.
	if _, err := svc.VoidSupplierInvoice(ctx, tenantID, inv.ID, "试图越权", opC); err == nil ||
		!strings.Contains(err.Error(), "INV_NOT_FOUND") {
		t.Fatalf("cross-scope void must read as not-found, got %v", err)
	}
}
