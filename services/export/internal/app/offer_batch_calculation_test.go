package app

import (
	"encoding/json"
	"testing"
)

func TestPrepareOfferRoundsConvertedFactoryPriceForUSDFormula(t *testing.T) {
	product := OfferProduct{ID: "p1", Product: "Coil", Quantity: "10", Unit: "MT"}
	source := OfferInquiry{}
	source.Body.Products = []OfferProduct{product}
	quoteBody, err := json.Marshal(map[string]any{
		"currency": "CNY",
		"prices": []map[string]string{{"productId": "p1", "price": "700"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	source.Quotes = []OfferSourceQuote{{ID: "q1", Kind: "PROCUREMENT", Body: quoteBody}}
	body := OfferBody{
		Customer: "Buyer",
		Currency: "USD",
		Lines: []OfferLine{{
			OfferProduct: product,
			FactoryQuoteID: "q1",
			Calculation: OfferCalculation{Formula: 2, Ocean: "10", Days: "10", MTPerUnit: "1"},
		}},
	}
	rates := []OfferRate{{Base: "USD", Quote: "CNY", Value: "7.05", At: "2026-09-08T00:00:00Z"}}

	got, err := prepareOffer(body, source, rates, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines[0].UnitPrice != "110.96" {
		t.Fatalf("unit price = %s, want 110.96", got.Lines[0].UnitPrice)
	}
}
