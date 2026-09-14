package app

import (
	"encoding/json"
	"testing"
)

func TestAllocateOfferLogisticsKeepsCurrenciesAndCentTail(t *testing.T) {
	body := OfferBody{Lines: []OfferLine{
		{OfferProduct: OfferProduct{ID: "a", Product: "A", Quantity: "1"}, Calculation: OfferCalculation{MTPerUnit: "1"}},
		{OfferProduct: OfferProduct{ID: "b", Product: "B", Quantity: "1"}, Calculation: OfferCalculation{MTPerUnit: "1"}},
		{OfferProduct: OfferProduct{ID: "c", Product: "C", Quantity: "1"}, Calculation: OfferCalculation{MTPerUnit: "1"}},
	}, Transports: []OfferTransport{{QuoteID: "q", Accepted: true}}}
	raw, _ := json.Marshal(map[string]any{"charges": []map[string]string{
		{"amount": "1", "quantity": "1", "currency": "USD", "allocationType": "FIXED"},
		{"amount": "2", "quantity": "1", "currency": "CNY", "allocationType": "DIRECT", "productId": "a"},
		{"amount": "3", "quantity": "1", "currency": "EUR", "allocationType": "PER_TON"},
	}})
	got, err := allocateOfferLogistics(body, OfferInquiry{Quotes: []OfferSourceQuote{{ID: "q", Kind: "LOGISTICS", Body: raw}}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a/USD": "0.33", "b/USD": "0.33", "c/USD": "0.34", "a/CNY": "2.00", "a/EUR": "3.00", "b/EUR": "3.00", "c/EUR": "3.00"}
	for _, row := range got {
		key := row.ProductID + "/" + row.Currency
		if want[key] != row.Amount {
			t.Fatalf("%s=%s want %s", key, row.Amount, want[key])
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing allocations: %#v", want)
	}
}

func TestAllocateOfferLogisticsRejectsMultipleShipments(t *testing.T) {
	_, err := allocateOfferLogistics(OfferBody{Transports: []OfferTransport{{Accepted: true}, {Accepted: true}}}, OfferInquiry{})
	if err == nil {
		t.Fatal("multiple accepted transport plans must be rejected")
	}
}
