package app

import (
	"encoding/json"
	"testing"
)

func TestSingleFactoryPriceAndLogisticsUnitConversion(t *testing.T) {
	product := OfferProduct{ID: "p", Product: "Steel", Unit: "MT", Quantity: "3"}
	source := OfferInquiry{}
	source.Body.Products = []OfferProduct{product}
	factory, _ := json.Marshal(map[string]any{"currency": "CNY", "prices": []map[string]string{{"productId": "p", "price": "700", "fobPrice": "999"}}})
	logistics := json.RawMessage("{\"transitDays\":\"30\",\"charges\":[{\"name\":\"海运费\",\"amount\":\"10\",\"currency\":\"USD\",\"unit\":\"MT\",\"quantity\":\"1\",\"allocationType\":\"PER_TON\"}]}")
	source.Quotes = []OfferSourceQuote{{ID: "f", Kind: "PROCUREMENT", Body: factory}, {ID: "l", Kind: "LOGISTICS", Body: logistics}}
	for _, tc := range []struct {
		formula        int
		unit, mt, want string
	}{{1, "MT", "1", "115.00"}, {2, "MT", "1", "114.29"}, {1, "PCS", "0.01", "1.15"}} {
		p := product
		p.Unit = tc.unit
		b := OfferBody{Customer: "Test", Currency: "USD", QuoteFX: "7.05", QuoteFXConfirmed: true, Incoterm: "CFR", LogisticsQuoteID: "l", Transports: []OfferTransport{{QuoteID: "l", Currency: "USD", Price: "10", Accepted: true, Quantities: map[string]string{"p": "3"}}}, Lines: []OfferLine{{OfferProduct: p, FactoryQuoteID: "f", Calculation: OfferCalculation{Formula: tc.formula, MTPerUnit: tc.mt, Ocean: "999"}}}}
		got, err := prepareOffer(b, source, nil, true, "*")
		if err != nil {
			t.Fatal(err)
		}
		if got.Lines[0].UnitPrice != tc.want {
			t.Fatalf("formula %d %s: got %s want %s", tc.formula, tc.unit, got.Lines[0].UnitPrice, tc.want)
		}
	}
}
