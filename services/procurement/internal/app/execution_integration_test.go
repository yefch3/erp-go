package app

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestPurchaseOrderSendAndExecutionLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed execution test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	var requirementID, orderID, itemID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status) VALUES ($1,1,'CT-EXEC',1,$1,11,'P-11','Execution Coil',7,'TON',10,10,'ORDERED') RETURNING id`, tenantID).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,expected_date,status,buyer_id,buyer_name,ordered_at) VALUES ($1,'PO-EXEC-1',9,'SUP-9','Mill','USD',5200,current_date+10,'ORDERED',77,'Buyer',now()) RETURNING id`, tenantID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items (tenant_id,po_id,requirement_id,product_id,product_code,product_name,uom_id,uom_code,qty,unit_price,amount) VALUES ($1,$2,$3,11,'P-11','Execution Coil',7,'TON',10,520,5200) RETURNING id`, tenantID, orderID, requirementID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_receipt_exceptions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_production_reminders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_production_milestones WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_supplier_confirmation_lines WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_supplier_confirmations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_send_attempts WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{})
	documents, err := svc.GetOrderDocuments(ctx, tenantID, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if documents.Version != PurchaseOrderDocumentVersion || len(documents.XLSXData) < 500 || !bytes.HasPrefix(documents.PDFData, []byte("%PDF-")) {
		t.Fatalf("invalid documents: %#v", documents)
	}

	failedAttempt, err := svc.BeginOrderSend(ctx, tenantID, orderID, "mill@example.com", 77, "Buyer")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := svc.CompleteOrderSend(ctx, tenantID, orderID, failedAttempt, false, 0, "", "smtp unavailable", []string{documents.XLSXFileName, documents.PDFFileName}, documents.Version); err != nil || status != "FAILED" {
		t.Fatalf("failed send status=%s err=%v", status, err)
	}
	successAttempt, err := svc.BeginOrderSend(ctx, tenantID, orderID, "mill@example.com", 77, "Buyer")
	if err != nil {
		t.Fatal(err)
	}
	if status, err := svc.CompleteOrderSend(ctx, tenantID, orderID, successAttempt, true, 12, "MAIL-12", "", []string{documents.XLSXFileName, documents.PDFFileName}, documents.Version); err != nil || status != "SENT" {
		t.Fatalf("successful send status=%s err=%v", status, err)
	}
	if _, err = svc.BeginOrderSend(ctx, tenantID, orderID, "mill@example.com", 77, "Buyer"); err == nil {
		t.Fatal("expected duplicate send to be rejected")
	}

	confirmation, err := svc.RecordSupplierConfirmation(ctx, tenantID, orderID, time.Now().UTC().Format("2006-01-02"), time.Now().UTC().AddDate(0, 0, 11).Format("2006-01-02"), "supplier confirms partial quantity and a later date", []SupplierConfirmationLine{{POItemID: itemID, ConfirmedQty: "8", ConfirmedUnitPrice: "525"}}, Operator{ID: 77, Name: "Buyer"})
	if err != nil {
		t.Fatal(err)
	}
	if confirmation.Status != "MATCHED" || confirmation.ApprovalInstanceID != 0 || len(confirmation.Lines) != 1 || confirmation.Lines[0].ConfirmedQty != "8.0000" || confirmation.Lines[0].ConfirmedUnitPrice != "525.0000" {
		t.Fatalf("supplier confirmation should be recorded without a second approval: %#v", confirmation)
	}
	// B5 尾巴：最新回签状态要跟着订单一起出来，列表和详情才有子标签可挂。
	if head, err := svc.GetOrder(ctx, tenantID, orderID); err != nil || head.ConfirmStatus != "APPROVED" {
		t.Fatalf("confirm status on order head=%q err=%v", head.ConfirmStatus, err)
	}

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	milestone, err := svc.SaveProductionMilestone(ctx, tenantID, orderID, ProductionMilestone{Node: "IN_PRODUCTION", PlannedDate: yesterday, OwnerID: 77, OwnerName: "Buyer", Remark: "late", Attachments: []ProductionAttachment{{FileName: "photo.jpg", FileURL: "https://files.example/photo.jpg", ContentType: "image/jpeg"}}}, Operator{ID: 77, Name: "Buyer"})
	if err != nil {
		t.Fatal(err)
	}
	if !milestone.Delayed || len(milestone.Attachments) != 1 {
		t.Fatalf("milestone=%#v", milestone)
	}

	exception, err := svc.ReportReceiptException(ctx, tenantID, orderID, ReceiptException{POItemID: itemID, Type: "DAMAGE", Qty: "1", Description: "edge damaged"}, Operator{ID: 88, Name: "Logistics"})
	if err != nil {
		t.Fatal(err)
	}
	if exception.Status != "OPEN" {
		t.Fatalf("exception=%#v", exception)
	}
	exception, err = svc.ResolveReceiptException(ctx, tenantID, orderID, exception.ID, "supplier credit note", Operator{ID: 77, Name: "Buyer"})
	if err != nil || exception.Status != "RESOLVED" {
		t.Fatalf("resolved exception=%#v err=%v", exception, err)
	}

	execution, err := svc.GetOrderExecution(ctx, tenantID, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(execution.Confirmations) != 1 || execution.Confirmations[0].Status != "MATCHED" || len(execution.Milestones) != 1 || len(execution.Reminders) != 1 || len(execution.Exceptions) != 1 {
		t.Fatalf("execution=%#v", execution)
	}
}
