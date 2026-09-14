package app

import "testing"

func TestSubmittedQuotesDoNotRequireCustomerTradeTerms(t *testing.T) {
	products := []InquiryProduct{{ID: "p"}}
	factory := InquiryQuoteBody{Company: "Factory", Currency: "CNY", Prices: []InquiryPrice{{ProductID: "p", Price: "700"}}}
	if err := validateInquiryQuote(&factory, products, "PROCUREMENT", true); err != nil {
		t.Fatal(err)
	}
	logistics := InquiryQuoteBody{Company: "Forwarder", Currency: "CNY", Charges: []InquiryCharge{{Name: "海运费", Amount: "70.5", Quantity: "1", Unit: "MT", Currency: "CNY"}, {Name: "内陆运费", Amount: "70", Quantity: "1", Unit: "MT", Currency: "CNY"}}}
	if err := validateInquiryQuote(&logistics, products, "LOGISTICS", true); err != nil {
		t.Fatal(err)
	}
	if logistics.Totals["CNY"] != "140.50" {
		t.Fatal(logistics.Totals)
	}
	logistics.Charges[1].Currency = "USD"
	if err := validateInquiryQuote(&logistics, products, "LOGISTICS", true); err == nil {
		t.Fatal("mixed currencies accepted")
	}
	logistics.Charges[1].Currency = "CNY"
	logistics.Charges[1].Unit = "PCS"
	if err := validateInquiryQuote(&logistics, products, "LOGISTICS", true); err == nil {
		t.Fatal("mixed units accepted")
	}
}
