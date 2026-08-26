package app

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestCostOfUsesExactDecimalArithmetic(t *testing.T) {
	unit, amount, err := costOf("1.2356", decimal.RequireFromString("3.1250"), "0")
	if err != nil {
		t.Fatal(err)
	}
	if unit.String() != "1.2356" || amount.String() != "3.86125" {
		t.Fatalf("unexpected exact cost: unit=%s amount=%s", unit, amount)
	}
}

// TestReceiveStockUpdatesMovingAverage 验证分批入库按数量加权，不能用两次单价做简单平均。
func TestReceiveStockUpdatesMovingAverage(t *testing.T) {
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	var warehouseID int64
	if err := pool.QueryRow(ctx, `INSERT INTO warehouses (tenant_id,code,name) VALUES ($1,$2,'WH3成本测试仓') RETURNING id`, tenantID, "COST-"+time.Now().Format("150405.000000000")).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM stock_ledger WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM stocks WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM warehouses WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, nil)
	line := ReceiveLine{ProductID: 202, ProductCode: "COST-P", ProductName: "成本产品", UomCode: "件", Qty: "10", UnitCost: "5"}
	if err := svc.ReceiveStock(ctx, tenantID, warehouseID, []ReceiveLine{line}, "首批", Operator{ID: 1}); err != nil {
		t.Fatal(err)
	}
	line.Qty, line.UnitCost = "30", "7"
	if err := svc.ReceiveStock(ctx, tenantID, warehouseID, []ReceiveLine{line}, "第二批", Operator{ID: 1}); err != nil {
		t.Fatal(err)
	}
	var onHand, totalCost, avgCost string
	if err := pool.QueryRow(ctx, `SELECT on_hand_qty::text,total_cost::text,avg_cost::text FROM stocks WHERE tenant_id=$1 AND warehouse_id=$2 AND product_id=202`, tenantID, warehouseID).Scan(&onHand, &totalCost, &avgCost); err != nil {
		t.Fatal(err)
	}
	if onHand != "40.0000" || totalCost != "260.0000" || avgCost != "6.500000" {
		t.Fatalf("unexpected moving average: on_hand=%s total=%s avg=%s", onHand, totalCost, avgCost)
	}
}

// TestFreezeStockSerializesConcurrentChanges 验证并发冻结只能消费一次可用量，且冻结与解冻都会留下流水。
func TestFreezeStockSerializesConcurrentChanges(t *testing.T) {
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	var warehouseID, stockID int64
	if err := pool.QueryRow(ctx, `INSERT INTO warehouses (tenant_id,code,name) VALUES ($1,$2,'WH3并发测试仓') RETURNING id`, tenantID, "WH3-"+time.Now().Format("150405.000000000")).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM stock_ledger WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM stocks WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM warehouses WHERE tenant_id=$1`, tenantID)
	}()
	if err := pool.QueryRow(ctx, `INSERT INTO stocks
		(tenant_id,warehouse_id,product_id,sku_id,product_code,product_name,on_hand_qty,total_cost,cost_currency)
		VALUES ($1,$2,101,0,'WH3-P','并发产品',10,50,'CNY') RETURNING id`, tenantID, warehouseID).Scan(&stockID); err != nil {
		t.Fatal(err)
	}

	svc := New(pool, nil)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- svc.FreezeStock(ctx, tenantID, stockID, "7", "并发冻结验证", Operator{ID: 1})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	succeeded := 0
	for err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("exactly one concurrent freeze must succeed, got %d", succeeded)
	}

	stock, err := svc.GetStock(ctx, tenantID, stockID)
	if err != nil {
		t.Fatal(err)
	}
	if stock.FrozenQty != "7.0000" || stock.AvailableQty != "3.0000" || stock.AvgCost != "5.000000" {
		t.Fatalf("unexpected frozen snapshot: %+v", stock)
	}
	if err := svc.UnfreezeStock(ctx, tenantID, stockID, "2.5", "解除部分质检冻结", Operator{ID: 2}); err != nil {
		t.Fatal(err)
	}
	stock, _ = svc.GetStock(ctx, tenantID, stockID)
	if stock.FrozenQty != "4.5000" || stock.AvailableQty != "5.5000" || stock.TotalCost != "50.0000" {
		t.Fatalf("unfreeze must not alter value: %+v", stock)
	}
	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM stock_ledger WHERE tenant_id=$1 AND stock_id=$2 AND movement IN ('FREEZE','UNFREEZE')`, tenantID, stockID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 {
		t.Fatalf("expected freeze and unfreeze ledger rows, got %d", ledgerCount)
	}
}
