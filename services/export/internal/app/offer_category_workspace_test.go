package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sgao19/erp-go/pkg/xlsx"
)

func categoryWorkspaceFixture() (OfferBody, OfferInquiry) {
	source := OfferInquiry{}
	source.Body.Products = []OfferProduct{{ID: "p1", Product: "Steel", Quantity: "20", Unit: "MT"}}
	q := json.RawMessage(`{"quoteCategory":"FOB_USD","currency":"USD","prices":[{"productId":"p1","price":"10"}]}`)
	source.Quotes = []OfferSourceQuote{{ID: "q1", Kind: "PROCUREMENT", SubmittedAt: "2026-09-16", Version: 1, Body: q}, {ID: "q2", Kind: "PROCUREMENT", SubmittedAt: "2026-09-16", Version: 1, Body: q}, {ID: "ship", Kind: "LOGISTICS", SubmittedAt: "2026-09-16", Version: 1, Body: json.RawMessage(`{"company":"Carrier","freightRates":[{"productId":"p1","usdPrice":"2.5000"}]}`)}}
	return OfferBody{CategoryWorkflow: true, Customer: "Customer", CategorySelections: []OfferCategorySelection{{ProductID: "p1", Category: "FOB_USD", QuoteID: "q1"}, {ProductID: "p1", Category: "FOB_USD", QuoteID: "q2"}}, Transports: []OfferTransport{{QuoteID: "ship"}}, CategoryCalculations: map[string]CategoryCalculation{"FOB_USD": {InterestRate: "0", InterestDays: "360"}}}, source
}

