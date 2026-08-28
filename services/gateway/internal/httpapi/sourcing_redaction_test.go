package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func TestRedactSupplierQuotePrices(t *testing.T) {
	resp := &prv1.ListSupplierQuoteComparisonResponse{Lines: []*prv1.SupplierQuoteComparisonLine{{
		SupplierName: "Mill", UnitPrice: "520", Amount: "1040", Qty: "2", PaymentTerms: "30 days",
	}}}
	redactSupplierQuotePrices(resp)
	line := resp.GetLines()[0]
	if line.GetUnitPrice() != "" || line.GetAmount() != "" {
		t.Fatalf("factory price leaked: %#v", line)
	}
	if line.GetQty() != "2" || line.GetPaymentTerms() != "30 days" {
		t.Fatalf("non-price fields were unexpectedly removed: %#v", line)
	}
}

func TestSalesProcurementProgressPayloadUsesSensitiveFieldWhitelist(t *testing.T) {
	payload := salesProcurementProgressPayload(
		&prv1.GetCaseResponse{SourcingCase: &prv1.SourcingCase{Status: "SOURCING"}},
		&prv1.ListFactoryRfqsResponse{FactoryRfqs: []*prv1.FactoryRfq{
			{Status: "QUOTED", SupplierName: "秘密供应商", FactoryName: "秘密工厂", ContactEmail: "secret@example.com"},
			{Status: "SENT", SupplierName: "另一个供应商"},
		}},
		&prv1.ListCostScenariosResponse{CostScenarios: []*prv1.CostScenario{
			{Id: 1, ScenarioNo: "COST-DRAFT", Status: "DRAFT", ProductTotal: "100", ChargeTotal: "20", CustomerTotal: "130"},
			{Id: 2, ScenarioNo: "COST-CONFIRMED", VersionNo: 3, Currency: "USD", Status: "CONFIRMED", ProductTotal: "200", ChargeTotal: "30", LandedTotal: "230", CustomerTotal: "260", CustomerQuotationId: 9},
		}},
	)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, secret := range []string{"秘密供应商", "秘密工厂", "secret@example.com", "100", "200", "230", "COST-DRAFT"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sales progress leaked procurement detail %q: %s", secret, got)
		}
	}
	for _, want := range []string{`"rfqCount":2`, `"quotedRfqCount":1`, `"scenarioNo":"COST-CONFIRMED"`, `"customerTotal":"260"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("sales progress missing %s: %s", want, got)
		}
	}
}

func TestRedactCostScenarioKeepsCustomerQuote(t *testing.T) {
	scenario := &prv1.CostScenario{
		MarginType: "PERCENT", MarginValue: "8", FxRate: "7.2",
		ProductTotal: "100", ChargeTotal: "20", LandedTotal: "120", MarginTotal: "10", CustomerTotal: "130",
		Charges: []*prv1.CostCharge{{Amount: "20"}},
		Lines:   []*prv1.CostScenarioLine{{SupplierName: "Mill", SourceUnitPrice: "50", ProductCost: "100", LandedCost: "60", MarginAmount: "5", CustomerUnitPrice: "65", CustomerAmount: "130"}},
	}
	redactCostScenario(scenario)
	if scenario.GetProductTotal() != "" || scenario.GetMarginTotal() != "" || len(scenario.GetCharges()) != 0 {
		t.Fatalf("cost fields leaked: %#v", scenario)
	}
	line := scenario.GetLines()[0]
	if line.GetSupplierName() != "" || line.GetSourceUnitPrice() != "" || line.GetLandedCost() != "" {
		t.Fatalf("line cost fields leaked: %#v", line)
	}
	if scenario.GetCustomerTotal() != "130" || line.GetCustomerUnitPrice() != "65" || line.GetCustomerAmount() != "130" {
		t.Fatalf("customer quote fields should remain visible: %#v", scenario)
	}
}
