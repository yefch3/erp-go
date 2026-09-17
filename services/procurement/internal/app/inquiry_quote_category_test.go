package app

import "testing"

func TestValidateInquiryQuoteCategorySetsCurrency(t *testing.T) {
	products := []InquiryProduct{{ID: "p"}}
	tests := []struct {
		category string
		currency string
	}{
		{"FOB_USD", "USD"},
		{"FOB_CNY", "CNY"},
		{"ALL_IN_PORT_CNY", "CNY"},
		{"EX_FACTORY_CNY", "CNY"},
		{"REPROCESSING_CNY", "CNY"},
		{"DIRECT_CFR_USD", "USD"},
	}
	for _, test := range tests {
		quote := InquiryQuoteBody{Company: "Factory", Currency: "WRONG", QuoteCategory: test.category, Prices: []InquiryPrice{{ProductID: "p", Price: "1"}}}
		if test.category == "REPROCESSING_CNY" {
			quote.Prices[0].FactoryPrice = "2"
		}
		if err := validateInquiryQuote(&quote, products, "PROCUREMENT", true); err != nil {
			t.Fatalf("category %s: %v", test.category, err)
		}
		if quote.Currency != test.currency {
			t.Fatalf("category %s currency = %s, want %s", test.category, quote.Currency, test.currency)
		}
	}
}

func TestValidateReprocessingRequiresFactoryAndProcessingPrices(t *testing.T) {
	products := []InquiryProduct{{ID: "p"}}
	for _, price := range []InquiryPrice{{ProductID: "p", Price: "1"}, {ProductID: "p", FactoryPrice: "2"}} {
		quote := InquiryQuoteBody{Company: "Factory", QuoteCategory: "REPROCESSING_CNY", Prices: []InquiryPrice{price}}
		if err := validateInquiryQuote(&quote, products, "PROCUREMENT", true); err == nil {
			t.Fatal("accepted reprocessing quote without both factory price and processing fee")
		}
	}
}

func TestValidateInquiryQuoteCategoryRequiredOnSubmit(t *testing.T) {
	products := []InquiryProduct{{ID: "p"}}
	quote := InquiryQuoteBody{Company: "Factory", Currency: "CNY", Prices: []InquiryPrice{{ProductID: "p", Price: "1"}}}
	if err := validateInquiryQuote(&quote, products, "PROCUREMENT", true); err == nil {
		t.Fatal("expected missing quote category to fail")
	}
}
