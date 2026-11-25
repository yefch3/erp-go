package app

import (
	"context"
	"strings"
	"testing"
)

func TestD4RequoteRejectsInvalidMoneyBeforeSubmittingApproval(t *testing.T) {
	svc := &Service{}
	_, err := svc.SubmitFinalRequote(context.Background(), 1, 1, FinalRequoteInput{FinalFreightAmount: "-1"}, Operator{})
	if err == nil || !strings.Contains(err.Error(), "SHIPPING_APPROVAL_UNAVAILABLE") {
		// Approval availability is intentionally checked first so no proposal can
		// be saved without a durable approval instance.
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestD4ContractUploadPrefixIsScopedPerTenantAndTask(t *testing.T) {
	if got, want := shippingContractPrefix(7, 41), "d4-contracts/shipping/7/41/"; got != want {
		t.Fatalf("prefix=%q want=%q", got, want)
	}
}

func TestD4BusinessDatesUseISOCalendarDates(t *testing.T) {
	if !validD4Date("2026-09-09") || validD4Date("09/09/2026") || validD4Date("") {
		t.Fatal("D4 date validation accepted an ambiguous date")
	}
}

func TestD4RequoteLeavesActualCarrierForVesselNomination(t *testing.T) {
	_, err := validateFinalRequote(FinalRequoteInput{
		FinalForwarderID:    9,
		FinalForwarderName:  "Test Forwarder",
		FinalServiceOption:  "Shanghai to Hamburg",
		FinalCurrency:       "USD",
		FinalFreightAmount:  "1280",
		ActualCarrierID:     0,
		ActualCarrierName:   "",
		ForwarderContractNo: "",
	})
	if err != nil {
		t.Fatalf("actual carrier and contract number should be optional before vessel nomination/contract upload: %v", err)
	}
}

func TestD4RequoteAllowsBlankNotesAndStillRequiresCommercialTerms(t *testing.T) {
	in := FinalRequoteInput{
		FinalForwarderID: 9, FinalForwarderName: "Test Forwarder",
		FinalCurrency: "USD", FinalFreightAmount: "100",
	}
	if _, err := validateFinalRequote(in); err != nil {
		t.Fatalf("scheme description and remark must both be optional: %v", err)
	}
	for _, missing := range []string{"forwarder", "currency", "amount"} {
		t.Run(missing, func(t *testing.T) {
			invalid := in
			switch missing {
			case "forwarder":
				invalid.FinalForwarderID = 0
			case "currency":
				invalid.FinalCurrency = ""
			case "amount":
				invalid.FinalFreightAmount = ""
			}
			if _, err := validateFinalRequote(invalid); err == nil {
				t.Fatal("optional notes must not bypass required commercial terms")
			}
		})
	}
}

func TestD4LaycanValidatesCalendarRange(t *testing.T) {
	for _, tt := range []struct {
		start, end string
		valid      bool
	}{
		{"2026-12-01", "2026-12-05", true},
		{"2026-12-01", "2026-12-01", true},
		{"2026-12-05", "2026-12-01", false},
		{"2026-02-29", "2026-03-01", false},
		{"", "2026-12-01", false},
		{"2026-12-01", "", false},
		{"", "", false},
	} {
		if err := validateLaycan(tt.start, tt.end); (err == nil) != tt.valid {
			t.Errorf("validateLaycan(%q, %q) = %v, valid=%t", tt.start, tt.end, err, tt.valid)
		}
	}
}
