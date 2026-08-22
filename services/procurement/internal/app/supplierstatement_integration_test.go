package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// allScope / selfScope pin the statement's all-or-nothing fence.
type fixedScope struct{ v Visibility }

func (f fixedScope) VisibleEmployees(context.Context, int64, string) (Visibility, error) {
	return f.v, nil
}

// The reconciliation view derives every number at read time. What this test
// pins down is the arithmetic of the whole book on one supplier: order,
// receipt, open exception, invoice, deposit parked on the order, settlement
// against the invoice, one reversal — and that the summary, the ledger's
// running balance, and the void guard all tell the same story.
func TestSupplierStatement(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed statement test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"payment_allocations", "supplier_payments",
			"supplier_invoice_lines", "supplier_invoices", "purchase_receipt_exceptions",
			"purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Buyer"}

	// One committed order: 10 t at 520 = 5200, of which 8 received and 1 in
	// an open short-shipment dispute. A DRAFT order rides along to prove
	// intentions are not counted as promises.
	var requirementID, orderID, itemID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status) VALUES ($1,1,'CT-ST',1,$1,11,'P-11','Coil',7,'TON',10,10,'ORDERED') RETURNING id`, tenantID).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-ST-1',9,'SUP-9','Mill','USD',5200,current_date+10,'ORDERED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items (tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount,received_qty) VALUES ($1,$2,$3,11,'P-11','Coil',7,'TON',10,520,5200,8) RETURNING id`, tenantID, orderID, requirementID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name) VALUES ($1,'PO-ST-DRAFT',9,'Mill','USD',99999,current_date+10,'DRAFT',77,'Buyer')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO purchase_receipt_exceptions (tenant_id,po_id,po_item_id,exception_type,qty,description,status,reported_by_id) VALUES ($1,$2,$3,'SHORT_SHIPMENT',1,'one ton short','OPEN',77)`, tenantID, orderID, itemID); err != nil {
		t.Fatal(err)
	}

	// The factory bills 4160 (8 t at our price), due yesterday → overdue.
	inv, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-ST-001",
		Currency: "USD", TotalAmount: "4160", InvoiceDate: "2026-08-01",
		DueDate: time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	// 3000 wired: 1000 parked on the order as a deposit, 2000 settles the
	// invoice, then 500 of the deposit is pulled back (a reversal, so the
	// ledger keeps both the mistake and the correction).
	pay, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "ADVANCE",
		Currency: "USD", Amount: "3000", PaidAt: "2026-08-05", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	park, err := svc.AllocateSupplierPayment(ctx, tenantID, pay.ID,
		[]PaymentAllocationInput{{POID: orderID, Amount: "1000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AllocateSupplierPayment(ctx, tenantID, pay.ID,
		[]PaymentAllocationInput{{InvoiceID: inv.ID, Amount: "2000"}}, op); err != nil {
		t.Fatal(err)
	}
	var parkAllocID int64
	for _, a := range park.Allocations {
		if a.POID == orderID {
			parkAllocID = a.ID
		}
	}
	if _, err = svc.ReverseSupplierPaymentAllocation(ctx, tenantID, parkAllocID, "wrong order", op); err != nil {
		t.Fatalf("reverse park: %v", err)
	}

	items, err := svc.ListSupplierStatements(ctx, tenantID, "", op)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("one supplier, one currency, one row — got %d", len(items))
	}
	v := items[0]
	// ordered 5200 (draft's 99999 must not count); received 8×520=4160;
	// exception 1×520=520; invoiced 4160; paid to invoice 2000;
	// advance parked 1000−1000=0 after the reversal;
	// unallocated 3000 − (1000 + 2000 − 1000) = 1000; balance 4160−2000=2160.
	assertMoney(t, "ordered", v.OrderedAmount, "5200")
	assertMoney(t, "received", v.ReceivedAmount, "4160")
	assertMoney(t, "exception", v.ExceptionAmount, "520")
	assertMoney(t, "invoiced", v.InvoicedAmount, "4160")
	assertMoney(t, "paid", v.PaidAmount, "2000")
	assertMoney(t, "advance", v.AdvanceAmount, "0")
	assertMoney(t, "unallocated", v.UnallocatedAmount, "1000")
	assertMoney(t, "balance", v.Balance, "2160")
	if v.OverdueCount != 1 {
		t.Fatalf("overdue count: want 1 got %d", v.OverdueCount)
	}
	assertMoney(t, "overdue amount", v.OverdueAmount, "2160")

	// The drill-in must tell the same story, ending on the same balance.
	summary, lines, err := svc.GetSupplierStatement(ctx, tenantID, 9, "usd", op)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Balance != v.Balance {
		t.Fatalf("summary and list disagree: %s vs %s", summary.Balance, v.Balance)
	}
	// invoice + park + settle + park-reversal = 4 lines
	if len(lines) != 4 {
		t.Fatalf("want 4 ledger lines, got %d: %+v", len(lines), lines)
	}
	if last := lines[len(lines)-1]; last.Balance != "2160.00" {
		t.Fatalf("running balance must end where the summary does, got %s", last.Balance)
	}
	reversals := 0
	for _, l := range lines {
		if l.Type == "ADVANCE_REVERSAL" {
			reversals++
		}
	}
	if reversals != 1 {
		t.Fatalf("the correction must stay visible in the ledger: %+v", lines)
	}

	// An invoice with live allocations must refuse to void — "the claim
	// doesn't count but the settlement stands" is not a state the book holds.
	if _, err := svc.VoidSupplierInvoice(ctx, tenantID, inv.ID, "trying anyway", op); err == nil ||
		!strings.Contains(err.Error(), "INV_HAS_ALLOCATIONS") {
		t.Fatalf("void with live allocations must be refused, got %v", err)
	}

	// The fence: a SELF-scoped caller gets a refusal, not a shrunken sum.
	fenced := New(pool, Deps{Scopes: fixedScope{Visibility{All: false, EmployeeIDs: []int64{77}, ScopeType: "SELF"}}})
	if _, err := fenced.ListSupplierStatements(ctx, tenantID, "", op); err == nil ||
		!strings.Contains(err.Error(), "PR_RECON_SCOPE_LIMITED") {
		t.Fatalf("partial-scope caller must be refused, got %v", err)
	}
}

func assertMoney(t *testing.T, name, got, want string) {
	t.Helper()
	if strings.TrimRight(strings.TrimRight(got, "0"), ".") != want &&
		got != want && got != want+".00" {
		t.Fatalf("%s: want %s got %s", name, want, got)
	}
}
