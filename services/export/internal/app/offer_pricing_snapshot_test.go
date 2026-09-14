package app

import (
	"encoding/json"
	"testing"
)

func TestPricingSnapshotSurvivesSaveAndNegotiation(t *testing.T) {
	p := OfferProduct{ID: "p", Product: "Steel", Unit: "MT", Quantity: "3"}
	source := OfferInquiry{}
	source.Body.Products = []OfferProduct{p}
	source.Quotes = []OfferSourceQuote{
		{ID: "f", Kind: "PROCUREMENT", Body: json.RawMessage(`{"currency":"CNY","prices":[{"productId":"p","price":"700"}]}`)},
		{ID: "l", Kind: "LOGISTICS", Body: json.RawMessage(`{"transitDays":"30","charges":[{"name":"ocean","amount":"10","currency":"USD","unit":"MT","quantity":"1","allocationType":"PER_TON"}]}`)},
	}
	b := OfferBody{Customer: "Test", Currency: "USD", QuoteFX: "7.05", QuoteFXConfirmed: true, Incoterm: "CFR", LogisticsQuoteID: "l", Lines: []OfferLine{{OfferProduct: p, FactoryQuoteID: "f", Calculation: OfferCalculation{Formula: 1, MTPerUnit: "1"}}}, Transports: []OfferTransport{{QuoteID: "l", Currency: "USD", Price: "10", Accepted: true, Quantities: map[string]string{"p": "3"}}}}
	priced, err := prepareOffer(b, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	if !offerPricingStale(priced, source) {
		t.Fatal("legacy calculated price was trusted")
	}
	priced.PricingSnapshot = offerPricingSnapshot(priced, source)
	priced.Lines[0].UnitPrice = "118.00"
	if err := validateOfferPricing(priced, source); err != nil {
		t.Fatal(err)
	}
	saved, err := prepareOffer(priced, source, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Lines[0].UnitPrice != "118.00" || offerPricingStale(saved, source) {
		t.Fatal("save invalidated calculation or overwrote negotiated price")
	}
	raw, _ := json.Marshal(saved)
	for _, tc := range []struct {
		name   string
		change func(*OfferBody)
	}{
		{"exchange rate", func(b *OfferBody) { b.QuoteFX = "6.72" }},
		{"unit", func(b *OfferBody) { b.Lines[0].Unit = "PCS" }},
		{"quantity", func(b *OfferBody) { b.Lines[0].Quantity = "4" }},
		{"formula", func(b *OfferBody) { b.Lines[0].Calculation.Formula = 2 }},
		{"selection", func(b *OfferBody) { b.Transports[0].Accepted = false }},
		{"logistics", func(b *OfferBody) { b.LogisticsQuoteID = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var changed OfferBody
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			tc.change(&changed)
			if validateOfferPricing(changed, source) == nil {
				t.Fatal("changed inputs were accepted")
			}
		})
	}
	source.Quotes[0].Version++
	if validateOfferPricing(saved, source) == nil {
		t.Fatal("changed procurement source was accepted")
	}
	// FOB uses an internal cost quote, with no customer sea-freight selection.
	b.Incoterm = "FOB"
	b.Lines[0].Calculation.Formula = 5
	b.Transports = nil
	fob, err := prepareOffer(b, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	if fob.Lines[0].UnitPrice != "105.00" {
		t.Fatalf("FOB included ocean freight: %s", fob.Lines[0].UnitPrice)
	}
}
