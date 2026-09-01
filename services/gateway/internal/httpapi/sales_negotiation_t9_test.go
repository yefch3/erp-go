package httpapi

import "testing"

func TestSingleValueForFormalQuotation(t *testing.T) {
	if got := singleValue(map[string]bool{"FOB": true}, "per line"); got != "FOB" {
		t.Fatalf("single value = %q", got)
	}
	if got := singleValue(map[string]bool{"FOB": true, "CIF": true}, "per line"); got != "per line" {
		t.Fatalf("mixed values = %q", got)
	}
	if got := singleValue(map[string]bool{"": true}, "per line"); got != "per line" {
		t.Fatalf("blank value = %q", got)
	}
}
