package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestSourcingReviewUsesLockedTemplateRequiredFields(t *testing.T) {
	fields := []store.ListInquiryTemplateFieldsRow{
		{FieldKey: "product", DisplayName: "产品", IsRequired: true},
		{FieldKey: "custom.customer_spec", DisplayName: "客户规格", IsRequired: true},
		{FieldKey: "remarks", DisplayName: "备注", IsRequired: false},
	}
	input := SourcingLineInput{Product: "不锈钢卷", CustomFields: map[string]string{}}
	if got, want := missingIntakeReviewFields(sourcingInputAsRow(input), fields), []string{"客户规格"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("缺失字段=%v, want %v", got, want)
	}

	input.CustomFields["custom.customer_spec"] = "食品级"
	if got := missingIntakeReviewFields(sourcingInputAsRow(input), fields); len(got) != 0 {
		t.Fatalf("完整动态规格不应被拦截: %v", got)
	}
}

func TestValidateFactoryRFQContact(t *testing.T) {
	today := time.Date(2026, time.August, 20, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		email    string
		currency string
		due      string
		wantErr  bool
	}{
		{name: "valid", email: "buyer@example.com", currency: "USD", due: "2026-08-21"},
		{name: "today is allowed", email: "buyer@example.com", currency: "CNY", due: "2026-08-20"},
		{name: "missing email", currency: "USD", due: "2026-08-21", wantErr: true},
		{name: "invalid email", email: "buyer", currency: "USD", due: "2026-08-21", wantErr: true},
		{name: "invalid currency", email: "buyer@example.com", currency: "US", due: "2026-08-21", wantErr: true},
		{name: "past due", email: "buyer@example.com", currency: "USD", due: "2026-08-19", wantErr: true},
		{name: "invalid due", email: "buyer@example.com", currency: "USD", due: "tomorrow", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFactoryRFQContact(tt.email, tt.currency, tt.due, today)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateFactoryRFQContact() error=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestRFQEligibleSourcingLineIDsKeepsReviewedLegacyLines(t *testing.T) {
	lines := []store.ListSourcingLinesRow{
		{ID: 10, Decision: "CONFIRMED"},
		{ID: 11, Decision: "PENDING"},
		{ID: 12, Decision: "SKIPPED"},
		{ID: 13, Decision: "CONFIRMED"},
	}
	if got, want := rfqEligibleSourcingLineIDs(lines), []int64{10, 11, 13}; !reflect.DeepEqual(got, want) {
		t.Fatalf("默认 RFQ 产品行=%v, want %v", got, want)
	}
}
