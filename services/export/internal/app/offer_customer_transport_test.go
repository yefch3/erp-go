package app

import (
	"encoding/json"
	"testing"
)

func TestCustomerSelectsTransportBeforePricing(t *testing.T) {
	product := OfferProduct{ID: "p", Product: "Steel", Unit: "MT", Quantity: "3"}
	source := OfferInquiry{}
	source.Body.Products = []OfferProduct{product}
	f, _ := json.Marshal(map[string]any{"currency": "CNY", "prices": []map[string]string{{"productId": "p", "price": "700"}}})
	l, _ := json.Marshal(map[string]any{"transitDays": "30", "charges": []map[string]string{{"name": "海运费", "amount": "10", "quantity": "1", "currency": "USD", "unit": "MT", "allocationType": "PER_TON"}}})
	source.Quotes = []OfferSourceQuote{{ID: "f", Kind: "PROCUREMENT", Body: f}, {ID: "l", Kind: "LOGISTICS", Body: l}, {ID: "other", Kind: "LOGISTICS", Body: l}}
	b := OfferBody{Customer: "Test", Currency: "USD", QuoteFX: "7.05", QuoteFXConfirmed: true, Incoterm: "CFR", Lines: []OfferLine{{OfferProduct: product, FactoryQuoteID: "f", Calculation: OfferCalculation{MTPerUnit: "1"}}}, Transports: []OfferTransport{{QuoteID: "l", Currency: "USD", Price: "10"}}}
	// Draft candidates can be saved before a final product price is known.
	draft, err := prepareOffer(b, source, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Lines[0].UnitPrice != "" || draft.Transports[0].Accepted {
		t.Fatal("draft became a priced/accepted offer")
	}
	b.LogisticsQuoteID = "l"
	b.Lines[0].Calculation.Formula = 1
	if _, err := prepareOffer(b, source, nil, true, "*"); err == nil {
		t.Fatal("pricing allowed before customer selected a transport")
	}
	b.Transports[0].Accepted = true
	b.Transports[0].Quantities = map[string]string{"p": "3"}
	priced, err := prepareOffer(b, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	if priced.Total != "345.00" {
		t.Fatalf("freight counted twice: %s", priced.Total)
	}
	if !priced.Transports[0].Accepted {
		t.Fatal("customer selection was lost")
	}
	b.LogisticsQuoteID = "other"
	if _, err := prepareOffer(b, source, nil, true, "*"); err == nil {
		t.Fatal("pricing uses a different quote than customer selection")
	}
}
