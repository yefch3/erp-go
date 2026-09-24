package app

import (
	"context"
	"testing"
)

func ptr(s string) *string { return &s }

func TestBasisValues(t *testing.T) {
	for _, tc := range []struct {
		spot, future   *string
		basis, percent *string
	}{
		{ptr("3250"), ptr("3177"), ptr("73"), ptr("2.30")},
		{ptr("27400"), ptr("27505"), ptr("-105"), ptr("-0.38")},
		{ptr("100"), ptr("0"), ptr("100"), nil},
		{ptr("100"), nil, nil, nil},
		{nil, ptr("50"), nil, nil},
	} {
		basis, percent := basisValues(tc.spot, tc.future)
		if !equalOptional(basis, tc.basis) || !equalOptional(percent, tc.percent) {
			t.Fatalf("basisValues(%v,%v) = %v,%v; want %v,%v", tc.spot, tc.future, basis, percent, tc.basis, tc.percent)
		}
	}
}

func equalOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestDailyNumberRejectsInvalidPrice(t *testing.T) {
	for _, value := range []string{"-1", "1.23456", "not a number", "100000000000000"} {
		if _, err := dailyNumber(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	if n, err := dailyNumber("763.8"); err != nil || n.String() != "763.8" {
		t.Fatalf("valid price: %v, %v", n, err)
	}
	if n, err := dailyNumber(""); err != nil || n != nil {
		t.Fatalf("empty price: %v, %v", n, err)
	}
}

type deniedDailyPermission struct{ code string }

func (d *deniedDailyPermission) Allowed(_ context.Context, _ int64, code string) (bool, error) {
	d.code = code
	return false, nil
}

func TestDailyPriceChecksPermissionBeforeData(t *testing.T) {
	for _, tc := range []struct{ action, want string }{
		{"day", "procurement:daily-price:read"},
		{"savePrices", "procurement:daily-price:write"},
		{"configure", "procurement:daily-price:manage"},
		{"deletePrice", "procurement:daily-price:delete"},
	} {
		checker := &deniedDailyPermission{}
		svc := &Service{permissions: checker}
		if _, err := svc.DailyPrice(context.Background(), 1, Operator{ID: 7}, DailyPriceCommand{Action: tc.action}); err == nil {
			t.Errorf("%s passed without permission", tc.action)
		}
		if checker.code != tc.want {
			t.Errorf("%s checked %s, want %s", tc.action, checker.code, tc.want)
		}
	}
}
