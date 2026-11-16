package app

import "testing"

func TestCategoryContractUsesOnlyAgreedSnapshot(t *testing.T) {
	a := OfferCategorySelection{ProductID: "p1", Category: "FOB_USD", QuoteID: "q1"}
	b := OfferCategorySelection{ProductID: "p1", Category: "FOB_USD", QuoteID: "q2"}
	body := OfferBody{CustomerSelections: []OfferSelectionSnapshot{
		{OfferCategorySelection: a, Product: OfferProduct{ID: "p1", Product: "HRC", Specification: "3mm", Quantity: "100", Unit: "MT"}},
		{OfferCategorySelection: b, Product: OfferProduct{ID: "p1", Product: "HRC", Specification: "3mm", Quantity: "100", Unit: "MT"}},
	}, Negotiations: []OfferNegotiation{{OfferCategorySelection: a, Status: "AGREED", ProposedPrice: "12.3456"}, {OfferCategorySelection: b, Status: "QUOTED", ProposedPrice: "15"}}}
	got, err := categoryContractBody(body)
	if err != nil || len(got.Lines) != 1 || got.Total != "1234.56" || got.Lines[0].FactoryQuoteID != "q1" || got.Currency != "USD" {
		t.Fatalf("%+v %v", got, err)
	}
	body.Negotiations[1].Status = "AGREED"
	if _, err = categoryContractBody(body); err == nil {
		t.Fatal("accepted duplicate product quantities")
	}
	body.Negotiations[0].Status = "QUOTED"
	body.Negotiations[1].Status = "QUOTED"
	if _, err = categoryContractBody(body); err == nil {
		t.Fatal("accepted unconfirmed alternatives")
	}
}
