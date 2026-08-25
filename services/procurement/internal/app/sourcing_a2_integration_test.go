package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// A2 收尾的两件事，各钉一根钉子：
//
//  1. 跨币种比价：一家报 USD、一家报 CNY，比较视图给每行配上账本币的换算
//     价——同一把尺；汇率不可得时换算列留空，绝不假装比较过。
//  2. 中标留痕：确认成本方案必须给原因，操作人、时间、原因一起进案件的
//     变更历史——中标常常不是最便宜的那行，"为什么选贵的"八个月后一定
//     有人问。
func TestQuoteComparisonAndAwardReason(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed A2 test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"cost_scenarios", "supplier_quote_lines", "supplier_quotes",
			"factory_rfqs", "sourcing_case_changes", "sourcing_lines", "sourcing_cases",
			"inquiry_template_fields", "inquiry_templates"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	rates := &stubRates{perUSD: map[string]string{"CNY": "7.2"}}
	svc := New(pool, Deps{Rates: rates})
	op := Operator{ID: 77, Name: "Buyer"}

	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "比价询盘",
		Lines: []SourcingLineInput{{Product: "Coil", Quantity: "10"}},
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	caseID := created.Head.ID
	lineID := created.Lines[0].ID

	// Two factories, two currencies: 610 USD/t vs 4300 CNY/t. On the book
	// axis (CNY at 7.2): 4392 vs 4300 — the CNY mill is cheaper, which the
	// raw numbers alone would never say.
	seedQuote := func(rfqNo, supplier, currency, price string) {
		var rfqID, quoteID int64
		if err := pool.QueryRow(ctx, `INSERT INTO factory_rfqs (tenant_id,case_id,rfq_no,supplier_id,supplier_name,currency,response_due_at) VALUES ($1,$2,$3,9,$4,$5,current_date+7) RETURNING id`,
			tenantID, caseID, rfqNo, supplier, currency).Scan(&rfqID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO supplier_quotes (tenant_id,factory_rfq_id,supplier_quote_no,currency,source,created_by,version_no) VALUES ($1,$2,$3,$4,'MANUAL',77,1) RETURNING id`,
			tenantID, rfqID, "SQ-"+rfqNo, currency).Scan(&quoteID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO supplier_quote_lines (tenant_id,supplier_quote_id,sourcing_line_id,qty,unit_price,amount) VALUES ($1,$2,$3,10,$4::numeric,$4::numeric*10)`,
			tenantID, quoteID, lineID, price); err != nil {
			t.Fatal(err)
		}
	}
	seedQuote("RFQ-USD", "US Mill", "USD", "610")
	seedQuote("RFQ-CNY", "国内钢厂", "CNY", "4300")

	rows, err := svc.ListSupplierQuoteComparison(ctx, tenantID, caseID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("comparison: %v rows=%d", err, len(rows))
	}
	byCurrency := map[string]SupplierQuoteComparisonLine{}
	for _, r := range rows {
		byCurrency[r.Row.Currency] = r
	}
	assertDecimal(t, "USD compare", byCurrency["USD"].ComparePrice, "4392") // 610 × 7.2
	assertDecimal(t, "CNY compare", byCurrency["CNY"].ComparePrice, "4300") // ×1
	if byCurrency["USD"].CompareCurrency != "CNY" {
		t.Fatalf("comparison currency should be the book currency, got %q", byCurrency["USD"].CompareCurrency)
	}

	// No fx service → the lens is honestly absent, the record untouched.
	blind := New(pool, Deps{})
	blindRows, err := blind.ListSupplierQuoteComparison(ctx, tenantID, caseID)
	if err != nil || len(blindRows) != 2 {
		t.Fatal(err)
	}
	for _, r := range blindRows {
		if r.Row.Currency != "CNY" && r.ComparePrice != "" {
			t.Fatalf("no rate, no lens — got %q for %s", r.ComparePrice, r.Row.Currency)
		}
	}

	// ---------------------------------------------------------- award reason
	if _, err := pool.Exec(ctx, `UPDATE sourcing_cases SET status='COSTING' WHERE tenant_id=$1 AND id=$2`, tenantID, caseID); err != nil {
		t.Fatal(err)
	}
	var scenarioID int64
	if err := pool.QueryRow(ctx, `INSERT INTO cost_scenarios (tenant_id,case_id,scenario_no,currency,allocation_basis,margin_type,margin_value,fx_rate,fx_rate_at,fx_source,product_total,charge_total,landed_total,margin_total,customer_total,status,created_by) VALUES ($1,$2,'CS-A2-1','USD','TONS','PERCENT',8,1,now(),'STUB',100,0,100,8,108,'DRAFT',77) RETURNING id`,
		tenantID, caseID).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ConfirmCostScenario(ctx, tenantID, scenarioID, "  ", op); err == nil ||
		!strings.Contains(err.Error(), "SC_COST_REASON_REQUIRED") {
		t.Fatalf("award without a reason must be refused, got %v", err)
	}

	view, err := svc.ConfirmCostScenario(ctx, tenantID, scenarioID, "价格第二低，但交期短 10 天", op)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if view.Header.Status != "CONFIRMED" || view.Header.ConfirmReason != "价格第二低，但交期短 10 天" {
		t.Fatalf("reason should ride the scenario: %+v", view.Header)
	}
	if view.Header.VersionNo != 1 {
		t.Fatalf("first cost scenario version = %d, want 1", view.Header.VersionNo)
	}

	// 生成客户报价只建立关联，不能把内部成本状态和客户报价状态混为一谈。
	if err := svc.LinkCustomerQuotation(ctx, tenantID, scenarioID, 9001, "QT-A2-V1"); err != nil {
		t.Fatalf("link customer quotation: %v", err)
	}
	linked, err := svc.GetCostScenario(ctx, tenantID, scenarioID)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Header.Status != "CONFIRMED" || linked.Header.CustomerQuotationID != 9001 {
		t.Fatalf("linked scenario should remain confirmed: %+v", linked.Header)
	}

	// 已有客户报价处理期间，即使数据库里提前存在 V2 草稿，也不允许确认替换 V1。
	var scenarioV2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO cost_scenarios (tenant_id,case_id,scenario_no,version_no,currency,allocation_basis,margin_type,margin_value,fx_rate,fx_rate_at,fx_source,product_total,charge_total,landed_total,margin_total,customer_total,status,created_by) VALUES ($1,$2,'CS-A2-2',2,'USD','TONS','PERCENT',6,1,now(),'STUB',100,0,100,6,106,'DRAFT',77) RETURNING id`,
		tenantID, caseID).Scan(&scenarioV2); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmCostScenario(ctx, tenantID, scenarioV2, "尚未收到客户答复", op); err == nil || !strings.Contains(err.Error(), "客户明确拒绝") {
		t.Fatalf("active customer quotation must block V2 confirmation, got %v", err)
	}

	// 只有正式拒绝事件能让 V1 失效并把案件退回成本测算，之后才能确认 V2。
	if err := svc.ReturnRejectedQuotationToCosting(ctx, tenantID, QuotationRejected{
		QuotationID: 9001, QuotationNo: "QT-A2-V1", CostScenarioID: scenarioID, SourcingCaseID: caseID,
	}, slog.Default()); err != nil {
		t.Fatalf("return rejected quotation: %v", err)
	}
	confirmedV2, err := svc.ConfirmCostScenario(ctx, tenantID, scenarioV2, "客户反馈价格偏高，利润率由 8% 调整为 6%", op)
	if err != nil {
		t.Fatalf("confirm V2: %v", err)
	}
	if confirmedV2.Header.VersionNo != 2 || confirmedV2.Header.Status != "CONFIRMED" {
		t.Fatalf("new cost version should be V2 confirmed: %+v", confirmedV2.Header)
	}
	old, err := svc.GetCostScenario(ctx, tenantID, scenarioID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Header.Status != "SUPERSEDED" {
		t.Fatalf("V1 status = %s, want SUPERSEDED", old.Header.Status)
	}

	// The decision lands in the case's history next to every other judgement.
	var changeCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sourcing_case_changes WHERE tenant_id=$1 AND case_id=$2 AND section='COST' AND action='CONFIRMED' AND after_json::text LIKE '%交期短 10 天%'`,
		tenantID, caseID).Scan(&changeCount); err != nil {
		t.Fatal(err)
	}
	if changeCount != 1 {
		t.Fatalf("the award must land in the change history, found %d rows", changeCount)
	}
}
