package app

import "testing"

func TestCustomerProfileDefaultsAndTags(t *testing.T) {
	in := CustomerInput{
		Currency: "usd",
		Tags:     []string{" 重点客户 ", "", "重点客户", "展会"},
	}
	in.normalizeProfile()

	if in.Currency != "USD" || in.CreditCurrency != "USD" {
		t.Fatalf("currency defaults = %q/%q", in.Currency, in.CreditCurrency)
	}
	if in.CreditStatus != "NORMAL" || in.BusinessStatus != "PROSPECT" {
		t.Fatalf("status defaults = %q/%q", in.CreditStatus, in.BusinessStatus)
	}
	if len(in.Tags) != 2 || in.Tags[0] != "重点客户" || in.Tags[1] != "展会" {
		t.Fatalf("normalized tags = %#v", in.Tags)
	}
}

func TestCustomerProfileValidation(t *testing.T) {
	tests := []CustomerInput{
		{Name: "A", PaymentDays: -1},
		{Name: "A", CreditLimitMinor: -1},
		{Name: "A", CreditCurrency: "US"},
		{Name: "A", CreditStatus: "UNKNOWN"},
		{Name: "A", BusinessStatus: "UNKNOWN"},
	}
	for i, in := range tests {
		if err := in.validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestCustomerAddressValidation(t *testing.T) {
	valid := CustomerAddressInput{
		AddressType: " shipping ", CountryCode: "us", AddressLine: "  1 Main St  ",
	}
	if err := valid.normalizeAndValidateAddress(); err != nil {
		t.Fatalf("valid address: %v", err)
	}
	if valid.AddressType != "SHIPPING" || valid.CountryCode != "US" || valid.AddressLine != "1 Main St" {
		t.Fatalf("normalized address = %#v", valid)
	}

	invalid := []CustomerAddressInput{
		{AddressType: "OTHER", AddressLine: "x"},
		{AddressType: "OFFICE"},
		{AddressType: "OFFICE", AddressLine: "x", CountryCode: "USA"},
		{AddressType: "OFFICE", AddressLine: "x", SortOrder: -1},
	}
	for i := range invalid {
		if err := invalid[i].normalizeAndValidateAddress(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}
