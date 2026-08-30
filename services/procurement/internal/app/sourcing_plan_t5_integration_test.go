package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestProcurementManagerPlanVersionsAndTargetedRework(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed T5 plan test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"procurement_rework_requests", "procurement_plan_items", "procurement_plans", "supplier_quote_lines", "supplier_quotes", "factory_rfq_lines", "factory_rfqs", "sourcing_case_changes", "sourcing_procurement_participants", "sourcing_lines", "sourcing_cases"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	sales := Operator{ID: 510, Name: "负责销售甲"}
	manager := Operator{ID: 520, Name: "采购经理"}
	buyerA := Operator{ID: 521, Name: "采购甲"}
	buyerB := Operator{ID: 522, Name: "采购乙"}
	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "T5 经理统一方案",
		Lines: []SourcingLineInput{
			{Product: "冷轧钢卷", Quantity: "20", QuantityUnit: "TON"},
			{Product: "镀锌钢板", Quantity: "10", QuantityUnit: "TON"},
		},
	}, sales)
	if err != nil {
		t.Fatal(err)
	}
	caseID := created.Head.ID
	lineA, lineB := created.Lines[0].ID, created.Lines[1].ID
	if _, err = svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{lineA, lineB}, "", sales); err != nil {
		t.Fatal(err)
	}
	for _, buyer := range []Operator{buyerA, buyerB} {
		if _, err = svc.JoinSourcingCase(ctx, tenantID, caseID, buyer); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = svc.RequestPrimarySourcingCase(ctx, tenantID, caseID, buyerA); err != nil {
		t.Fatal(err)
	}

	seedQuote := func(no, supplier string, supplierID, lineID int64, price string, buyer Operator) int64 {
		var rfqID int64
		if err := pool.QueryRow(ctx, `INSERT INTO factory_rfqs
(tenant_id,case_id,rfq_no,supplier_id,supplier_name,currency,status,created_by,created_by_name)
VALUES ($1,$2,$3,$4,$5,'USD','DRAFT',$6,$7) RETURNING id`,
			tenantID, caseID, no, supplierID, supplier, buyer.ID, buyer.Name).Scan(&rfqID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO factory_rfq_lines
(tenant_id,factory_rfq_id,sourcing_line_id,line_no,qty,uom_code)
VALUES ($1,$2,$3,1,20,'TON')`, tenantID, rfqID, lineID); err != nil {
			t.Fatal(err)
		}
		quote, err := svc.CreateSupplierQuote(ctx, tenantID, NewSupplierQuote{
			FactoryRFQID: rfqID, Currency: "USD", Incoterm: "FOB", PaymentTerms: "T/T",
			ValidUntil: time.Now().AddDate(0, 1, 0).Format("2006-01-02"), Source: "MANUAL",
			Lines: []SupplierQuoteLineInput{{SourcingLineID: lineID, Qty: "20", UnitPrice: price, MOQ: "5", LeadTime: "15"}},
		}, buyer)
		if err != nil {
			t.Fatal(err)
		}
		var quoteLineID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM supplier_quote_lines WHERE tenant_id=$1 AND supplier_quote_id=$2`, tenantID, quote.ID).Scan(&quoteLineID); err != nil {
			t.Fatal(err)
		}
		return quoteLineID
	}
	quoteA1 := seedQuote("RFQ-T5-A1", "供应商甲", 6101, lineA, "600", buyerA)
	quoteA2 := seedQuote("RFQ-T5-A2", "供应商乙", 6102, lineA, "605", buyerB)
	quoteB1 := seedQuote("RFQ-T5-B1", "供应商丙", 6103, lineB, "580", buyerA)

	_, err = svc.CreateProcurementPlan(ctx, tenantID, NewProcurementPlan{
		CaseID: caseID, ManagerNote: "缺少第二个产品的推荐项",
		Selections: []ProcurementPlanSelectionInput{{SourcingLineID: lineA, SupplierQuoteLineID: quoteA1, SelectionType: "RECOMMENDED", Priority: 1, Reason: "价格最低"}},
	}, manager)
	if err == nil || !strings.Contains(err.Error(), "SC_PLAN_INCOMPLETE") {
		t.Fatalf("incomplete plan error = %v", err)
	}

	plan1, err := svc.CreateProcurementPlan(ctx, tenantID, NewProcurementPlan{
		CaseID: caseID, ManagerNote: "价格与供应稳定性综合选择",
		Selections: []ProcurementPlanSelectionInput{
			{SourcingLineID: lineA, SupplierQuoteLineID: quoteA1, SelectionType: "RECOMMENDED", Priority: 1, Reason: "价格最低", Risk: "交期待确认"},
			{SourcingLineID: lineA, SupplierQuoteLineID: quoteA2, SelectionType: "BACKUP", Priority: 1, Reason: "替代供应商"},
			{SourcingLineID: lineB, SupplierQuoteLineID: quoteB1, SelectionType: "RECOMMENDED", Priority: 1, Reason: "条款完整"},
		},
	}, manager)
	if err != nil {
		t.Fatal(err)
	}
	if plan1.Header.VersionNo != 1 || plan1.Header.TargetSalesID != sales.ID || len(plan1.Items) != 3 {
		t.Fatalf("unexpected plan 1: header=%+v items=%d", plan1.Header, len(plan1.Items))
	}
	if plan1.Items[0].AvailableQty == "" || plan1.Items[0].BuyerID == 0 {
		t.Fatalf("quote snapshot incomplete: %+v", plan1.Items[0])
	}

	rework, err := svc.CreateProcurementRework(ctx, tenantID, NewProcurementRework{
		CaseID: caseID, PlanID: plan1.Header.ID, SupplierQuoteLineID: quoteA2,
		RequestType: "RENEGOTIATE", Reason: "备选价格仍偏高",
	}, manager)
	if err != nil {
		t.Fatal(err)
	}
	if rework.AssignedBuyerID != buyerB.ID || rework.SupplierName != "供应商乙" || rework.SourcingLineID != lineA {
		t.Fatalf("rework was not routed to original buyer/supplier/product: %+v", rework)
	}
	if err := svc.ResolveProcurementRework(ctx, tenantID, rework.ID, "错误人员处理", buyerA); err == nil || !strings.Contains(err.Error(), "SC_REWORK_ASSIGNEE_REQUIRED") {
		t.Fatalf("wrong buyer resolve error = %v", err)
	}
	if err := svc.ResolveProcurementRework(ctx, tenantID, rework.ID, "已取得新价格并上传", buyerB); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateProcurementPlan(ctx, tenantID, NewProcurementPlan{
		CaseID: caseID, ManagerNote: "不允许再次选择已退回版本",
		Selections: []ProcurementPlanSelectionInput{
			{SourcingLineID: lineA, SupplierQuoteLineID: quoteA2, SelectionType: "RECOMMENDED", Priority: 1, Reason: "错误重选"},
			{SourcingLineID: lineB, SupplierQuoteLineID: quoteB1, SelectionType: "RECOMMENDED", Priority: 1, Reason: "继续采用"},
		},
	}, manager)
	if err == nil || !strings.Contains(err.Error(), "SC_PLAN_QUOTE_INVALID") {
		t.Fatalf("returned quote must not be selectable, got %v", err)
	}

	plan2, err := svc.CreateProcurementPlan(ctx, tenantID, NewProcurementPlan{
		CaseID: caseID, ManagerNote: "议价后形成第二版",
		Selections: []ProcurementPlanSelectionInput{
			{SourcingLineID: lineA, SupplierQuoteLineID: quoteA1, SelectionType: "RECOMMENDED", Priority: 1, Reason: "继续采用"},
			{SourcingLineID: lineB, SupplierQuoteLineID: quoteB1, SelectionType: "RECOMMENDED", Priority: 1, Reason: "继续采用"},
		},
	}, manager)
	if err != nil {
		t.Fatal(err)
	}
	if plan2.Header.VersionNo != 2 {
		t.Fatalf("plan 2 version = %d, want 2", plan2.Header.VersionNo)
	}
	old, err := svc.GetProcurementPlan(ctx, tenantID, plan1.Header.ID)
	if err != nil || old.Header.Status != "SUPERSEDED" {
		t.Fatalf("old plan status = %q, err=%v", old.Header.Status, err)
	}
	submitted, err := svc.SubmitProcurementPlanToSales(ctx, tenantID, plan2.Header.ID, manager)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Header.Status != "SUBMITTED_TO_SALES" || submitted.Header.TargetSalesID != sales.ID {
		t.Fatalf("submitted plan = %+v", submitted.Header)
	}
	var handoff string
	if err := pool.QueryRow(ctx, `SELECT handoff_status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`, tenantID, caseID).Scan(&handoff); err != nil {
		t.Fatal(err)
	}
	if handoff != "PROCUREMENT_PLAN_SUBMITTED" {
		t.Fatalf("handoff = %q, want PROCUREMENT_PLAN_SUBMITTED", handoff)
	}
}
