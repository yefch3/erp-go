package app

import (
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
)

func TestReimbursementExpenseValidation(t *testing.T) {
	base := TravelReimbursementInput{TripStart: "2026-11-16", TripEnd: "2026-11-17", Origin: " 上海 ", Destination: " 北京 ", Purpose: " 办公费用 ", Amount: "12.5", Currency: "cny", PaymentAccount: " Test "}
	cases := []struct{ name, kind, date, code string }{
		{"legacy travel", "", "", ""}, {"explicit travel", "TRAVEL", "", ""},
		{"office supplies", "OFFICE_SUPPLIES", "2026-11-17", ""}, {"other expenses", "OTHER", "2026-11-17", ""},
		{"office needs expense date", "OFFICE_SUPPLIES", "", "PR_EXPENSE_DATE_INVALID"},
		{"invalid date", "OTHER", "2026-02-30", "PR_EXPENSE_DATE_INVALID"},
		{"unknown type", "MEALS", "2026-11-17", "PR_EXPENSE_TYPE_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			in.ExpenseType = tc.kind
			in.ExpenseDate = tc.date
			err := validateTravelInput(&in)
			if apierr.CodeFromError(err) != tc.code {
				t.Fatalf("err=%v, want %s", err, tc.code)
			}
			if err != nil {
				return
			}
			if in.ExpenseType == "TRAVEL" {
				if in.TripStart != base.TripStart || in.ExpenseDate != "" {
					t.Fatalf("travel=%+v", in)
				}
			} else {
				if in.TripStart != "" || in.TripEnd != "" || in.Origin != "" || in.Destination != "" || in.ExpenseDate != tc.date {
					t.Fatalf("nontravel=%+v", in)
				}
			}
			if in.Amount != "12.50" || in.Currency != "CNY" || in.Purpose != "办公费用" || in.PaymentAccount != "Test" {
				t.Fatalf("normalization=%+v", in)
			}
		})
	}
	for _, change := range []func(*TravelReimbursementInput){
		func(in *TravelReimbursementInput) { in.TripEnd = "2026-11-15" }, func(in *TravelReimbursementInput) { in.Origin = " " }, func(in *TravelReimbursementInput) { in.Destination = "" }, func(in *TravelReimbursementInput) { in.TripStart = "" },
		func(in *TravelReimbursementInput) { in.Purpose = " " }, func(in *TravelReimbursementInput) { in.Amount = "0" }, func(in *TravelReimbursementInput) { in.Amount = "1.001" }, func(in *TravelReimbursementInput) { in.PaymentAccount = " " },
	} {
		in := base
		change(&in)
		if err := validateTravelInput(&in); err == nil {
			t.Fatalf("accepted invalid travel: %+v", in)
		}
	}
	for _, change := range []func(*TravelReimbursementInput){func(in *TravelReimbursementInput) { in.Purpose = " " }, func(in *TravelReimbursementInput) { in.Amount = "-1" }, func(in *TravelReimbursementInput) { in.PaymentAccount = "" }} {
		in := base
		in.ExpenseType = "OTHER"
		in.ExpenseDate = "2026-11-17"
		change(&in)
		if err := validateTravelInput(&in); err == nil {
			t.Fatalf("accepted invalid expense: %+v", in)
		}
	}
}
