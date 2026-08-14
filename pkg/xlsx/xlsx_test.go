package xlsx

import "testing"

func TestBuildParseRoundTripPreservesFixedTemplate(t *testing.T) {
	want := [][]string{
		{"RFQ No", "Line ID", "Specification", "Quantity", "Unit", "Unit Price", "MOQ", "Lead Time", "Remark"},
		{"RFQ-1", "42", "HRC <ASTM> & G60", "12.5000", "TON", "", "5", "20 days", "keep dry"},
	}
	data, err := Build("Supplier Quote", want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d", len(got), len(want))
	}
	for r := range want {
		for c := range want[r] {
			if got[r][c] != want[r][c] {
				t.Fatalf("cell %d,%d = %q, want %q", r, c, got[r][c], want[r][c])
			}
		}
	}
}

func TestParseRejectsNonWorkbook(t *testing.T) {
	if _, err := Parse([]byte("not an xlsx")); err == nil {
		t.Fatal("invalid archive was accepted")
	}
}
