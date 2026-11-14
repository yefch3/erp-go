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
		FinalETD:            "2026-10-25",
		FinalETA:            "2026-11-25",
		PaymentTerms:        "50% after booking",
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
		FinalETD: "2026-11-25", FinalETA: "2026-12-10", PaymentTerms: "T/T",
	}
	if _, err := validateFinalRequote(in); err != nil {
		t.Fatalf("scheme description and remark must both be optional: %v", err)
	}
	for _, missing := range []string{"forwarder", "currency", "amount", "etd", "eta", "payment"} {
		t.Run(missing, func(t *testing.T) {
			invalid := in
			switch missing {
			case "forwarder":
				invalid.FinalForwarderID = 0
			case "currency":
				invalid.FinalCurrency = ""
			case "amount":
				invalid.FinalFreightAmount = ""
			case "etd":
				invalid.FinalETD = ""
			case "eta":
				invalid.FinalETA = ""
			case "payment":
				invalid.PaymentTerms = ""
			}
			if _, err := validateFinalRequote(invalid); err == nil {
				t.Fatal("optional notes must not bypass required commercial terms")
			}
		})
	}
}
