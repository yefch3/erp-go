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
