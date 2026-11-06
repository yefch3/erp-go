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

func TestExecutionRequoteKeepsSeparateDeliveryGroups(t *testing.T) {
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
	var secondReq, secondQuote int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,status) VALUES ($1,$2,'CT-REQUOTE-RETRY',1,$2::bigint+1,12,'P-12','B',7,'MT',5,'WAITING_REQUOTE') RETURNING id`, tenantID, contractID).Scan(&secondReq); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_execution_supplier_quotes (tenant_id,requirement_id,supplier_id,supplier_code,supplier_name,currency,unit_price,selected,selected_by_id,quote_category,incoterm,calculated_unit_price) VALUES ($1,$2,6,'S6','测试工厂','CNY',50,true,5,'FOB_CNY','FOB',50) RETURNING id`, tenantID, secondReq).Scan(&secondQuote); err != nil {
		t.Fatal(err)
	}
	next := input("50", secondQuote)
	next.Lines[0].RequirementID = secondReq
	next.ExpectedDate = "2026-10-30"
	second, err := svc.CreateOrder(ctx, tenantID, next, op)
	if err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2 AND requirement_id=$3`, tenantID, first.ID, requirementID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || remaining != 1 {
		t.Fatalf("two delivery groups produced same draft=%t; first group's surviving lines=%d", first.ID == second.ID, remaining)
	}
	for _, retry := range []struct {
		in CreateOrderInput
		id int64
	}{{input("50", firstQuoteID), first.ID}, {next, second.ID}} {
		got, err := svc.CreateOrder(ctx, tenantID, retry.in, op)
		if err != nil || got.ID != retry.id {
			t.Fatalf("retry: got %d want %d error %v", got.ID, retry.id, err)
		}
	}
	combined := input("50", firstQuoteID)
	combined.Lines = append(combined.Lines, next.Lines...)
	if _, err := svc.CreateOrder(ctx, tenantID, combined, op); err == nil {
		t.Fatal("overlapping groups must not create a third draft")
	}
	file, err := svc.UploadOrderDraftFile(ctx, tenantID, first.ID, "test.txt", "text/plain", []byte("retry fixture"), op)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.UploadOrderDraftFile(ctx, tenantID, first.ID, "test.txt", "text/plain", []byte("retry fixture"), op)
	if err != nil || file.ID != again.ID {
		t.Fatalf("attachment retry: %v", err)
	}
	if first.PoNo == second.PoNo {
		t.Fatal("separate orders must have separate numbers")
	}

}

func TestContractVerificationRetry(t *testing.T) {
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
	svc.approvals = &d7ApprovalStub{}
	_, instance, err := svc.SubmitOrder(ctx, tenantID, first.ID, op)
	if err != nil {
		t.Fatal(err)
	}
	state, err := svc.ApplyApprovalDecision(ctx, tenantID, first.ID, instance, "APPROVED", "", noopClaim)
	if err != nil || state != "ORDERED" {
		t.Fatalf("approval: %s %v", state, err)
	}
	_, err = svc.SaveOrderContract(ctx, tenantID, first.ID, "TEST-PO", "T/T", contractKeyPrefix(tenantID, first.ID)+"signed.pdf", "signed.pdf", op)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.VerifyOrderContract(ctx, tenantID, first.ID, op)
	if err != nil || result.PaymentRequestedAt == "" {
		t.Fatalf("finance verification: %v", err)
	}
	// Emulate a successful first service update followed by an interrupted request.
	again, err := svc.VerifyOrderContract(ctx, tenantID, first.ID, Operator{ID: 6, Name: "重试财务"})
	if err != nil {
		t.Fatalf("verification retry failed: %v", err)
	}
	if again.ContractVerifiedAt != result.ContractVerifiedAt || again.PaymentRequestedAt != result.PaymentRequestedAt || again.ContractVerifiedByName != op.Name || again.PaymentRequestedByName != op.Name {
		t.Fatal("verification retry overwrote the original audit")
	}
}
