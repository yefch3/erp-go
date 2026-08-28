package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 跨服务交接可能在出口已建报价、采购尚未回写时中断。再次点击必须拿回
// 已存在的报价，让网关继续完成关联，而不是撞唯一索引报 500。
func TestCreateQuotationReusesExistingCostScenarioQuote(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantID := time.Now().UnixNano()
	const scenarioID = int64(88001)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM quotation_items WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM quotations WHERE tenant_id=$1", tenantID)
	})

	var quotationID int64
	err = pool.QueryRow(ctx, `INSERT INTO quotations
		(tenant_id, quote_no, customer_id, customer_name, currency,
		 fx_rate, fx_rate_at, fx_source, total_amount, base_amount,
		 source_cost_scenario_id, source_cost_scenario_no)
		VALUES ($1,'QT-ORPHAN-1',9,'测试客户','USD',1,now(),'TEST',100,100,$2,'CS-ORPHAN-1')
		RETURNING id`, tenantID, scenarioID).Scan(&quotationID)
	if err != nil {
		t.Fatal(err)
	}

	svc := New(pool, Deps{})
	quotation, items, err := svc.CreateQuotation(ctx, tenantID, QuotationInput{
		SourceCostScenarioID: scenarioID,
	})
	if err != nil {
		t.Fatalf("重试应返回已创建的报价，而不是再次插入: %v", err)
	}
	if quotation.ID != quotationID || quotation.QuoteNo != "QT-ORPHAN-1" {
		t.Fatalf("返回的不是成本方案已有报价: %+v", quotation)
	}
	if len(items) != 0 {
		t.Fatalf("测试报价没有明细，实际返回 %d 条", len(items))
	}
}
