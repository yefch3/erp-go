package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 报价转来的采购需求，能不能分几次买完。
//
// 业务的原话：询价比价定下来才给客户报价、才签合同，所以供应商和数量本来
// 就是定好的。定好之后仍然会变——这批工厂只供得了 80 吨，剩下 20 吨要么
// 过些天再向同一家追加，要么另找一家。两条路从前都走不通：换一家被「供应
// 商必须与已确认成本方案一致」挡住，找同一家追加被「该客户报价与供应商已
// 经生成采购单」挡住。
//
// 现在两条都通，但都要写明原因——偏离已确认的成本方案是要留痕的事。
func TestQuotationSourcedReorder(t *testing.T) {
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
		for _, tbl := range []string{"purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
	}()

	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{
		9:  {ID: 9, Code: "S9", Name: "宁波钢厂", Currency: "CNY", Status: "ACTIVE"},
		10: {ID: 10, Code: "S10", Name: "唐山钢厂", Currency: "CNY", Status: "ACTIVE"},
	}})
	op := Operator{ID: 77, Name: "采购员"}

	// 一份报价转来的待采购行：要 100 吨，成本方案定的是宁波钢厂、520。
	newRequirement := func(quotationID int64) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO purchase_requirements
			(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,
			 product_id,product_code,product_name,uom_id,uom_code,required_qty,status,
			 source,quotation_id,quotation_no,cost_scenario_id,
			 supplier_id,supplier_code,supplier_name,source_currency,source_unit_price)
			VALUES ($1,0,'',0,$2,11,'P-11','钢卷',7,'TON',100,'PENDING',
			 'CUSTOMER_QUOTATION',$2,'QT-1',$2,9,'S9','宁波钢厂','CNY',520) RETURNING id`,
			tenantID, quotationID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	order := func(supplierID, reqID int64, qty, price, reason string) CreateOrderInput {
		return CreateOrderInput{
			SupplierID: supplierID, Currency: "CNY", ExpectedDate: "2026-10-01",
			DeliveryLocationType: "PORT", DeliveryPortName: "宁波港",
			SourceChangeReason: reason,
			Lines:              []OrderLine{{RequirementID: reqID, Qty: qty, UnitPrice: price}},
		}
	}
	requirementState := func(id int64) (ordered, status string) {
		if err := pool.QueryRow(ctx,
			`SELECT ordered_qty::text, status FROM purchase_requirements WHERE id=$1`,
			id).Scan(&ordered, &status); err != nil {
			t.Fatal(err)
		}
		return
	}
	// 本测试关注分单和补购规则；用正式审批通过时相同的事务方法推进订单，
	// 不绕过采购需求的锁定与并发校验。
	approve := func(poID int64) {
		t.Helper()
		if err := pgdb.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			return commitOrderRequirements(ctx, svc.q.WithTx(tx), tenantID, poID)
		}); err != nil {
			t.Fatalf("模拟审批通过采购单 %d: %v", poID, err)
		}
	}

	// ---- 剩下的 20 吨换一家工厂 ----
	reqA := newRequirement(tenantID%1000000 + 1)
	firstA, err := svc.CreateOrder(ctx, tenantID, order(9, reqA, "80", "520", ""), op)
	if err != nil {
		t.Fatalf("按成本方案建立 80 吨草稿：%v", err)
	}
	if ordered, status := requirementState(reqA); ordered != "0.0000" || status != "PENDING" {
		t.Fatalf("草稿不能占用采购需求，实际 已下单 %s 状态 %s", ordered, status)
	}
	approve(firstA.ID)
	if ordered, status := requirementState(reqA); ordered != "80.0000" || status != "PARTIALLY_ORDERED" {
		t.Fatalf("订了 80 之后需求该剩 20 可买，实际 已下单 %s 状态 %s", ordered, status)
	}
	// 不说为什么就换厂：不行。报给客户的价是按宁波钢厂算的，换一家利润就变了。
	if _, err := svc.CreateOrder(ctx, tenantID, order(10, reqA, "20", "540", ""), op); code(err) != "PO_SUPPLIER_CHANGE_REASON_REQUIRED" {
		t.Fatalf("换供应商不写原因就不该放行，得到 %v", err)
	}
	changed, err := svc.CreateOrder(ctx, tenantID, order(10, reqA, "20", "540", "宁波钢厂本批只能供 80 吨，余量转唐山钢厂"), op)
	if err != nil {
		t.Fatalf("写了原因就该放行：%v", err)
	}
	approve(changed.ID)
	if ordered, status := requirementState(reqA); ordered != "100.0000" || status != "ORDERED" {
		t.Fatalf("两张单加起来买齐了，需求该是已下单 100，实际 %s / %s", ordered, status)
	}
	// 原因随单存档——半年后翻账的人要能看见当初为什么换了工厂。
	var storedReason, storedSupplier string
	if err := pool.QueryRow(ctx,
		`SELECT source_change_reason, supplier_name FROM purchase_orders WHERE id=$1`,
		changed.ID).Scan(&storedReason, &storedSupplier); err != nil {
		t.Fatal(err)
	}
	if storedReason == "" || storedSupplier != "唐山钢厂" {
		t.Fatalf("换厂的原因没存下来：原因 %q 供应商 %q", storedReason, storedSupplier)
	}

	// ---- 剩下的 20 吨还找同一家追加 ----
	reqB := newRequirement(tenantID%1000000 + 2)
	firstB, err := svc.CreateOrder(ctx, tenantID, order(9, reqB, "80", "520", ""), op)
	if err != nil {
		t.Fatal(err)
	}
	approve(firstB.ID)
	if _, err := svc.CreateOrder(ctx, tenantID, order(9, reqB, "20", "520", ""), op); code(err) != "PO_QUOTATION_REORDER_REASON_REQUIRED" {
		t.Fatalf("向同一家补购不写原因就不该放行，得到 %v", err)
	}
	secondB, err := svc.CreateOrder(ctx, tenantID, order(9, reqB, "20", "520", "首批只排到 80 吨，余量本月底补齐"), op)
	if err != nil {
		t.Fatalf("写了原因的补购该放行：%v", err)
	}
	approve(secondB.ID)
	if ordered, status := requirementState(reqB); ordered != "100.0000" || status != "ORDERED" {
		t.Fatalf("补购之后需求该是已下单 100，实际 %s / %s", ordered, status)
	}

	// ---- 上次没办完留下的草稿，仍然当重复处理 ----
	//
	// 这条不能松：前端认这个错误码，会把那张草稿调出来接着办。松了就会
	// 留下两张半成品，谁也说不清哪张作数。
	reqC := newRequirement(tenantID%1000000 + 3)
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_orders
		(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,
		 status,buyer_id,buyer_name,source_quotation_id)
		VALUES ($1,'PO-DRAFT-C',9,'S9','宁波钢厂','CNY',41600,'DRAFT',77,'采购员',$2)`,
		tenantID, tenantID%1000000+3); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateOrder(ctx, tenantID, order(9, reqC, "80", "520", ""), op); code(err) != "PO_QUOTATION_ALREADY_ORDERED" {
		t.Fatalf("留着草稿时该让前端接着办那一张，得到 %v", err)
	}
	// 连带写了原因也不行——草稿的问题不是「要不要补购」，是「上一张还没办完」。
	if _, err := svc.CreateOrder(ctx, tenantID, order(9, reqC, "80", "520", "补购"), op); code(err) != "PO_QUOTATION_ALREADY_ORDERED" {
		t.Fatalf("写了原因也不该绕过未办完的草稿，得到 %v", err)
	}
}
