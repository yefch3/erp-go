package app

import "testing"

func TestNormalizeSupplierInputAcceptsShortNameOnly(t *testing.T) {
	in := SupplierInput{ShortName: "ACME"}
	if err := normalizeSupplierInput(&in); err != nil {
		t.Fatal(err)
	}
	if in.Name != "ACME" || in.ShortName != "ACME" {
		t.Fatalf("supplier names = %#v", in)
	}
}

func TestNormalizeSupplierInputRejectsAllNamesBlank(t *testing.T) {
	in := SupplierInput{}
	if err := normalizeSupplierInput(&in); err == nil {
		t.Fatal("expected a missing-name error")
	}
}
