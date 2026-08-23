package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestCloseWithShortfall 走的就是业务举的那个例子：订 100 吨，工厂只发
// 80 吨。关掉这单的时候，剩下的 20 吨要能回到待采购清单，好让人另找
// 一家工厂开新单。
func TestCloseWithShortfall(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, tbl := range []string{"purchase_inspections", "purchase_receipt_exceptions", "purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
	}()

	// 需求 100 吨，已经全部下给了一张单。
	var reqID, poID, itemID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements
		(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status)
		VALUES ($1,1,'CT-SHORT',1,$1,11,'P-11','钢卷',7,'TON',100,100,'ORDERED') RETURNING id`, tenantID).Scan(&reqID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders
		(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,status,send_status,buyer_id,buyer_name,ordered_at)
		VALUES ($1,'PO-SHORT',9,'S9','钢厂甲','CNY',52000,'PARTIALLY_RECEIVED','SENT',77,'采购员',now()) RETURNING id`, tenantID).Scan(&poID); err != nil {
		t.Fatal(err)
	}
	// 订 100，到 80。
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items
		(tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount,received_qty)
		VALUES ($1,$2,$3,11,'P-11','钢卷',7,'TON',100,520,52000,80) RETURNING id`, tenantID, poID, reqID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "采购员"}

	// 不说剩下的怎么办：不让关。
	if err := svc.CloseOrder(ctx, tenantID, poID, CloseOrderInput{}, op); code(err) != "PO_SHORTFALL_ACTION_REQUIRED" {
		t.Fatalf("没交代剩下的 20 吨就不该放行，得到 %v", err)
	}
	// 选了「另找工厂」但不写原因：也不让关。半年后翻账的人得知道出了什么事。
	if err := svc.CloseOrder(ctx, tenantID, poID, CloseOrderInput{ShortfallAction: "REORDER"}, op); code(err) != "PO_CLOSE_NOTE_REQUIRED" {
		t.Fatalf("没写原因就不该放行，得到 %v", err)
	}
	// 悬着的到货异常仍然拦得住——这道闸没被新逻辑绕过去。
	exc, err := svc.ReportReceiptException(ctx, tenantID, poID, ReceiptException{
		POItemID: itemID, Type: "SHORT_SHIPMENT", Qty: "20", Description: "工厂产能不足",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CloseOrder(ctx, tenantID, poID, CloseOrderInput{
		ShortfallAction: "REORDER", Note: "工厂只能供 80 吨",
	}, op); code(err) != "PO_OPEN_ISSUES" {
		t.Fatalf("异常没处理完不该放行，得到 %v", err)
	}
	if _, err := svc.ResolveReceiptException(ctx, tenantID, poID, exc.ID, "余量另采", op); err != nil {
		t.Fatal(err)
	}

	// 正式关单：剩下的 20 吨放回待采购。
	if err := svc.CloseOrder(ctx, tenantID, poID, CloseOrderInput{
		ShortfallAction: "REORDER", Note: "钢厂甲产能不足，只供 80 吨，余量另找钢厂乙",
	}, op); err != nil {
		t.Fatal(err)
	}

	// 单关掉了，而且记下了当初的判断。
	head, err := svc.GetOrder(ctx, tenantID, poID)
	if err != nil {
		t.Fatal(err)
	}
	if head.ClosedAt == "" || head.ShortfallAction != "REORDER" || head.CloseNote == "" {
		t.Fatalf("关单信息没记全：%+v", head)
	}
	// 订单明细的 100 一个字没改——「订了 100」是事实。
	var orderedQty string
	if err := pool.QueryRow(ctx, `SELECT qty::text FROM purchase_order_items WHERE id=$1`, itemID).Scan(&orderedQty); err != nil {
		t.Fatal(err)
	}
	if orderedQty != "100.0000" {
		t.Fatalf("订单明细数量不该被改动：%s", orderedQty)
	}

	// 关键：那 20 吨回到了待采购清单，可以给另一家工厂开新单。
	var ordered, required string
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT ordered_qty::text, required_qty::text, status FROM purchase_requirements WHERE id=$1`,
		reqID).Scan(&ordered, &required, &status); err != nil {
		t.Fatal(err)
	}
	if ordered != "80.0000" {
		t.Fatalf("已下单量该退回到 80，实际 %s", ordered)
	}
	if status != "PARTIALLY_ORDERED" {
		t.Fatalf("需求该回到「部分已订」好让人继续买，实际 %s", status)
	}

	// 反面：选「不要了」的单，差额不回池子。
	var reqID2, poID2 int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements
		(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status)
		VALUES ($1,2,'CT-DROP',1,$2,11,'P-11','钢卷',7,'TON',50,50,'ORDERED') RETURNING id`, tenantID, tenantID+1).Scan(&reqID2); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders
		(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,status,buyer_id,buyer_name,ordered_at)
		VALUES ($1,'PO-DROP',9,'S9','钢厂甲','CNY',26000,'PARTIALLY_RECEIVED',77,'采购员',now()) RETURNING id`, tenantID).Scan(&poID2); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO purchase_order_items
		(tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount,received_qty)
		VALUES ($1,$2,$3,11,'P-11','钢卷',7,'TON',50,520,26000,45)`, tenantID, poID2, reqID2); err != nil {
		t.Fatal(err)
	}
	if err := svc.CloseOrder(ctx, tenantID, poID2, CloseOrderInput{
		ShortfallAction: "DROPPED", Note: "客户把这批减到 45 吨",
	}, op); err != nil {
		t.Fatal(err)
	}
	var ordered2 string
	if err := pool.QueryRow(ctx, `SELECT ordered_qty::text FROM purchase_requirements WHERE id=$1`, reqID2).Scan(&ordered2); err != nil {
		t.Fatal(err)
	}
	if ordered2 != "50.0000" {
		t.Fatalf("选了「不要了」就不该退回池子，实际已下单量变成 %s", ordered2)
	}
}
