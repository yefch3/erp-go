package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestSourcingShippingTaskAndRepeatedCurrentQuotes(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed T6 shipping test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"sourcing_shipping_plan_items", "sourcing_shipping_plans", "sourcing_shipping_option_lines", "sourcing_shipping_options", "sourcing_shipping_participants", "sourcing_shipping_requests", "sourcing_case_changes", "sourcing_lines", "sourcing_cases"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	sales := Operator{ID: 610, Name: "T6 负责销售"}
	shipping := Operator{ID: 620, Name: "T6 船运专员"}
	shippingTwo := Operator{ID: 621, Name: "T6 船运专员二"}
	manager := Operator{ID: 630, Name: "T6 船运经理"}
	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "T6 船运协作",
		Lines: []SourcingLineInput{{Product: "冷轧钢卷", Quantity: "20", QuantityUnit: "TON", Port: "鹿特丹"}, {Product: "热轧钢板", Quantity: "12", QuantityUnit: "TON", Port: "鹿特丹"}},
	}, sales)
	if err != nil {
		t.Fatal(err)
	}
	caseID := created.Head.ID
	if _, err = svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{created.Lines[0].ID, created.Lines[1].ID}, "", sales); err != nil {
		t.Fatal(err)
	}

	collaboration, err := svc.GetSourcingShippingCollaboration(ctx, tenantID, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if collaboration.Request.Status != "WAITING_PARTICIPATION" || collaboration.Request.SalesEmployeeID != sales.ID || collaboration.Request.DestinationPort != "鹿特丹" {
		t.Fatalf("unexpected automatic shipping task: %+v", collaboration.Request)
	}
	if collaboration.Request.CargoSummary == "" {
		t.Fatal("cargo summary was not copied to shipping task")
	}

	collaboration, err = svc.StartSourcingShippingTask(ctx, tenantID, caseID, shipping)
	if err != nil || collaboration.Request.Status != "QUOTING" || len(collaboration.Participants) != 1 {
		t.Fatalf("start shipping task: status=%q err=%v", collaboration.Request.Status, err)
	}
	if _, err = svc.JoinSourcingShippingTask(ctx, tenantID, caseID, shippingTwo); err != nil {
		t.Fatal(err)
	}
	base := NewSourcingShippingOption{
		CaseID: caseID, CarrierForwarder: "测试船公司 A", PortOfLoading: "上海", PortOfDischarge: "鹿特丹",
		QuotedAt: "2026-08-30", EstimatedDeparture: "2026-09-10", EstimatedArrival: "2026-10-10",
		ValidUntil: validFor(30),
		Lines: []NewSourcingShippingOptionLine{
			{SourcingLineID: created.Lines[0].ID, Currency: "USD", ChargeBasis: "PER_TON", UnitRate: "42", TotalFreight: "840"},
			{SourcingLineID: created.Lines[1].ID, Currency: "USD", ChargeBasis: "PER_TON", UnitRate: "46", TotalFreight: "552"},
		},
	}
	collaboration, err = svc.AddSourcingShippingOption(ctx, tenantID, base, shipping)
	if err != nil || collaboration.Request.Status != "MANAGER_REVIEW" || len(collaboration.Options) != 1 {
		t.Fatalf("first shipping option: status=%q options=%d err=%v", collaboration.Request.Status, len(collaboration.Options), err)
	}
	if len(collaboration.Options[0].Lines) != 2 {
		t.Fatalf("expected two cargo prices, got %d", len(collaboration.Options[0].Lines))
	}
	// A second shipping company can quote different prices and timing. T6 keeps
	// both alternatives so sales can present more than one to the customer.
	base.CarrierForwarder = "测试船公司 B"
	base.ServiceOptionName = "十月快船"
	base.EstimatedArrival = "2026-10-04"
	base.Lines[0].UnitRate = "51"
	// Keep every quote consumed later in this test valid relative to the day
	// the suite runs. The same base value is reused for later quote versions.
	base.ValidUntil = validFor(30)
	collaboration, err = svc.AddSourcingShippingOption(ctx, tenantID, base, shipping)
	if err != nil || len(collaboration.Options) != 2 {
		t.Fatalf("second carrier option should be retained: options=%d err=%v", len(collaboration.Options), err)
	}
	// The same carrier may submit a newer version and may quote only part of
	// the cargo. The API keeps the full version chain newest-first so the UI can
	// compare the current quote while still exposing history.
	base.EstimatedArrival = "2026-10-02"
	base.Lines = base.Lines[:1]
	base.Lines[0].UnitRate = "49"
	base.Lines[0].TotalFreight = "980"
	collaboration, err = svc.AddSourcingShippingOption(ctx, tenantID, base, shipping)
	if err != nil || len(collaboration.Options) != 3 {
		t.Fatalf("new carrier quote version should be retained: options=%d err=%v", len(collaboration.Options), err)
	}
	if collaboration.Options[0].Option.CarrierForwarder != "测试船公司 B" || len(collaboration.Options[0].Lines) != 1 || collaboration.Options[0].Lines[0].UnitRate != "49.000000" {
		t.Fatalf("newest partial quote should be returned first: %+v", collaboration.Options[0])
	}
	// The same carrier can offer a second sailing concurrently; it is not an
	// old/new version of the first sailing option.
	base.ServiceOptionName = "十月慢船"
	base.EstimatedArrival = "2026-10-18"
	collaboration, err = svc.AddSourcingShippingOption(ctx, tenantID, base, shipping)
	if err != nil || len(collaboration.Options) != 4 || collaboration.Options[0].Option.VersionNo != 1 {
		t.Fatalf("second sailing option should remain independent: options=%d err=%v", len(collaboration.Options), err)
	}
	// A second employee owns an independent quote version, even when the
	// carrier name happens to be the same.
	base.CarrierForwarder = "测试船公司 A"
	base.ServiceOptionName = "十月直航"
	base.Lines = []NewSourcingShippingOptionLine{
		{SourcingLineID: created.Lines[0].ID, Currency: "USD", ChargeBasis: "PER_TON", UnitRate: "40", TotalFreight: "800"},
		{SourcingLineID: created.Lines[1].ID, Currency: "USD", ChargeBasis: "PER_TON", UnitRate: "44", TotalFreight: "528"},
	}
	collaboration, err = svc.AddSourcingShippingOption(ctx, tenantID, base, shippingTwo)
	if err != nil || len(collaboration.Options) != 5 || collaboration.Options[0].Option.CreatedBy != shippingTwo.ID || collaboration.Options[0].Option.VersionNo != 1 {
		t.Fatalf("second employee must own an independent quote: %+v err=%v", collaboration.Options[0].Option, err)
	}
	selections := make([]ShippingPlanSelectionInput, 0, 2)
	for _, line := range collaboration.Options[0].Lines {
		selections = append(selections, ShippingPlanSelectionInput{SourcingLineID: line.SourcingLineID, ShippingOptionLineID: line.ID, SelectionType: "RECOMMENDED", Priority: 1, Reason: "价格与时间合适"})
	}
	plan, err := svc.CreateSourcingShippingPlan(ctx, tenantID, NewShippingPlan{CaseID: caseID, ManagerNote: "统一推荐方案", Selections: selections}, manager)
	if err != nil || plan.Header.Status != "CONFIRMED" || len(plan.Items) != 2 || plan.Header.TargetSalesID != sales.ID {
		t.Fatalf("create manager plan: %+v err=%v", plan, err)
	}
	plan, err = svc.SubmitSourcingShippingPlanToSales(ctx, tenantID, plan.Header.ID, manager)
	if err != nil || plan.Header.Status != "SUBMITTED_TO_SALES" || plan.Header.TargetSalesID != sales.ID {
		t.Fatalf("submit plan to responsible salesperson: %+v err=%v", plan, err)
	}
}
