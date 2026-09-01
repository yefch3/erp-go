package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestT7T8SalesNegotiationCustomerSelectionAndFinalRecheck(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed T7 sales negotiation test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"sourcing_final_recheck_tasks", "sourcing_customer_selection_shipment_items", "sourcing_customer_selection_shipments", "sourcing_customer_selection_items", "sourcing_customer_selections", "sourcing_shipping_rework_requests", "sourcing_customer_feedback", "sourcing_sales_shipping_option_lines", "sourcing_sales_shipping_options", "sourcing_sales_plan_items", "sourcing_sales_plans", "sourcing_shipping_plan_items", "sourcing_shipping_plans", "sourcing_shipping_option_lines", "sourcing_shipping_options", "sourcing_shipping_participants", "sourcing_shipping_requests", "procurement_rework_requests", "procurement_plan_items", "procurement_plans", "supplier_quote_lines", "supplier_quotes", "factory_rfq_lines", "factory_rfqs", "sourcing_case_changes", "sourcing_procurement_participants", "sourcing_lines", "sourcing_cases"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	sales := Operator{ID: 710, Name: "T7 负责销售"}
	otherSales := Operator{ID: 711, Name: "其他销售"}
	buyer := Operator{ID: 720, Name: "T7 采购"}
	procurementManager := Operator{ID: 721, Name: "T7 采购经理"}
	shipping := Operator{ID: 730, Name: "T7 船运"}
	shippingManager := Operator{ID: 731, Name: "T7 船运经理"}
	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{Title: "T7 销售议价", Lines: []SourcingLineInput{{Product: "冷轧钢卷", Quantity: "20", QuantityUnit: "TON", Port: "Los Angeles"}}}, sales)
	if err != nil {
		t.Fatal(err)
	}
	caseID, lineID := created.Head.ID, created.Lines[0].ID
	if _, err = svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{lineID}, "", sales); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.JoinSourcingCase(ctx, tenantID, caseID, buyer); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.RequestPrimarySourcingCase(ctx, tenantID, caseID, buyer); err != nil {
		t.Fatal(err)
	}

	var rfqID int64
	err = pool.QueryRow(ctx, `INSERT INTO factory_rfqs(tenant_id,case_id,rfq_no,supplier_id,supplier_name,currency,status,created_by,created_by_name) VALUES($1,$2,'RFQ-T7',7701,'T7 供应商','USD','DRAFT',$3,$4) RETURNING id`, tenantID, caseID, buyer.ID, buyer.Name).Scan(&rfqID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO factory_rfq_lines(tenant_id,factory_rfq_id,sourcing_line_id,line_no,qty,uom_code) VALUES($1,$2,$3,1,20,'TON')`, tenantID, rfqID, lineID); err != nil {
		t.Fatal(err)
	}
	quote, err := svc.CreateSupplierQuote(ctx, tenantID, NewSupplierQuote{FactoryRFQID: rfqID, Currency: "USD", Incoterm: "FOB", PaymentTerms: "T/T", ValidUntil: "2026-12-31", Source: "MANUAL", Lines: []SupplierQuoteLineInput{{SourcingLineID: lineID, Qty: "20", UnitPrice: "600", MOQ: "5", LeadTime: "15"}}}, buyer)
	if err != nil {
		t.Fatal(err)
	}
	var quoteLineID int64
	if err = pool.QueryRow(ctx, `SELECT id FROM supplier_quote_lines WHERE tenant_id=$1 AND supplier_quote_id=$2`, tenantID, quote.ID).Scan(&quoteLineID); err != nil {
		t.Fatal(err)
	}
	procurement, err := svc.CreateProcurementPlan(ctx, tenantID, NewProcurementPlan{CaseID: caseID, ManagerNote: "采购推荐", Selections: []ProcurementPlanSelectionInput{{SourcingLineID: lineID, SupplierQuoteLineID: quoteLineID, SelectionType: "RECOMMENDED", Priority: 1, Reason: "价格合适"}}}, procurementManager)
	if err != nil {
		t.Fatal(err)
	}
	procurement, err = svc.SubmitProcurementPlanToSales(ctx, tenantID, procurement.Header.ID, procurementManager)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.StartSourcingShippingTask(ctx, tenantID, caseID, shipping); err != nil {
		t.Fatal(err)
	}
	shippingView, err := svc.AddSourcingShippingOption(ctx, tenantID, NewSourcingShippingOption{CaseID: caseID, CarrierForwarder: "T7 船公司", PortOfLoading: "上海", PortOfDischarge: "Los Angeles", QuotedAt: "2026-08-30", EstimatedDeparture: "2026-09-10", EstimatedArrival: "2026-10-02", ValidUntil: "2026-09-05", Lines: []NewSourcingShippingOptionLine{{SourcingLineID: lineID, Currency: "USD", ChargeBasis: "PER_TON", UnitRate: "40", TotalFreight: "800"}}}, shipping)
	if err != nil {
		t.Fatal(err)
	}
	optionLine := shippingView.Options[0].Lines[0]
	shippingView, err = svc.AddSourcingShippingOption(ctx, tenantID, NewSourcingShippingOption{CaseID: caseID, CarrierForwarder: "错误目的港船公司", PortOfLoading: "上海", PortOfDischarge: "Long Beach", QuotedAt: "2026-08-30", EstimatedDeparture: "2026-09-11", EstimatedArrival: "2026-10-03", ValidUntil: "2026-09-05", Lines: []NewSourcingShippingOptionLine{{SourcingLineID: lineID, Currency: "USD", ChargeBasis: "PER_TON", UnitRate: "35", TotalFreight: "700"}}}, shipping)
	if err != nil {
		t.Fatal(err)
	}
	var incompatibleOptionLineID int64
	for _, option := range shippingView.Options {
		if option.Option.CarrierForwarder == "错误目的港船公司" {
			incompatibleOptionLineID = option.Lines[0].ID
		}
	}
	shippingPlan, err := svc.CreateSourcingShippingPlan(ctx, tenantID, NewShippingPlan{CaseID: caseID, ManagerNote: "船运候选", Selections: []ShippingPlanSelectionInput{{SourcingLineID: lineID, ShippingOptionLineID: optionLine.ID, SelectionType: "RECOMMENDED", Priority: 1, Reason: "到港快"}, {SourcingLineID: lineID, ShippingOptionLineID: incompatibleOptionLineID, SelectionType: "BACKUP", Priority: 2, Reason: "价格低"}}}, shippingManager)
	if err != nil {
		t.Fatal(err)
	}
	shippingPlan, err = svc.SubmitSourcingShippingPlanToSales(ctx, tenantID, shippingPlan.Header.ID, shippingManager)
	if err != nil {
		t.Fatal(err)
	}

	newPlan := func(price string) (SalesPlanView, error) {
		shippingInputs := make([]SalesShippingOptionInput, 0, len(shippingPlan.Items))
		for _, item := range shippingPlan.Items {
			shippingInputs = append(shippingInputs, SalesShippingOptionInput{ShippingPlanItemIDs: []int64{item.ID}, CustomerCurrency: "USD", CustomerFreightAmount: item.TotalFreight})
		}
		return svc.CreateSalesPlan(ctx, tenantID, NewSalesPlan{CaseID: caseID, ProcurementPlanID: procurement.Header.ID, ShippingPlanID: shippingPlan.Header.ID, ValidUntil: "2026-09-30", CustomerNote: "含独立采购与船运候选", Items: []SalesPlanItemInput{{SourcingLineID: lineID, ProcurementPlanItemID: procurement.Items[0].ID, OptionType: "PRIMARY", Priority: 1, CustomerCurrency: "USD", CustomerUnitPrice: price, PromisedDeliveryDate: "2026-10-15"}}, ShippingOptions: shippingInputs}, sales)
	}
	plan1, err := newPlan("720")
	if err != nil {
		t.Fatal(err)
	}
	if plan1.Header.VersionNo != 1 || len(plan1.Items) != 1 {
		t.Fatalf("unexpected first sales plan: %+v", plan1)
	}
	if _, err = svc.CreateSalesPlan(ctx, tenantID, NewSalesPlan{CaseID: caseID, ProcurementPlanID: procurement.Header.ID, ValidUntil: "2026-09-30", Items: []SalesPlanItemInput{{SourcingLineID: lineID, ProcurementPlanItemID: procurement.Items[0].ID, OptionType: "PRIMARY", Priority: 1, CustomerCurrency: "USD", CustomerUnitPrice: "725"}}}, otherSales); err == nil || !strings.Contains(err.Error(), "SC_CASE_NOT_FOUND") {
		t.Fatalf("other sales ownership error = %v", err)
	}
	plan2, err := newPlan("715")
	if err != nil {
		t.Fatal(err)
	}
	if plan2.Header.VersionNo != 2 {
		t.Fatalf("sales plan version=%d want 2", plan2.Header.VersionNo)
	}
	old, err := svc.GetSalesPlan(ctx, tenantID, plan1.Header.ID)
	if err != nil || old.Header.Status != "SUPERSEDED" {
		t.Fatalf("old sales plan status=%q err=%v", old.Header.Status, err)
	}

	// T8: the customer may select only part of the presented products. The
	// selected combination is frozen and sent back to the original buyer and
	// shipping owner; only both resolutions complete the final recheck.
	groupKey := customerShipmentGroupKey(plan2.Items[0].SupplierID, plan2.Items[0].FactoryID)
	var compatibleSalesShippingID, incompatibleSalesShippingID int64
	for _, option := range plan2.ShippingOptions {
		if option.Header.PortOfDischarge == "Los Angeles" {
			compatibleSalesShippingID = option.Header.ID
		} else {
			incompatibleSalesShippingID = option.Header.ID
		}
	}
	if _, badErr := svc.ConfirmCustomerSelection(ctx, tenantID, ConfirmCustomerSelectionInput{CaseID: caseID, SalesPlanID: plan2.Header.ID, SalesPlanItemIDs: []int64{plan2.Items[0].ID}, ShipmentChoices: []CustomerShipmentChoiceInput{{ShipmentGroupKey: groupKey, SalesShippingOptionID: incompatibleSalesShippingID}}, CustomerConfirmedAt: "2026-08-31T09:00:00Z"}, sales); badErr == nil || !strings.Contains(badErr.Error(), "SC_CUSTOMER_SHIPMENT_DESTINATION") {
		t.Fatalf("incompatible destination error=%v", badErr)
	}
	selection, err := svc.ConfirmCustomerSelection(ctx, tenantID, ConfirmCustomerSelectionInput{CaseID: caseID, SalesPlanID: plan2.Header.ID, SalesPlanItemIDs: []int64{plan2.Items[0].ID}, ShipmentChoices: []CustomerShipmentChoiceInput{{ShipmentGroupKey: groupKey, SalesShippingOptionID: compatibleSalesShippingID}}, CustomerContact: "客户联系人", ConfirmationNote: "客户确认该产品和船运", CustomerConfirmedAt: "2026-08-31T10:00:00Z"}, sales)
	if err != nil || selection.Header.Status != "INTENT_RECHECK_PENDING" || len(selection.Items) != 1 || len(selection.Shipments) != 1 || len(selection.Tasks) != 2 {
		t.Fatalf("customer selection=%+v err=%v", selection, err)
	}
	var finalProcurementReworkID, finalShippingReworkID int64
	for _, task := range selection.Tasks {
		if task.TaskDomain == "PROCUREMENT" {
			finalProcurementReworkID = task.ProcurementReworkID
		}
		if task.TaskDomain == "SHIPPING" {
			finalShippingReworkID = task.ShippingReworkID
		}
	}
	if finalProcurementReworkID == 0 || finalShippingReworkID == 0 {
		t.Fatalf("missing final recheck links: %+v", selection.Tasks)
	}
	if err = svc.ResolveProcurementRework(ctx, tenantID, finalProcurementReworkID, ProcurementReworkResolution{
		Note: "最终价格与交期已确认", Currency: "USD", UnitPrice: "2", AvailableQty: "20",
		LeadTime: 5, DeliveryDate: "2026-09-05", PaymentTerms: "T/T", Incoterm: "FOB", ValidUntil: "2026-09-03",
	}, buyer); err != nil {
		t.Fatal(err)
	}
	selections, err := svc.ListCustomerSelections(ctx, tenantID, caseID, sales)
	if err != nil || selections[0].Header.Status != "INTENT_RECHECK_PENDING" {
		t.Fatalf("selection should wait for shipping: %+v err=%v", selections, err)
	}
	if err = svc.ResolveShippingRework(ctx, tenantID, finalShippingReworkID, ShippingReworkResolution{
		Note: "最终船期与运费已确认", Currency: "USD", FreightAmount: "200",
		EstimatedDeparture: "2026-09-01", EstimatedArrival: "2026-09-20", ValidUntil: "2026-09-03",
	}, shipping); err != nil {
		t.Fatal(err)
	}
	selections, err = svc.ListCustomerSelections(ctx, tenantID, caseID, sales)
	if err != nil || selections[0].Header.Status != "AWAITING_CUSTOMER_CONFIRMATION" {
		t.Fatalf("selection should await customer confirmation: %+v err=%v", selections, err)
	}
	selection = selections[0]
	accepted, err := svc.DecideCustomerSelection(ctx, tenantID, DecideCustomerSelectionInput{
		CaseID: caseID, SelectionID: selection.Header.ID, Accepted: true,
		CustomerContact: "客户联系人", DecisionNote: "客户接受最终价格和船期", DecidedAt: "2026-09-01T12:00:00Z",
		ItemPrices:     []FinalCustomerItemPriceInput{{SelectionItemID: selection.Items[0].ID, Currency: "USD", UnitPrice: "12"}},
		ShipmentPrices: []FinalCustomerShipmentPriceInput{{SelectionShipmentID: selection.Shipments[0].Header.ID, Currency: "USD", FreightAmount: "260"}},
	}, sales)
	if err != nil || accepted.Header.Status != "CUSTOMER_CONFIRMED" {
		t.Fatalf("selection should be customer confirmed: %+v err=%v", accepted, err)
	}

	feedback, err := svc.AddCustomerFeedback(ctx, tenantID, CustomerFeedbackInput{CaseID: caseID, SalesPlanID: plan2.Header.ID, ContactName: "客户联系人", Channel: "PHONE", Result: "REQUOTE_REQUIRED", Summary: "希望再降低运费", ContactedAt: "2026-08-30T12:00:00Z"}, sales)
	if err != nil || len(feedback) != 1 || feedback[0].Result != "REQUOTE_REQUIRED" {
		t.Fatalf("feedback=%+v err=%v", feedback, err)
	}
	procurementRework, err := svc.CreateSalesProcurementRework(ctx, tenantID, plan2.Header.ID, NewProcurementRework{SupplierQuoteLineID: quoteLineID, RequestType: "RENEGOTIATE", Reason: "客户要求降采购价"}, sales)
	if err != nil || procurementRework.AssignedBuyerID != buyer.ID {
		t.Fatalf("procurement rework=%+v err=%v", procurementRework, err)
	}
	shippingRework, err := svc.CreateShippingRework(ctx, tenantID, ShippingReworkInput{CaseID: caseID, SalesPlanID: plan2.Header.ID, ShippingOptionLineID: optionLine.ID, RequestType: "RENEGOTIATE", Reason: "客户要求降运费"}, sales)
	if err != nil || shippingRework.AssignedShippingID != shipping.ID {
		t.Fatalf("shipping rework=%+v err=%v", shippingRework, err)
	}
	tasks, err := svc.ListMyShippingReworks(ctx, tenantID, shipping)
	if err != nil || len(tasks) != 1 || tasks[0].CaseNo == "" {
		t.Fatalf("shipping tasks=%+v err=%v", tasks, err)
	}
	if err = svc.ResolveShippingRework(ctx, tenantID, shippingRework.ID, ShippingReworkResolution{Note: "已更新报价"}, shippingManager); err == nil || !strings.Contains(err.Error(), "SC_SHIPPING_REWORK_ASSIGNEE") {
		t.Fatalf("wrong shipping assignee error=%v", err)
	}
	if err = svc.ResolveShippingRework(ctx, tenantID, shippingRework.ID, ShippingReworkResolution{Note: "已更新报价"}, shipping); err != nil {
		t.Fatal(err)
	}
}
