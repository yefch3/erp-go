package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestExecutionRequoteReusesEditableSupplierDraft(t *testing.T) {
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
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_draft_files WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_execution_files WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_execution_inquiry_files WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_execution_supplier_quotes WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()

	var requirementID, firstQuoteID, revisedQuoteID int64
	contractID := tenantID%1000000 + 9000
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements
		(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,
		 product_id,product_code,product_name,uom_id,uom_code,required_qty,status)
		VALUES ($1,$2,'CT-REQUOTE-RETRY',1,$2,11,'P-11','热轧钢板',7,'MT',5,'WAITING_REQUOTE') RETURNING id`,
		tenantID, contractID).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	for index, quote := range []struct {
		price string
		id    *int64
	}{{"50", &firstQuoteID}, {"55", &revisedQuoteID}} {
		if err = pool.QueryRow(ctx, `INSERT INTO purchase_execution_supplier_quotes
			(tenant_id,requirement_id,supplier_id,supplier_code,supplier_name,currency,unit_price,selected,selected_by_id,quote_category,incoterm,calculated_unit_price)
			VALUES ($1,$2,6,'S6','测试工厂','CNY',$3,$4,$5,'FOB_CNY','FOB',$3) RETURNING id`, tenantID, requirementID, quote.price, index == 0, func() int64 {
			if index == 0 {
				return 5
			}
			return 0
		}()).Scan(quote.id); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{
		6: {ID: 6, Code: "S6", Name: "测试工厂", Currency: "CNY", Status: "ACTIVE"},
	}})
	input := func(price string, quoteID int64) CreateOrderInput {
		return CreateOrderInput{
			SupplierID: 6, Currency: "CNY", ExpectedDate: "2026-10-20",
			FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "CUSTOM",
			DeliveryAddress: "按外销合同约定", Remark: "T/T",
			Lines: []OrderLine{{RequirementID: requirementID, Qty: "5", UnitPrice: price, ExecutionQuoteID: quoteID}},
		}
	}
	op := Operator{ID: 5, Name: "采购员"}
	first, err := svc.CreateOrder(ctx, tenantID, input("50", firstQuoteID), op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET status='REJECTED',reject_reason='重新询价' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SelectExecutionSupplierQuote(ctx, tenantID, requirementID, revisedQuoteID, op); err != nil {
		t.Fatal(err)
	}
	cleared, err := svc.SelectExecutionSupplierQuote(ctx, tenantID, requirementID, revisedQuoteID, op)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Selected || cleared.SelectedByID != 0 {
		t.Fatalf("clicking selected quote should clear it, got %#v", cleared)
	}
	selectedAgain, err := svc.SelectExecutionSupplierQuote(ctx, tenantID, requirementID, revisedQuoteID, op)
	if err != nil {
		t.Fatal(err)
	}
	if !selectedAgain.Selected || selectedAgain.SelectedByID != op.ID {
		t.Fatalf("clicking cleared quote should select it again, got %#v", selectedAgain)
	}

	revised, err := svc.CreateOrder(ctx, tenantID, input("55", revisedQuoteID), op)
	if err != nil {
		t.Fatalf("rejected final requote should be reusable: %v", err)
	}
	if revised.ID != first.ID || revised.PoNo != "CT-REQUOTE-RETRY" || revised.Status != "DRAFT" {
		t.Fatalf("revised order=%#v, first=%#v", revised, first)
	}
	retried, err := svc.CreateOrder(ctx, tenantID, input("55", revisedQuoteID), op)
	if err != nil || retried.ID != first.ID {
		t.Fatalf("retry should reuse draft %d, got %#v err=%v", first.ID, retried, err)
	}
	var orderCount, lineCount int
	var storedPrice string
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM purchase_orders WHERE tenant_id=$1`, tenantID).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*),max(unit_price)::text FROM purchase_order_items WHERE tenant_id=$1`, tenantID).Scan(&lineCount, &storedPrice); err != nil {
		t.Fatal(err)
	}
	if orderCount != 1 || lineCount != 1 || storedPrice != "55.0000" {
		t.Fatalf("orders=%d lines=%d price=%s", orderCount, lineCount, storedPrice)
	}
	var storedQuoteID int64
	if err = pool.QueryRow(ctx, `SELECT execution_quote_id FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2`, tenantID, revised.ID).Scan(&storedQuoteID); err != nil || storedQuoteID != revisedQuoteID {
		t.Fatalf("stored execution quote=%d want=%d err=%v", storedQuoteID, revisedQuoteID, err)
	}
	if err = svc.DeleteExecutionSupplierQuote(ctx, tenantID, requirementID, revisedQuoteID); err == nil {
		t.Fatal("quote referenced by an order must remain as an audit record")
	}
}
