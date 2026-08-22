package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// The matcher judges the factory's claim against our order and our receipt,
// never against the claim's own unit price. Numbers here are chosen so every
// verdict is hand-checkable:
//
//	ordered 10 × 520 = 5200
//	received 8            → 8 × 520 = 4160
//	open SHORT_SHIPMENT 1  → payable qty 7 → payable 3640
func TestThreeWayMatch(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed match test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()

	var reqID, req2ID, orderID, itemID, item2ID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status) VALUES ($1,1,'CT-MATCH',1,$1,11,'P-11','Match Coil',7,'TON',10,10,'ORDERED') RETURNING id`, tenantID).Scan(&reqID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status) VALUES ($1,1,'CT-MATCH',1,$2,12,'P-12','Match Sheet',7,'TON',10,10,'ORDERED') RETURNING id`, tenantID, tenantID+1).Scan(&req2ID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-MATCH-1',9,'SUP-9','Mill','USD',5200,current_date+10,'PARTIALLY_RECEIVED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items (tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount,received_qty) VALUES ($1,$2,$3,11,'P-11','Match Coil',7,'TON',10,520,5200,8) RETURNING id`, tenantID, orderID, reqID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	// A second line for the tolerance cases: received 7, no exceptions, so
	// payable is 3640 with nothing billed against it yet. They cannot share
	// the first line — once it is fully billed, the cumulative rule
	// (correctly) flags any further paper, tolerance or not.
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items (tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount,received_qty) VALUES ($1,$2,$3,12,'P-12','Match Sheet',7,'TON',10,520,5200,7) RETURNING id`, tenantID, orderID, req2ID).Scan(&item2ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO purchase_receipt_exceptions (tenant_id,po_id,po_item_id,exception_type,qty,description,reported_by_id,reported_by_name) VALUES ($1,$2,$3,'SHORT_SHIPMENT',1,'短装一吨',77,'Buyer')`, tenantID, orderID, itemID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, tbl := range []string{"supplier_invoice_lines", "supplier_invoices", "purchase_receipt_exceptions", "purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+tbl+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Buyer"}
	mkInvoice := func(no string, item int64, total, lineAmount string) SupplierInvoice {
		inv, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
			SupplierID: 9, SupplierName: "Mill", InvoiceNo: no,
			Currency: "USD", TotalAmount: total, InvoiceDate: "2026-08-21",
			Lines: []SupplierInvoiceLineInput{
				{POItemID: item, Qty: "7", UnitPrice: "520", Amount: lineAmount},
			},
		}, op)
		if err != nil {
			t.Fatalf("create %s: %v", no, err)
		}
		return inv
	}

	// Claim equals payable → the verdict lands with the entry, already MATCHED.
	exact := mkInvoice("FP-M-1", itemID, "3640", "3640")
	if exact.MatchStatus != "MATCHED" {
		t.Fatalf("exact claim should MATCH on create, got %s (%s)", exact.MatchStatus, exact.MatchNote)
	}

	// The factory bills as if everything arrived intact: 5200 vs payable 3640.
	// EXCEPTION — recorded, explained, and deliberately not blocked.
	over, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-M-2",
		Currency: "USD", TotalAmount: "5200", InvoiceDate: "2026-08-21",
		Lines: []SupplierInvoiceLineInput{
			{POItemID: itemID, Qty: "10", UnitPrice: "520", Amount: "5200"},
		},
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if over.MatchStatus != "EXCEPTION" {
		t.Fatalf("over-claim must be EXCEPTION, got %s", over.MatchStatus)
	}
	for _, needle := range []string{"3640", "5200", "520"} {
		if !strings.Contains(over.MatchNote, needle) {
			t.Fatalf("the note must show the arithmetic (missing %q): %s", needle, over.MatchNote)
		}
	}
	// FP-M-2 also trips the cumulative rule: FP-M-1 already billed the full
	// payable, so anything more on the same line is double billing.
	if !strings.Contains(over.MatchNote, "累计") {
		t.Fatalf("cumulative over-billing must be named: %s", over.MatchNote)
	}

	// A charge on no order can never MATCH — but it records, because pushing
	// it into a spreadsheet is how it ends up reconciled by nobody.
	freight, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-M-3",
		Currency: "USD", TotalAmount: "300", InvoiceDate: "2026-08-21",
		Lines: []SupplierInvoiceLineInput{{Description: "海运费", Amount: "300"}},
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if freight.MatchStatus != "EXCEPTION" || !strings.Contains(freight.MatchNote, "订单外") {
		t.Fatalf("an off-order charge is an EXCEPTION with its nature named, got %s (%s)", freight.MatchStatus, freight.MatchNote)
	}

	// Tolerance turns near-misses back into MATCHED — min(pct, abs), the
	// ERPNext convention. 3645 vs 3640: inside 1% (36.4), outside abs 3.
	svc.UseMatchTolerance(decimal.RequireFromString("0.01"), decimal.RequireFromString("3"))
	near := mkInvoice("FP-M-4", item2ID, "3645", "3645")
	if near.MatchStatus != "EXCEPTION" {
		t.Fatalf("min(36.4, 3)=3 < 5: still EXCEPTION, got %s", near.MatchStatus)
	}
	svc.UseMatchTolerance(decimal.RequireFromString("0.01"), decimal.Zero)
	re, err := svc.MatchSupplierInvoice(ctx, tenantID, near.ID, op)
	if err != nil {
		t.Fatal(err)
	}
	if re.MatchStatus != "MATCHED" {
		t.Fatalf("within 1%% alone must MATCH on re-run, got %s (%s)", re.MatchStatus, re.MatchNote)
	}

	// The exception being resolved changes the arithmetic; a re-run must see
	// the new world. payable becomes 8 × 520 = 4160, so 5200 still differs
	// but the note must now say 4160.
	if _, err := pool.Exec(ctx, `UPDATE purchase_receipt_exceptions SET status='RESOLVED' WHERE tenant_id=$1`, tenantID); err != nil {
		t.Fatal(err)
	}
	svc.UseMatchTolerance(decimal.Zero, decimal.Zero)
	re2, err := svc.MatchSupplierInvoice(ctx, tenantID, over.ID, op)
	if err != nil {
		t.Fatal(err)
	}
	if re2.MatchStatus != "EXCEPTION" || !strings.Contains(re2.MatchNote, "4160") {
		t.Fatalf("re-run must reflect the resolved exception (want 4160 in note): %s (%s)", re2.MatchStatus, re2.MatchNote)
	}
}
