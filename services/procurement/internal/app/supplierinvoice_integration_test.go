package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// The invoice is the one document in the three-way match written by the
// other side. What this test pins down is the boundary of validation: a
// coherent claim must be recordable even if its amounts turn out wrong, and
// an incoherent one (someone else's PO, a lines/total gap, a duplicate
// number) must be refused at the door.
func TestSupplierInvoiceLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed invoice test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()

	var requirementID, orderID, itemID, otherOrderID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status) VALUES ($1,1,'CT-INV',1,$1,11,'P-11','Invoice Coil',7,'TON',10,10,'ORDERED') RETURNING id`, tenantID).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-INV-1',9,'SUP-9','Mill','USD',5200,current_date+10,'ORDERED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items (tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount) VALUES ($1,$2,$3,11,'P-11','Invoice Coil',7,'TON',10,520,5200) RETURNING id`, tenantID, orderID, requirementID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	// A second supplier's order: binding a line to it must be refused.
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-INV-2',8,'SUP-8','Somebody Else','USD',999,current_date+10,'ORDERED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&otherOrderID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_invoice_lines WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_invoices WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Buyer"}

	base := SupplierInvoiceInput{
		SupplierID: 9, SupplierCode: "SUP-9", SupplierName: "Mill",
		InvoiceNo: "FP-2026-001", InvoiceType: "VAT_SPECIAL", Currency: "usd",
		TotalAmount: "5200", InvoiceDate: "2026-08-20", DueDate: "2026-09-20",
		Lines: []SupplierInvoiceLineInput{
			{POItemID: itemID, Description: "Invoice Coil", Qty: "10", UnitPrice: "520", Amount: "5200"},
		},
	}

	inv, err := svc.CreateSupplierInvoice(ctx, tenantID, base, op)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Since P2 the verdict lands with the entry. Nothing has been received
	// against this order yet, so the honest verdict for a full-amount claim
	// is EXCEPTION — billed before delivery is exactly what the matcher is
	// for. PENDING now only means "the matcher could not run".
	if inv.Status != "OPEN" || inv.MatchStatus != "EXCEPTION" {
		t.Fatalf("fresh invoice should be OPEN/EXCEPTION (billed before goods), got %s/%s", inv.Status, inv.MatchStatus)
	}
	if inv.Currency != "USD" {
		t.Fatalf("currency should be uppercased, got %q", inv.Currency)
	}
	if len(inv.Lines) != 1 || inv.Lines[0].PONo != "PO-INV-1" || inv.Lines[0].POID != orderID {
		t.Fatalf("line should resolve its PO from the item: %+v", inv.Lines)
	}

	// Same paper twice is the classic double-payment opening.
	if _, err := svc.CreateSupplierInvoice(ctx, tenantID, base, op); err == nil ||
		!strings.Contains(err.Error(), "INV_DUPLICATE") {
		t.Fatalf("duplicate invoice number must be refused, got %v", err)
	}

	// A line bound to another supplier's order must be refused: that is how
	// one factory's paper quietly claims another factory's goods.
	crossed := base
	crossed.InvoiceNo = "FP-2026-002"
	crossed.Lines = []SupplierInvoiceLineInput{{POID: otherOrderID, Amount: "5200"}}
	if _, err := svc.CreateSupplierInvoice(ctx, tenantID, crossed, op); err == nil ||
		!strings.Contains(err.Error(), "INV_LINE_SUPPLIER_MISMATCH") {
		t.Fatalf("cross-supplier binding must be refused, got %v", err)
	}

	// Lines that do not reproduce the header total are a typo caught at
	// entry, not a discrepancy for the matcher.
	gap := base
	gap.InvoiceNo = "FP-2026-003"
	gap.TotalAmount = "5300"
	if _, err := svc.CreateSupplierInvoice(ctx, tenantID, gap, op); err == nil ||
		!strings.Contains(err.Error(), "INV_LINES_TOTAL_MISMATCH") {
		t.Fatalf("lines/total gap must be refused, got %v", err)
	}

	// An invoice with NO lines is legal — a freight bill binds to nothing —
	// and the amounts are recorded as claimed, not judged.
	freight := SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-2026-004",
		Currency: "USD", TotalAmount: "300", InvoiceDate: "2026-08-20",
	}
	if _, err := svc.CreateSupplierInvoice(ctx, tenantID, freight, op); err != nil {
		t.Fatalf("a lineless charge invoice must be recordable: %v", err)
	}

	items, listTotal, err := svc.ListSupplierInvoices(ctx, tenantID, SupplierInvoiceFilter{}, 1, 20)
	if err != nil || listTotal != 2 || len(items) != 2 {
		t.Fatalf("list: want 2 invoices, got %d (%v)", listTotal, err)
	}
	filtered, _, err := svc.ListSupplierInvoices(ctx, tenantID, SupplierInvoiceFilter{Keyword: "FP-2026-004"}, 1, 20)
	if err != nil || len(filtered) != 1 {
		t.Fatalf("keyword filter: want 1, got %d (%v)", len(filtered), err)
	}

	// Void keeps the paper on record with its reason; a second void must
	// fail, because "already decided" is a state, not a repeatable action.
	if _, err := svc.VoidSupplierInvoice(ctx, tenantID, inv.ID, "", op); err == nil {
		t.Fatal("void without a reason must be refused")
	}
	voided, err := svc.VoidSupplierInvoice(ctx, tenantID, inv.ID, "录错供应商", op)
	if err != nil || voided.Status != "VOID" || voided.VoidReason == "" {
		t.Fatalf("void: %v %+v", err, voided)
	}
	if _, err := svc.VoidSupplierInvoice(ctx, tenantID, inv.ID, "再来一次", op); err == nil ||
		!strings.Contains(err.Error(), "INV_NOT_OPEN") {
		t.Fatalf("double void must be refused, got %v", err)
	}
}
