package app

import (
	"encoding/json"
	"testing"
)

func TestPrepareOfferUsesManuallyConfirmedRateAndFormulaSource(t *testing.T) {
	product := OfferProduct{ID: "p1", Product: "Coil", Quantity: "10", Unit: "MT"}
	source := OfferInquiry{}
	source.Body.Products = []OfferProduct{product}
	quoteBody, err := json.Marshal(map[string]any{
		"currency": "USD",
		"prices":   []map[string]string{{"productId": "p1", "fobPrice": "100"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	source.Quotes = []OfferSourceQuote{{ID: "q1", Kind: "PROCUREMENT", Body: quoteBody}}
	body := OfferBody{
		Customer: "Buyer",
		Currency: "USD",
		QuoteFX:  "7.05", QuoteFXConfirmed: true,
		Lines: []OfferLine{{
			OfferProduct:   product,
			FactoryQuoteID: "q1",
			Calculation:    OfferCalculation{Formula: 2, Ocean: "10", Days: "10", MTPerUnit: "1"},
		}},
	}
	got, err := prepareOffer(body, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines[0].UnitPrice != "111.67" {
		t.Fatalf("unit price = %s, want 111.67", got.Lines[0].UnitPrice)
	}
}
