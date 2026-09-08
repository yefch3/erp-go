package app

import "testing"

func TestFormalOfferFormulas(t *testing.T) {
	// Rate buffer gives exactly 7. Factory = 700 RMB (100 USD in formula 2).
	want := []string{"115.00", "115.00", "130.00", "135.00", "125.00"}
	for formula := 1; formula <= 5; formula++ {
		input := OfferCalculation{Formula: formula, Factory: "700", Slitting: "14", ShortHaul: "21", Inland: "70", Port: "35", Ocean: "10", Days: "30", MTPerUnit: "1"}
		if formula == 2 {
			input.Factory = "100"
		}
		got, err := calculateOfferPrice(input, "7.05")
		if err != nil || got != want[formula-1] {
			t.Errorf("formula %d: %s %v, want %s", formula, got, err, want[formula-1])
		}
	}
}
func TestOfferFinalNegotiationDoesNotUseOriginalQuantity(t *testing.T) {
	original, err := offerLineAmount("100", "120")
	if err != nil || original != "12000.00" {
		t.Fatal(original, err)
	}
	final, err := offerLineAmount("60", "125")
	if err != nil || final != "7500.00" {
		t.Fatal(final, err)
	}
}
func TestOfferUnitConversionAndRounding(t *testing.T) {
	got, err := calculateOfferPrice(OfferCalculation{Formula: 2, Factory: "1", Ocean: "0", Days: "30", MTPerUnit: "0.001"}, "")
	if err != nil || got != "1.01" {
		t.Fatalf("interest must respect tonnes per PCS and round half up: %s %v", got, err)
	}
	if _, err := calculateOfferPrice(OfferCalculation{Formula: 1, Factory: "7", MTPerUnit: "1"}, "0.05"); err == nil {
		t.Fatal("zero denominator allowed")
	}
	if _, err := calculateOfferPrice(OfferCalculation{Formula: 2, Factory: "7"}, ""); err == nil {
		t.Fatal("missing unit weight allowed")
	}
}

func TestCustomerTemplateSpecificationKeepsLabelsAndValues(t *testing.T) {
	var source OfferInquiry
	source.Body.Template = []byte(`{"fields":[{"fieldKey":"thickness","displayName":"厚度(mm)"},{"fieldKey":"grade","displayName":"材质"}]}`)
	p := OfferProduct{Specification: "Q235", CustomFields: map[string]string{"thickness": "2", "grade": "A", "empty": ""}}
	if got := offerSpecification(p, source); got != "Q235; 材质: A; 厚度(mm): 2" {
		t.Fatalf("customer template fields lost: %s", got)
	}
}