func TestCategoryOfferPDFUsesSelectedCustomerPrices(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	b.CategorySelections = b.CategorySelections[:1]
	calculated, err := prepareOffer(b, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	stageOfferSelections(&calculated, source)
	calculated.Customer = "Customer"
	calculated.DocumentLanguage = "EN"
	calculated.Negotiations[0].ProposedPrice = "15.25"
	data, err := categoryOfferPDF(OfferView{Body: calculated, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatal("generated file is not a PDF")
	}
	calculated.Negotiations[0].ProposedPrice = ""
	if _, err := categoryOfferPDF(OfferView{Body: calculated, Source: source}); err != nil {
		t.Fatalf("initial calculated price was not used as fallback: %v", err)
	}
	calculated.Negotiations[0].InitialPrice = ""
	calculated.CustomerSelections[0].CFRUnitPrice = ""
	if _, err := categoryOfferPDF(OfferView{Body: calculated, Source: source}); err == nil {
		t.Fatal("generated quotation without an initial or adjusted customer price")
	}
}

func TestCategoryOfferWorkbookUsesLanguageAndSelectedCustomerPrices(t *testing.T) {
	body, source := categoryWorkspaceFixture()
	body.CategorySelections = body.CategorySelections[:1]
	calculated, err := prepareOffer(body, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	stageOfferSelections(&calculated, source)
	calculated.Customer = "Customer"
	calculated.DocumentLanguage = "EN"
	calculated.Negotiations[0].ProposedPrice = "15.25"
	data, err := categoryOfferWorkbook(OfferView{Body: calculated, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "CUSTOMER QUOTATION" || rows[6][1] != "Product" {
		t.Fatalf("workbook did not use English labels: %#v / %#v", rows[0], rows[6])
	}
	if rows[7][5] != "15.2500" || rows[7][6] != "305.00" || rows[8][6] != "305.00" {
		t.Fatalf("workbook price/amount/total = %q/%q/%q", rows[7][5], rows[7][6], rows[8][6])
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range zr.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		xmlData, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		sheet := string(xmlData)
		for _, want := range []string{`<c r="D8" s="2"><v>20</v></c>`, `<c r="F8" s="2"><v>15.2500</v></c>`} {
			if !strings.Contains(sheet, want) {
				t.Fatalf("customer quotation numeric cell missing %s: %s", want, sheet)
			}
		}
		return
	}
	t.Fatal("customer quotation worksheet missing")
}

func TestCategoryWorkspaceCalculatesFOBUSDFromProductFreightUnitPrice(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	got, err := prepareOffer(b, source, nil, true, "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range got.CategorySelections {
		if selection.SupplierFOBUnitPrice != "10.0000" || selection.ProductFreightUnitPrice != "2.5000" || selection.CFRUnitPrice != "12.5000" || selection.FreightQuoteID != "ship" {
			t.Fatalf("unexpected CFR result: %#v", selection)
		}
	}
	stageOfferSelections(&got, source)
	if got.Negotiations[0].InitialPrice != "12.5000" {
		t.Fatalf("calculated unit price was not staged as the initial customer price: %#v", got.Negotiations[0])
	}
	if len(got.CustomerLogistics) != 1 || got.CustomerLogistics[0].ID != "ship" {
		t.Fatalf("automatic logistics source was not staged: %#v", got.CustomerLogistics)
	}
}

func TestCategoryWorkspaceFOBUSDRequiresProductFreightUnitPrice(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	source.Quotes[2].Body = json.RawMessage(`{"company":"Carrier","freightRates":[]}`)
	if _, err := prepareOffer(b, source, nil, true, "*"); err == nil {
		t.Fatal("calculation accepted a product without ocean freight unit price")
	}
}

func TestCategoryWorkspaceCalculatesUnitPriceFormulasWithInterest(t *testing.T) {
	tests := []struct {
		category, quoteBody, fx, port, inland, loss, wantUnit string
	}{
		{"FOB_USD", `{"quoteCategory":"FOB_USD","currency":"USD","prices":[{"productId":"p1","price":"10"}]}`, "", "", "", "", "12.6250"},
		{"FOB_CNY", `{"quoteCategory":"FOB_CNY","currency":"CNY","prices":[{"productId":"p1","price":"70"}]}`, "7", "", "", "", "12.6250"},
		{"ALL_IN_PORT_CNY", `{"quoteCategory":"ALL_IN_PORT_CNY","currency":"CNY","prices":[{"productId":"p1","price":"70"}]}`, "7", "7", "", "", "13.6350"},
		{"EX_FACTORY_CNY", `{"quoteCategory":"EX_FACTORY_CNY","currency":"CNY","prices":[{"productId":"p1","price":"70"}]}`, "7", "7", "7", "", "14.6450"},
		{"REPROCESSING_CNY", `{"quoteCategory":"REPROCESSING_CNY","currency":"CNY","prices":[{"productId":"p1","price":"14","factoryPrice":"56"}]}`, "7", "7", "7", "7", "15.6550"},
		{"DIRECT_CFR_USD", `{"quoteCategory":"DIRECT_CFR_USD","currency":"USD","prices":[{"productId":"p1","price":"18"}]}`, "", "", "", "", "18.1800"},
	}
	for _, test := range tests {
		t.Run(test.category, func(t *testing.T) {
			b, source := categoryWorkspaceFixture()
			source.Quotes[0].Body = json.RawMessage(test.quoteBody)
			b.CategorySelections = []OfferCategorySelection{{ProductID: "p1", Category: test.category, QuoteID: "q1"}}
			b.CategoryCalculations = map[string]CategoryCalculation{test.category: {
				QuoteFX: test.fx, PortCharge: test.port, InlandFreight: test.inland, Loss: test.loss,
				InterestRate: "6", InterestDays: "60",
			}}
			source.Quotes[2].Body = json.RawMessage(`{"freightRates":[{"productId":"p1","usdPrice":"2.5"}]}`)
			got, err := prepareOffer(b, source, nil, true, test.category)
			if err != nil {
				t.Fatal(err)
			}
			selection := got.CategorySelections[0]
			if selection.CFRUnitPrice != test.wantUnit {
				t.Fatalf("unexpected result: %#v", selection)
			}
			if test.category == "DIRECT_CFR_USD" && (selection.ProductFreightUnitPrice != "" || selection.FreightQuoteID != "") {
				t.Fatalf("direct CFR quote unexpectedly used logistics: %#v", selection)
			}
		})
	}
}
func TestCategoryWorkspaceAllowsAlternativesWithoutFX(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	got, err := prepareOffer(b, source, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.CategorySelections) != 2 {
		t.Fatal("lost supplier alternatives")
	}
	stageOfferSelections(&got, source)
	if len(got.CustomerSelections) != 2 || len(got.CustomerLogistics) != 1 || len(got.Negotiations) != 2 || got.SelectionSavedAt == "" {
		t.Fatal("missing staged snapshot")
	}
	got.Negotiations[0].InitialPrice = "12.50"
	got.Negotiations[0].CustomerCounterPrice = "11.80"
	got.Negotiations[0].ProposedPrice = "12.10"
	got.Negotiations[0].Status = "CUSTOMER_COUNTERED"
	got.DocumentLanguage = "EN"
	got.CategoryCalculations = map[string]CategoryCalculation{"FOB_USD": {QuoteFX: "7.05", Note: "test"}}
	if _, err := prepareOffer(got, source, nil, false, ""); err != nil {
		t.Fatalf("valid calculation and negotiation rejected: %v", err)
	}
	// Saved customer alternatives remain stable when sales changes its working selection.
	got.CategorySelections = nil
	source.Quotes[0].Version = 2
	source.Quotes[0].Body = json.RawMessage(`{"prices":[{"productId":"p1","price":"999"}]}`)
	if got.CustomerSelections[0].Quote.Version != 1 {
		t.Fatal("staged quote changed with source")
	}
	var data map[string]any
	if err := json.Unmarshal(got.CustomerSelections[0].Quote.Body, &data); err != nil {
		t.Fatal(err)
	}
	if data["currency"] != "USD" {
		t.Fatal("snapshot body was replaced")
	}
}

func TestCategoryWorkspaceRejectsInvalidDocumentLanguage(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	b.DocumentLanguage = "DE"
	if _, err := prepareOffer(b, source, nil, false, ""); err == nil {
		t.Fatal("accepted invalid document language")
	}
}

func TestCategoryWorkspaceRejectsInvalidNegotiation(t *testing.T) {
	b, source := categoryWorkspaceFixture()
	stageOfferSelections(&b, source)
	b.Negotiations[0].Status = "UNKNOWN"
	if _, err := prepareOffer(b, source, nil, false, ""); err == nil {
		t.Fatal("accepted invalid negotiation status")
	}
	b.Negotiations[0].Status = "DRAFT"
	b.Negotiations[0].InitialPrice = "bad price"
	if _, err := prepareOffer(b, source, nil, false, ""); err == nil {
		t.Fatal("accepted invalid negotiation price")
	}
}
func TestCategoryWorkspaceRejectsInvalidSources(t *testing.T) {
	for _, scenario := range []string{"duplicate", "missing product", "category", "currency", "draft", "history", "logistics"} {
		t.Run(scenario, func(t *testing.T) {
			b, s := categoryWorkspaceFixture()
			switch scenario {
			case "duplicate":
				b.CategorySelections = append(b.CategorySelections, b.CategorySelections[0])
			case "missing product":
				b.CategorySelections[0].ProductID = "missing"
			case "category":
				b.CategorySelections[0].Category = "EX_FACTORY_CNY"
			case "currency":
				s.Quotes[0].Body = json.RawMessage(`{"quoteCategory":"FOB_USD","currency":"CNY","prices":[{"productId":"p1"}]}`)
			case "draft":
				s.Quotes[0].SubmittedAt = ""
			case "history":
				s.Quotes[0].Historical = true
			case "logistics":
				b.Transports[0].QuoteID = "q1"
			}
			if _, err := prepareOffer(b, s, nil, false, ""); err == nil {
				t.Fatal("accepted invalid quote source")
			}
		})
	}
}
