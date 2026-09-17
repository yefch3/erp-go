package app

import "testing"

func TestSubmittedQuotesDoNotRequireCustomerTradeTerms(t *testing.T) {
	products := []InquiryProduct{{ID: "p", Quantity: "2"}}
	factory := InquiryQuoteBody{Company: "Factory", Currency: "CNY", QuoteCategory: "EX_FACTORY_CNY", Prices: []InquiryPrice{{ProductID: "p", Price: "700"}}}
	if err := validateInquiryQuote(&factory, products, "PROCUREMENT", true); err != nil {
		t.Fatal(err)
	}
	logistics := InquiryQuoteBody{Company: "Forwarder", Currency: "CNY", ExchangeRates: map[string]string{"CNY": "7.05"}, Charges: []InquiryCharge{{Name: "海运费", Amount: "70.5", Quantity: "1", Unit: "MT", Currency: "CNY"}, {Name: "内陆运费", Amount: "70", Quantity: "1", Unit: "MT", Currency: "CNY"}}}
	if err := validateInquiryQuote(&logistics, products, "LOGISTICS", true); err != nil {
		t.Fatal(err)
	}
	if logistics.Totals["CNY"] != "140.50" {
		t.Fatal(logistics.Totals)
	}
	if logistics.TotalUSD != "19.93" {
		t.Fatalf("USD total = %q", logistics.TotalUSD)
	}
	logistics.Charges[1].Currency = "USD"
	logistics.Charges[1].Unit = "PCS"
	if err := validateInquiryQuote(&logistics, products, "LOGISTICS", true); err != nil {
		t.Fatalf("mixed other-charge currencies and units should remain separate: %v", err)
	}
	if logistics.Totals["CNY"] != "70.50" || logistics.Totals["USD"] != "70.00" {
		t.Fatal(logistics.Totals)
	}
	logistics.Charges = nil
	logistics.FreightRates = []InquiryFreightRate{{ProductID: "p", Price: "35", Currency: "USD", Unit: "MT"}}
	if err := validateInquiryQuote(&logistics, products, "LOGISTICS", true); err != nil {
		t.Fatalf("product ocean freight should be a complete logistics quote: %v", err)
	}
	if logistics.FreightRates[0].USDPrice != "35.0000" {
		t.Fatalf("normalized freight unit price = %#v", logistics.FreightRates[0])
	}
}

func TestSubmittedLogisticsQuoteRequiresRateForEachForeignCurrency(t *testing.T) {
	products := []InquiryProduct{{ID: "p", Quantity: "2"}}
	quote := InquiryQuoteBody{Company: "Forwarder", FreightRates: []InquiryFreightRate{{ProductID: "p", Price: "705", Currency: "CNY", Unit: "MT"}}}
	if err := validateInquiryQuote(&quote, products, "LOGISTICS", false); err != nil {
		t.Fatalf("draft may wait for its exchange rate: %v", err)
	}
	if err := validateInquiryQuote(&quote, products, "LOGISTICS", true); err == nil {
		t.Fatal("submitted foreign-currency quote accepted without exchange rate")
	}
	quote.ExchangeRates = map[string]string{"CNY": "7.05"}
	if err := validateInquiryQuote(&quote, products, "LOGISTICS", true); err != nil {
		t.Fatal(err)
	}
	if quote.FreightRates[0].USDPrice != "100.0000" || quote.Currency != "USD" {
		t.Fatalf("normalized quote = %#v", quote)
	}
}
