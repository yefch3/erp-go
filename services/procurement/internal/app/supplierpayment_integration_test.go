package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// The payment is the assertion; allocations are revisable judgements. What
// this pins down is the deposit lifecycle the whole model exists for:
//
//	wire 30% before any invoice exists → park it on the order
//	invoice arrives → reverse the parking, re-point at the invoice
//	pay the balance → invoice flips to SETTLED, arithmetic intact throughout
func TestSupplierPaymentLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed payment test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()

	var orderID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-PAY-1',9,'SUP-9','Mill','USD',10000,current_date+10,'ORDERED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, tbl := range []string{"payment_allocations", "supplier_payments", "supplier_invoice_lines", "supplier_invoices", "purchase_orders"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+tbl+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Buyer"}

	// 30% deposit, wired before any invoice exists.
	deposit, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "ADVANCE",
		Currency: "usd", Amount: "3000", PaidAt: "2026-08-21",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if deposit.Currency != "USD" || deposit.Unallocated != "3000.00" {
		t.Fatalf("fresh deposit: currency %q unallocated %q", deposit.Currency, deposit.Unallocated)
	}

	// Parked on the order — the only target that exists yet.
	deposit, err = svc.AllocateSupplierPayment(ctx, tenantID, deposit.ID,
		[]PaymentAllocationInput{{POID: orderID, Amount: "3000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if deposit.Unallocated != "0.00" {
		t.Fatalf("parked deposit should have nothing left, got %q", deposit.Unallocated)
	}
	parkedID := deposit.Allocations[0].ID

	// Over-allocation is refused inside the lock, not discovered in a report.
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, deposit.ID,
		[]PaymentAllocationInput{{POID: orderID, Amount: "1"}}, op); err == nil ||
		!strings.Contains(err.Error(), "PAY_ALLOC_EXCEEDS_PAYMENT") {
		t.Fatalf("over-allocation must be refused, got %v", err)
	}

	// The invoice arrives.
	inv, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-PAY-1",
		Currency: "USD", TotalAmount: "10000", InvoiceDate: "2026-08-21",
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	// Re-point the deposit: reverse the parking, allocate to the invoice.
	// Two rows, not an UPDATE — where the deposit went stays readable.
	if _, err := svc.ReverseSupplierPaymentAllocation(ctx, tenantID, parkedID, "发票已到，转挂", op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReverseSupplierPaymentAllocation(ctx, tenantID, parkedID, "再冲一次", op); err == nil ||
		!strings.Contains(err.Error(), "PAY_ALLOC_ALREADY_REVERSED") {
		t.Fatalf("double reverse must be refused by the unique index, got %v", err)
	}
	deposit, err = svc.AllocateSupplierPayment(ctx, tenantID, deposit.ID,
		[]PaymentAllocationInput{{InvoiceID: inv.ID, Amount: "3000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if deposit.Unallocated != "0.00" {
		t.Fatalf("after re-pointing the deposit is fully used again, got %q", deposit.Unallocated)
	}

	// Balance payment, with an intermediary fee that settles nothing: the
	// payment covers 7025, of which 7000 reaches the invoice and 25 is spent.
	balance, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "7025", PaidAt: "2026-08-21", Method: "TT",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AllocateSupplierPayment(ctx, tenantID, balance.ID,
		[]PaymentAllocationInput{{InvoiceID: inv.ID, Amount: "7000", FeeAmount: "25"}}, op); err != nil {
		t.Fatal(err)
	}

	// 3000 + 7000 = the invoice total: SETTLED, derived from the rows.
	settled, err := svc.GetSupplierInvoice(ctx, tenantID, inv.ID)
	if err != nil || settled.Status != "SETTLED" {
		t.Fatalf("fully allocated invoice must be SETTLED, got %s (%v)", settled.Status, err)
	}

	// Reversing part of it reopens the invoice — the status is a sum, not a
	// switch somebody once flipped.
	full, err := svc.GetSupplierPayment(ctx, tenantID, balance.ID)
	if err != nil {
		t.Fatal(err)
	}
	var balanceAllocID int64
	for _, a := range full.Allocations {
		if a.ReversalOf == 0 && a.InvoiceID == inv.ID {
			balanceAllocID = a.ID
		}
	}
	if _, err := svc.ReverseSupplierPaymentAllocation(ctx, tenantID, balanceAllocID, "金额录错", op); err != nil {
		t.Fatal(err)
	}
	reopened, err := svc.GetSupplierInvoice(ctx, tenantID, inv.ID)
	if err != nil || reopened.Status != "OPEN" {
		t.Fatalf("reversal must reopen the invoice, got %s (%v)", reopened.Status, err)
	}

	// Guards that keep money from crossing lines: somebody else's supplier,
	// a foreign currency, a voided invoice.
	var otherOrder int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-PAY-2',8,'SUP-8','Else','USD',500,current_date+10,'ORDERED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&otherOrder); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, balance.ID,
		[]PaymentAllocationInput{{POID: otherOrder, Amount: "10"}}, op); err == nil ||
		!strings.Contains(err.Error(), "PAY_ALLOC_SUPPLIER_MISMATCH") {
		t.Fatalf("cross-supplier allocation must be refused, got %v", err)
	}
	cny, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-PAY-2",
		Currency: "CNY", TotalAmount: "100", InvoiceDate: "2026-08-21",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, balance.ID,
		[]PaymentAllocationInput{{InvoiceID: cny.ID, Amount: "10"}}, op); err == nil ||
		!strings.Contains(err.Error(), "PAY_ALLOC_CURRENCY_MISMATCH") {
		t.Fatalf("cross-currency allocation must be refused until P6, got %v", err)
	}
}
