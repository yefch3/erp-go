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
			{Id: 2, ScenarioNo: "COST-CONFIRMED", VersionNo: 3, RequirementVersionNo: 2, Currency: "USD", Status: "CONFIRMED", ProductTotal: "200", ChargeTotal: "30", LandedTotal: "230", CustomerTotal: "260", CustomerQuotationId: 9, SubmittedToSalesAt: "2026-08-27T12:00:00Z"},
			{Id: 3, ScenarioNo: "COST-NOT-SUBMITTED", Status: "CONFIRMED", CustomerTotal: "999"},
		}},
		&prv1.ListProcurementPlansResponse{ProcurementPlans: []*prv1.ProcurementPlan{
			{Id: 7, PlanNo: "PP-DRAFT", Status: "CONFIRMED", Items: []*prv1.ProcurementPlanItem{{SupplierName: "未提交供应商"}}},
			{Id: 8, PlanNo: "PP-SUBMITTED", VersionNo: 2, ManagerNote: "优先采用书面报价", Status: "SUBMITTED_TO_SALES", TargetSalesName: "负责销售", ConfirmedByName: "采购经理", Items: []*prv1.ProcurementPlanItem{{ProductName: "冷轧钢卷", SupplierName: "获选供应商", FactoryName: "获选工厂", BuyerName: "采购甲", SelectionType: "RECOMMENDED", Currency: "USD", UnitPrice: "520", AvailableQty: "20", UomCode: "TON", PaymentTerms: "T/T", Incoterm: "FOB", LeadTime: 15}}},
		}},
	)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, secret := range []string{"秘密供应商", "秘密工厂", "secret@example.com", "100", "200", "230", "COST-DRAFT", "COST-NOT-SUBMITTED", "999", "未提交供应商"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sales progress leaked procurement detail %q: %s", secret, got)
		}
	}
	for _, want := range []string{`"rfqCount":2`, `"quotedRfqCount":1`, `"scenarioNo":"COST-CONFIRMED"`, `"requirementVersionNo":2`, `"customerTotal":"260"`, `"planNo":"PP-SUBMITTED"`, `"versionNo":2`, `"managerNote":"优先采用书面报价"`, `"confirmedByName":"采购经理"`, `"productName":"冷轧钢卷"`, `"supplierName":"获选供应商"`, `"factoryName":"获选工厂"`, `"buyerName":"采购甲"`, `"unitPrice":"520"`, `"availableQty":"20"`, `"paymentTerms":"T/T"`, `"incoterm":"FOB"`} {
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
