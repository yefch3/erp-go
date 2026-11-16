package app

import (
	"encoding/json"
	"testing"
)

func TestManualFreightUsesChosenQuoteDespiteNewerAlternative(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	source.Quotes = append(source.Quotes, OfferSourceQuote{ID: "newer", Kind: "LOGISTICS", SubmittedAt: "2099-01-01", Version: 9, Body: json.RawMessage(`{"freightRates":[{"productId":"p1","usdPrice":"99"}]}`)})
	b.CategorySelections[1].FreightQuoteID = "newer"
	b.CategorySelections[1].FreightQuoteVersion = 9
	got, err := prepareOffer(b, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	if got.CategorySelections[0].CFRUnitPrice != "12.5000" || got.CategorySelections[1].CFRUnitPrice != "109.0000" {
		t.Fatalf("did not use individually selected freight: %#v", got.CategorySelections)
	}
	stageOfferSelections(&got, source)
	if len(got.CustomerLogistics) != 2 {
		t.Fatalf("chosen logistics were not staged: %#v", got.CustomerLogistics)
	}
}

func TestManualFreightRequiresCurrentExplicitSelection(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*OfferBody, *OfferInquiry)
	}{
		{"missing", func(b *OfferBody, _ *OfferInquiry) { b.CategorySelections[0].FreightQuoteID = "" }},
		{"legacy automatic choice", func(b *OfferBody, _ *OfferInquiry) { b.CategorySelections[0].FreightQuoteVersion = 0 }},
		{"unknown", func(b *OfferBody, _ *OfferInquiry) { b.CategorySelections[0].FreightQuoteID = "missing" }},
		{"wrong kind", func(b *OfferBody, _ *OfferInquiry) { b.CategorySelections[0].FreightQuoteID = "q1" }},
		{"changed", func(_ *OfferBody, s *OfferInquiry) { s.Quotes[2].Version++ }},
		{"historical", func(_ *OfferBody, s *OfferInquiry) { s.Quotes[2].Historical = true }},
		{"draft", func(_ *OfferBody, s *OfferInquiry) { s.Quotes[2].SubmittedAt = "" }},
		{"expired", func(_ *OfferBody, s *OfferInquiry) {
			s.Quotes[2].Body = json.RawMessage(`{"validUntil":"2000-01-01","freightRates":[{"productId":"p1","usdPrice":"2.5"}]}`)
		}},
		{"wrong product", func(_ *OfferBody, s *OfferInquiry) {
			s.Quotes[2].Body = json.RawMessage(`{"freightRates":[{"productId":"p2","usdPrice":"2.5"}]}`)
		}},
		{"invalid price", func(_ *OfferBody, s *OfferInquiry) {
			s.Quotes[2].Body = json.RawMessage(`{"freightRates":[{"productId":"p1","usdPrice":"-1"}]}`)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			b, source := categoryWorkspaceFixture()
			test.change(&b, &source)
			if _, err := prepareOffer(b, source, nil, true, "*"); err == nil {
				t.Fatal("invalid freight selection accepted")
			}
		})
	}
}

func TestDirectCFRDoesNotRequireFreightSelection(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	b.Transports = nil
	b.CategorySelections = []OfferCategorySelection{{ProductID: "p1", Category: "DIRECT_CFR_USD", QuoteID: "q1"}}
	b.CategoryCalculations["DIRECT_CFR_USD"] = CategoryCalculation{InterestRate: "0", InterestDays: "0"}
	source.Quotes = source.Quotes[:1]
	source.Quotes[0].Body = json.RawMessage(`{"quoteCategory":"DIRECT_CFR_USD","currency":"USD","prices":[{"productId":"p1","price":"18"}]}`)
	got, err := prepareOffer(b, source, nil, true, "*")
	if err != nil || got.CategorySelections[0].CFRUnitPrice != "18.0000" {
		t.Fatalf("direct CFR unexpectedly requires freight: %v / %#v", err, got.CategorySelections)
	}
}
