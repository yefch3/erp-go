package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestInspectionAndCloseLifecycle 走完 A3 的整条链：收货 → 质检（合格生而
// 已处置、不合格开着等处置）→ 处置 → 结案。三道闸各踩一次：没收满不能
// 结案、有未解决异常不能结案、有未解决质检不能结案，都清了才放行。
func TestInspectionAndCloseLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed inspection test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	var requirementID, orderID, itemID, receiptID, strangerOrderID, strangerReceiptID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status) VALUES ($1,1,'CT-QC',1,$1,11,'P-11','QC Coil',7,'TON',10,10,'ORDERED') RETURNING id`, tenantID).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-QC-1',9,'SUP-9','Mill','USD',5200,'ORDERED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items (tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount,received_qty) VALUES ($1,$2,$3,11,'P-11','QC Coil',7,'TON',10,520,5200,10) RETURNING id`, tenantID, orderID, requirementID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_receipts (tenant_id,po_id,receipt_no,warehouse_id,operator_id,operator_name) VALUES ($1,$2,'RCPT-QC-1',3,77,'Buyer') RETURNING id`, tenantID, orderID).Scan(&receiptID); err != nil {
		t.Fatal(err)
	}
	// 另一张单的收货单：质检不许跨单挂靠。
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,status,buyer_id,buyer_name) VALUES ($1,'PO-QC-2',9,'SUP-9','Mill','USD',100,'ORDERED',77,'Buyer') RETURNING id`, tenantID).Scan(&strangerOrderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_receipts (tenant_id,po_id,receipt_no,warehouse_id,operator_id,operator_name) VALUES ($1,$2,'RCPT-QC-2',3,77,'Buyer') RETURNING id`, tenantID, strangerOrderID).Scan(&strangerReceiptID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_inspections WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_receipt_exceptions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_receipt_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_receipts WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{})
	inspector := Operator{ID: 88, Name: "QC"}

	// 合格：生而已处置，不挡任何门。
	pass, err := svc.RecordInspection(ctx, tenantID, orderID, PurchaseInspection{ReceiptID: receiptID, POItemID: itemID, Result: "PASS", InspectedQty: "5"}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if pass.Status != "RESOLVED" || pass.ReceiptNo != "RCPT-QC-1" {
		t.Fatalf("pass inspection=%#v", pass)
	}

	// 不合格但什么都没说：拒收——它既不能驱动扣款也不能作证。
	if _, err = svc.RecordInspection(ctx, tenantID, orderID, PurchaseInspection{ReceiptID: receiptID, Result: "FAIL"}, inspector); err == nil {
		t.Fatal("a FAIL with no defect qty and no note must be refused")
	}
	// 合格却带不合格数量：自相矛盾，拒收。
	if _, err = svc.RecordInspection(ctx, tenantID, orderID, PurchaseInspection{ReceiptID: receiptID, Result: "PASS", DefectQty: "1"}, inspector); err == nil {
		t.Fatal("a PASS with defect qty must be refused")
	}
	// 挂别人的收货单：拒收。
	if _, err = svc.RecordInspection(ctx, tenantID, orderID, PurchaseInspection{ReceiptID: strangerReceiptID, Result: "PASS"}, inspector); err == nil {
		t.Fatal("a receipt from another order must be refused")
	}

	fail, err := svc.RecordInspection(ctx, tenantID, orderID, PurchaseInspection{
		ReceiptID: receiptID, POItemID: itemID, Result: "FAIL", InspectedQty: "5", DefectQty: "2",
		Note:        "表面锈斑",
		Attachments: []ProductionAttachment{{FileName: "rust.jpg", FileURL: "https://files.example/rust.jpg", ContentType: "image/jpeg"}},
	}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if fail.Status != "OPEN" || len(fail.Attachments) != 1 {
		t.Fatalf("fail inspection=%#v", fail)
	}

	buyer := Operator{ID: 77, Name: "Buyer"}
	// 没收满：不能结案。
	if err = svc.CloseOrder(ctx, tenantID, orderID, CloseOrderInput{}, buyer); code(err) != "PO_NOT_CLOSABLE" {
		t.Fatalf("expected PO_NOT_CLOSABLE, got %v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET status='RECEIVED' WHERE tenant_id=$1 AND id=$2`, tenantID, orderID); err != nil {
		t.Fatal(err)
	}

	// 开着一条异常 + 一条不合格质检：门不开，而且拒绝里带着数量。
	exception, err := svc.ReportReceiptException(ctx, tenantID, orderID, ReceiptException{ReceiptID: receiptID, POItemID: itemID, Type: "SHORT_SHIPMENT", Qty: "1", Description: "短一捆"}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	err = svc.CloseOrder(ctx, tenantID, orderID, CloseOrderInput{}, buyer)
	if code(err) != "PO_OPEN_ISSUES" {
		t.Fatalf("expected PO_OPEN_ISSUES, got %v", err)
	}
	if apiErr, ok := err.(*apierr.Error); !ok || apiErr.Meta["openExceptions"] != "1" || apiErr.Meta["openInspections"] != "1" {
		t.Fatalf("meta=%v", err)
	}

	if _, err = svc.ResolveReceiptException(ctx, tenantID, orderID, exception.ID, "供应商补发", buyer); err != nil {
		t.Fatal(err)
	}
	// 异常清了、质检还开着：仍然不行。
	if err = svc.CloseOrder(ctx, tenantID, orderID, CloseOrderInput{}, buyer); code(err) != "PO_OPEN_ISSUES" {
		t.Fatalf("expected PO_OPEN_ISSUES after exception resolved, got %v", err)
	}

	// 处置：方式必须在四选一里。
	if _, err = svc.ResolveInspection(ctx, tenantID, orderID, fail.ID, "SHRUG", "", buyer); err == nil {
		t.Fatal("an unknown disposition must be refused")
	}
	resolved, err := svc.ResolveInspection(ctx, tenantID, orderID, fail.ID, "DEDUCTION", "按 2 吨扣款", buyer)
	if err != nil || resolved.Status != "RESOLVED" || resolved.Disposition != "DEDUCTION" {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	// 已处置的不能再处置。
	if _, err = svc.ResolveInspection(ctx, tenantID, orderID, fail.ID, "RETURN", "", buyer); code(err) != "PO_INSPECTION_NOT_OPEN" {
		t.Fatalf("expected PO_INSPECTION_NOT_OPEN, got %v", err)
	}

	// 三道闸全清：结案放行，且只放一次。
	if err = svc.CloseOrder(ctx, tenantID, orderID, CloseOrderInput{}, buyer); err != nil {
		t.Fatal(err)
	}
	head, err := svc.GetOrder(ctx, tenantID, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if head.ClosedAt == "" || head.ClosedByName != "Buyer" {
		t.Fatalf("closed head=%#v", head)
	}
	if err = svc.CloseOrder(ctx, tenantID, orderID, CloseOrderInput{}, buyer); code(err) != "PO_ALREADY_CLOSED" {
		t.Fatalf("expected PO_ALREADY_CLOSED, got %v", err)
	}

	// 执行视图带出质检列表，最新在前。
	execution, err := svc.GetOrderExecution(ctx, tenantID, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(execution.Inspections) != 2 || execution.Inspections[0].ID != fail.ID {
		t.Fatalf("execution inspections=%#v", execution.Inspections)
	}
}

func code(err error) string {
	if apiErr, ok := err.(*apierr.Error); ok {
		return apiErr.Code
	}
	return ""
}
