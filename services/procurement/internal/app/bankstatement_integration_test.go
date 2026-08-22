package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// The bank statement is the one voice in the reconciliation written by a
// machine that does not care what anybody meant. What this test pins down:
// imports dedupe on the bank's own reference so re-uploads are harmless,
// the suggestion finds the obvious counterpart without acting on it, the
// match guards hold (currency, direction, one-payment-per-row), and
// unmatching touches only our side of the story.
func TestBankStatementLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed bank statement test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_payments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Finance"}

	// A payment recorded three days before the wire shows on the statement.
	pay, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "10000", PaidAt: "2026-08-17", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	csv := "交易日期,借贷,金额,币种,对方户名,流水号\n" +
		"2026-08-20,借,10000.00,USD,鞍山钢厂,BK-" + itoa64(tenantID) + "-1\n" +
		"2026-08-21,贷,5000.00,USD,海外客户,BK-" + itoa64(tenantID) + "-2\n" +
		"坏日期,借,1,USD,x,BK-" + itoa64(tenantID) + "-3\n"
	summary, err := svc.ImportBankStatement(ctx, tenantID, "aug.csv", []byte(csv), "", op)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Imported != 2 || summary.Duplicates != 0 || len(summary.Errors) != 1 {
		t.Fatalf("first import: %+v", summary)
	}

	// Last week's file again: nothing doubles, nothing breaks.
	again, err := svc.ImportBankStatement(ctx, tenantID, "aug.csv", []byte(csv), "", op)
	if err != nil || again.Imported != 0 || again.Duplicates != 2 {
		t.Fatalf("re-upload must be harmless: %+v %v", again, err)
	}

	items, total, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op)
	if err != nil || total != 2 {
		t.Fatalf("list: %v total=%d", err, total)
	}
	var debit, credit *BankTransactionView
	for i := range items {
		if items[i].Direction == "DEBIT" {
			debit = &items[i]
		} else {
			credit = &items[i]
		}
	}
	// The 10000 USD wire three days from the payment is the obvious
	// counterpart — suggested, never auto-acted.
	if debit == nil || debit.SuggestedPaymentID != pay.ID {
		t.Fatalf("the wire should suggest its payment: %+v", debit)
	}
	if debit.MatchedPaymentID != 0 {
		t.Fatalf("a suggestion is not a match: %+v", debit)
	}
	if credit.SuggestedPaymentID != 0 {
		t.Fatalf("incoming money suggests no supplier payment: %+v", credit)
	}

	// Money that arrived cannot confirm money that left.
	if err := svc.MatchBankTransaction(ctx, tenantID, credit.ID, pay.ID, op); err == nil ||
		!strings.Contains(err.Error(), "BANK_TXN_DIRECTION") {
		t.Fatalf("credit row must refuse to match a payment, got %v", err)
	}

	if err := svc.MatchBankTransaction(ctx, tenantID, debit.ID, pay.ID, op); err != nil {
		t.Fatalf("match: %v", err)
	}
	got, err := svc.GetSupplierPayment(ctx, tenantID, pay.ID)
	if err != nil || got.BankRef != debit.BankRef {
		t.Fatalf("the payment should carry the bank's reference: %+v %v", got, err)
	}

	// One payment per bank row, one bank row per payment.
	pay2, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "10000", PaidAt: "2026-08-19", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, debit.ID, pay2.ID, op); err == nil {
		t.Fatal("a claimed bank row must refuse a second payment")
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, credit.ID, pay.ID, op); err == nil {
		t.Fatal("a matched payment must refuse a second row")
	}

	items, _, _ = svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{Status: "MATCHED"}, 1, 20, op)
	if len(items) != 1 || items[0].MatchedPaymentNo != got.PaymentNo {
		t.Fatalf("matched filter should show the claim: %+v", items)
	}

	// Withdrawing the judgement frees both sides; the bank row is untouched.
	if err := svc.UnmatchBankTransaction(ctx, tenantID, debit.ID, op); err != nil {
		t.Fatalf("unmatch: %v", err)
	}
	got, _ = svc.GetSupplierPayment(ctx, tenantID, pay.ID)
	if got.BankRef != "" {
		t.Fatalf("unmatch should release the payment, got %q", got.BankRef)
	}
	if err := svc.UnmatchBankTransaction(ctx, tenantID, debit.ID, op); err == nil ||
		!strings.Contains(err.Error(), "BANK_TXN_NOT_MATCHED") {
		t.Fatalf("double unmatch must say so, got %v", err)
	}

	// The account is not divisible by clerk: partial scope sees nothing.
	fenced := New(pool, Deps{Scopes: fixedScope{Visibility{All: false, ScopeType: "SELF"}}})
	if _, _, err := fenced.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op); err == nil ||
		!strings.Contains(err.Error(), "PR_RECON_SCOPE_LIMITED") {
		t.Fatalf("partial scope must be refused, got %v", err)
	}
}

func itoa64(v int64) string { return time.Unix(0, v).Format("150405.000000000") }
