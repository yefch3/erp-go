package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type executionRFQDeleteTestAccess struct{ batchDraftTestAccess }

func (a executionRFQDeleteTestAccess) VisibleEmployees(ctx context.Context, id int64, module string) (Visibility, error) {
	if module == requirementScopeModule {
		return Visibility{All: id == 23, EmployeeIDs: []int64{id}}, nil
	}
	return a.batchDraftTestAccess.VisibleEmployees(ctx, id, module)
}

func (a executionRFQDeleteTestAccess) Allowed(ctx context.Context, id int64, code string) (bool, error) {
	if code == "procurement:execution-rfq:delete" {
		return id == 22 || id == 23, nil
	}
	return a.batchDraftTestAccess.Allowed(ctx, id, code)
}

func TestDeleteExecutionRFQBatch(t *testing.T) {
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
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"purchase_execution_assignments", "purchase_order_items", "purchase_orders", "purchase_execution_supplier_quotes", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	}()
	buyer := Operator{ID: 21, Name: "采购员"}
	limited := Operator{ID: 22, Name: "其他经理"}
	manager := Operator{ID: 23, Name: "采购经理"}
	access := executionRFQDeleteTestAccess{batchDraftTestAccess{assignmentTestAccess{allQuotes: map[int64]bool{23: true}, selectors: map[int64]bool{23: true}}}}
	svc := New(pool, Deps{Scopes: access, Permissions: access, People: access,
		Suppliers: supplierMapDirectoryStub{6: {ID: 6, Code: "S6", Name: "工厂", Currency: "CNY", Status: "ACTIVE"}}})
	requirement := func(item, owner int64) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_name,uom_code,required_qty,status,owner_id)
			VALUES ($1,1201,'CT-DELETE-RFQ',1,$2,$2,'板材','MT',20,'WAITING_REQUOTE',$3) RETURNING id`, tenant, item, owner).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second := requirement(1, buyer.ID), requirement(2, limited.ID)
	quoteInput := SaveExecutionSupplierQuoteInput{RequirementID: first, SupplierID: 6, Currency: "CNY", UnitPrice: "50", PaymentTerms: "T/T"}
	quote, err := svc.SaveExecutionSupplierQuote(ctx, tenant, quoteInput, buyer)
	if err != nil {
		t.Fatal(err)
	}
	checkDelete := func(actor Operator, tenantID int64, want string) {
		t.Helper()
		_, err := svc.ManageExecutionAssignment(ctx, tenantID, actor, ExecutionAssignmentInput{Action: "delete_batch", BatchKey: "CONTRACT-ID:1201"})
		if (want == "" && err != nil) || (want != "" && (err == nil || !strings.Contains(err.Error(), want))) {
			t.Fatalf("delete result=%v, expected error code %q", err, want)
		}
	}
	checkDelete(buyer, tenant, "EXECUTION_RFQ_DELETE_PERMISSION")
	checkDelete(limited, tenant, "EXECUTION_RFQ_DELETE_BATCH_NOT_FOUND")
	checkDelete(manager, tenant+1, "EXECUTION_RFQ_DELETE_BATCH_NOT_FOUND")
	task, err := svc.ManageExecutionAssignment(ctx, tenant, manager, ExecutionAssignmentInput{Action: "assign", RequirementID: first, QuoteID: quote.ID})
	if err != nil {
		t.Fatal(err)
	}
	checkDelete(manager, tenant, "EXECUTION_RFQ_DELETE_TASK_EXISTS")
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, ExecutionAssignmentInput{Action: "cancel", RequirementID: first, ID: task.ID}); err != nil {
		t.Fatal(err)
	}
	var orderID int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_orders (tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name)
		VALUES ($1,'PO-DELETE-RFQ',6,'工厂','CNY',50,'DRAFT',$2,$3) RETURNING id`, tenant, buyer.ID, buyer.Name).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_order_items (tenant_id,po_id,requirement_id,product_id,product_name,uom_code,qty,unit_price,amount)
		VALUES ($1,$2,$3,2,'板材','MT',1,50,50)`, tenant, orderID, second); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"DRAFT", "PENDING_APPROVAL", "ORDERED"} {
		if _, err := pool.Exec(ctx, `UPDATE purchase_orders SET status=$3 WHERE tenant_id=$1 AND id=$2`, tenant, orderID, status); err != nil {
			t.Fatal(err)
		}
		checkDelete(manager, tenant, "EXECUTION_RFQ_DELETE_ORDER_EXISTS")
	}
	if _, err := pool.Exec(ctx, `UPDATE purchase_orders SET status='CANCELLED' WHERE tenant_id=$1 AND id=$2`, tenant, orderID); err != nil {
		t.Fatal(err)
	}
	checkDelete(manager, tenant, "")
	lateRequirement := requirement(3, buyer.ID)
	for _, batchKey := range []string{"", "CONTRACT-ID:1201"} {
		rows, total, _, err := svc.ListExecutionBatchRequirements(ctx, tenant, manager, "", batchKey, "", 1, 20)
		if err != nil || len(rows) != 0 || total != 0 {
			t.Fatalf("deleted batch key %q still visible: total=%d rows=%d err=%v", batchKey, total, len(rows), err)
		}
	}
	var recorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_requirements WHERE tenant_id=$1 AND execution_rfq_deleted_by=$2
		AND execution_rfq_deleted_at IS NOT NULL AND status='WAITING_REQUOTE' AND required_qty=20`, tenant, manager.ID).Scan(&recorded); err != nil || recorded != 2 {
		t.Fatalf("deletion did not preserve requirements and record the manager: %d %v", recorded, err)
	}
	if quotes, err := svc.ListExecutionSupplierQuotes(ctx, tenant, first); err != nil || len(quotes) != 1 || quotes[0].ID != quote.ID {
		t.Fatalf("existing quotation lost: %+v %v", quotes, err)
	}
	if _, err := svc.SaveExecutionSupplierQuote(ctx, tenant, quoteInput, buyer); err == nil {
		t.Fatal("a stale page could save a quotation after deletion")
	}
	quoteInput.RequirementID = lateRequirement
	if _, err := svc.SaveExecutionSupplierQuote(ctx, tenant, quoteInput, buyer); err == nil {
		t.Fatal("a line received late reopened the deleted contract version")
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, ExecutionAssignmentInput{Action: "assign", RequirementID: first, QuoteID: quote.ID}); err == nil {
		t.Fatal("a stale page could assign negotiation after deletion")
	}
}
