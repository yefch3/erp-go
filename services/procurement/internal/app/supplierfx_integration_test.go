package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// stubRates quotes units-per-USD like the real fx service, and can be
// re-priced mid-test — the whole point of a snapshot is that the rate moved
// between the invoice and the payment.
type stubRates struct{ perUSD map[string]string }

func (r *stubRates) Latest(_ context.Context, currency string) (Rate, error) {
	v, ok := r.perUSD[currency]
	if !ok {
		v = "1" // USD quotes itself
	}
	d, _ := decimal.NewFromString(v)
	return Rate{Rate: d, At: time.Unix(0, 0), Source: "STUB", Base: "USD"}, nil
}

// The fx story (A4 P6): a USD claim booked while CNY stood at 7.3, settled
// with cash that left while it stood at 7.2. What this test pins down is
// that both snapshots are stamped at entry — not recomputed at read — and
// that the realized difference lands as allocation × (invoice − payment).
func TestSupplierFxSnapshots(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed fx test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"payment_allocations", "supplier_payments", "supplier_invoices"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	rates := &stubRates{perUSD: map[string]string{"CNY": "7.3"}}
	svc := New(pool, Deps{Rates: rates})
	op := Operator{ID: 77, Name: "Buyer"}

	inv, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-FX-001",
		Currency: "USD", TotalAmount: "4160", InvoiceDate: "2026-08-01",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	assertDecimal(t, "invoice fx_rate", inv.FxRate, "7.3")
	assertDecimal(t, "invoice base", inv.BaseAmount, "30368") // 4160 × 7.3
	if inv.BaseCurrency != "CNY" {
		t.Fatalf("book currency should default to CNY, got %q", inv.BaseCurrency)
	}

	// The rate moves before the cash leaves.
	rates.perUSD["CNY"] = "7.2"
	pay, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "3000", PaidAt: "2026-08-10", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	assertDecimal(t, "payment fx_rate", pay.FxRate, "7.2")
	assertDecimal(t, "payment base", pay.BaseAmount, "21600")

	if _, err = svc.AllocateSupplierPayment(ctx, tenantID, pay.ID,
		[]PaymentAllocationInput{{InvoiceID: inv.ID, Amount: "2000"}}, op); err != nil {
		t.Fatal(err)
	}

	items, err := svc.ListSupplierStatements(ctx, tenantID, "", op)
	if err != nil || len(items) != 1 {
		t.Fatalf("one statement row expected: %v %d", err, len(items))
	}
	v := items[0]
	if v.BaseCurrency != "CNY" {
		t.Fatalf("statement book currency: got %q", v.BaseCurrency)
	}
	assertDecimal(t, "invoiced base", v.InvoicedBase, "30368")
	assertDecimal(t, "paid base", v.PaidBase, "14400") // 2000 × 7.2
	// Booked at 7.3, settled at 7.2: the 2000 USD cost 200 CNY less than
	// the claim was worth when entered — a realized gain.
	assertDecimal(t, "fx gain", v.FxGainLoss, "200")

	// A book-currency payment converts at exactly 1 without asking fx.
	cny, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "ADVANCE",
		Currency: "CNY", Amount: "500", PaidAt: "2026-08-11", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	assertDecimal(t, "CNY fx_rate", cny.FxRate, "1")
	assertDecimal(t, "CNY base", cny.BaseAmount, "500")

	// Without an fx service the snapshot is zero — "not captured", never a
	// fabricated conversion — and gain/loss quietly excludes such rows.
	bare := New(pool, Deps{})
	blind, err := bare.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-FX-002",
		Currency: "USD", TotalAmount: "100", InvoiceDate: "2026-08-12",
	}, op)
	if err != nil {
		t.Fatalf("fx being unreachable must not block recording a fact: %v", err)
	}
	assertDecimal(t, "blind fx_rate", blind.FxRate, "0")
	assertDecimal(t, "blind base", blind.BaseAmount, "0")
}

func assertDecimal(t *testing.T, name, got, want string) {
	t.Helper()
	g, err1 := decimal.NewFromString(got)
	w, err2 := decimal.NewFromString(want)
	if err1 != nil || err2 != nil || !g.Equal(w) {
		t.Fatalf("%s: want %s got %s", name, want, got)
	}
}
