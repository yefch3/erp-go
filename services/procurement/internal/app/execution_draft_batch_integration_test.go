package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type batchDraftTestAccess struct{ assignmentTestAccess }

func (a batchDraftTestAccess) VisibleEmployees(ctx context.Context, id int64, module string) (Visibility, error) {
	if module == orderScopeModule {
		return Visibility{All: id == 23, EmployeeIDs: []int64{id}}, nil
	}
	return a.assignmentTestAccess.VisibleEmployees(ctx, id, module)
}

func TestExecutionDraftBatchCreatesBuyerOwnedEditableOrders(t *testing.T) {
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
		for _, table := range []string{"purchase_execution_assignment_files", "purchase_execution_assignments", "purchase_order_draft_files", "purchase_order_items", "purchase_orders", "purchase_execution_supplier_quotes", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	}()
	buyer := Operator{ID: 21, Name: "采购员甲"}
	other := Operator{ID: 22, Name: "采购员乙"}
	manager := Operator{ID: 23, Name: "领导"}
	access := batchDraftTestAccess{assignmentTestAccess{allQuotes: map[int64]bool{manager.ID: true}, selectors: map[int64]bool{manager.ID: true}}}
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{6: {ID: 6, Code: "S6", Name: "工厂", Currency: "CNY", Status: "ACTIVE"}},
		Scopes: access, Permissions: access, People: access})
	run := func(actor Operator, in ExecutionAssignmentInput) ExecutionAssignmentResult {
		t.Helper()
		result, err := svc.ManageExecutionAssignment(ctx, tenant, actor, in)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	batch := ExecutionAssignmentInput{Action: "preview_drafts", BatchKey: "CONTRACT-ID:1201"}
	var orderLines []OrderLine
	var lastRequirementID int64
	for index := 1; index <= 5; index++ {
		var requirementID int64
		err := pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,
			product_id,product_name,spec,uom_id,uom_code,required_qty,status)
			VALUES($1,1201,'CT-BATCH-DRAFT',1,$2,1,'板材',$3,1,'MT',20,'WAITING_REQUOTE') RETURNING id`,
			tenant, index, fmt.Sprintf("规格 %d", index)).Scan(&requirementID)
		if err != nil {
			t.Fatal(err)
		}
		lastRequirementID = requirementID
		owner := buyer
		if index > 3 {
			owner = other
		}
		quote, err := svc.SaveExecutionSupplierQuote(ctx, tenant, SaveExecutionSupplierQuoteInput{
			RequirementID: requirementID, SupplierID: 6, Currency: "CNY", UnitPrice: "3.8", PaymentTerms: "T/T",
		}, owner)
		if err != nil {
			t.Fatal(err)
		}
		task := run(manager, ExecutionAssignmentInput{Action: "assign", RequirementID: requirementID, QuoteID: quote.ID})
		line := ExecutionDraftInput{AssignmentID: task.ID, Price: "3.5", Date: "2027-01-02", Terms: "T/T"}
		if index == 1 {
			file := run(buyer, ExecutionAssignmentInput{Action: "upload", ID: task.ID, RequirementID: requirementID, FileName: "quote.pdf", FileData: []byte("quote")})
			line.FileIDs = []int64{file.ID}
		}
		batch.DraftLines = append(batch.DraftLines, line)
		if owner == buyer {
			orderLines = append(orderLines, OrderLine{RequirementID: requirementID, Qty: "20", UnitPrice: "3.6"})
		}
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, buyer, batch); err == nil {
		t.Fatal("buyer previewed another buyer's tasks")
	}
	mine := batch
	mine.DraftLines = mine.DraftLines[:3]
	if got := run(buyer, mine); len(got.DraftGroups) != 1 || len(got.DraftGroups[0].Lines) != 3 {
		t.Fatalf("buyer preview = %+v", got)
	}
	preview := run(manager, batch)
	if len(preview.DraftGroups) != 2 || len(preview.DraftGroups[0].Lines) != 3 || len(preview.DraftGroups[1].Lines) != 2 {
		t.Fatalf("manager preview = %+v", preview)
	}
	batch.Action, batch.DraftPreviewToken = "draft_batch", preview.DraftPreviewToken
	if _, err := pool.Exec(ctx, `UPDATE purchase_requirements SET required_qty=1 WHERE tenant_id=$1 AND id=$2`, tenant, lastRequirementID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, batch); err == nil {
		t.Fatal("batch generated despite one specification's quantity changing")
	}
	var partialOrders int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_orders WHERE tenant_id=$1`, tenant).Scan(&partialOrders); err != nil || partialOrders != 0 {
		t.Fatalf("partial drafts after failure: count=%d err=%v", partialOrders, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE purchase_requirements SET required_qty=20 WHERE tenant_id=$1 AND id=$2`, tenant, lastRequirementID); err != nil {
		t.Fatal(err)
	}
	result := run(manager, batch)
	for index, group := range result.DraftGroups {
		owner := []Operator{buyer, other}[index]
		peer := []Operator{other, buyer}[index]
		head, err := svc.GetOrder(ctx, tenant, group.OrderID)
		if err != nil || head.BuyerID != owner.ID {
			t.Fatalf("draft owner = %d, expected %d, err = %v", head.BuyerID, owner.ID, err)
		}
		if err := svc.AuthorizeOrder(ctx, tenant, group.OrderID, peer); err == nil {
			t.Fatal("peer gained access to another buyer's draft")
		}
		var linked, items int
		err = pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM purchase_execution_assignments WHERE tenant_id=$1 AND po_id=$2 AND changed_by=$3),
			(SELECT count(*) FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2 AND execution_quote_id>0)`,
			tenant, group.OrderID, manager.ID).Scan(&linked, &items)
		if err != nil || linked != len(group.Lines) || items != len(group.Lines) {
			t.Fatalf("draft linked=%d items=%d, want=%d, err=%v", linked, items, len(group.Lines), err)
		}
	}
	firstID := result.DraftGroups[0].OrderID
	files, err := svc.ListOrderDraftFiles(ctx, tenant, firstID)
	if err != nil || len(files) != 1 || files[0].FileName != "quote.pdf" {
		t.Fatalf("attachments = %+v, error = %v", files, err)
	}
	terms := "T/T"
	input := CreateOrderInput{SupplierID: 6, Currency: "CNY", ExpectedDate: "2027-01-02", PaymentTerms: &terms,
		FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "CUSTOM", DeliveryAddress: "按合同交货", Lines: orderLines}
	if _, err := svc.UpdateOrder(ctx, tenant, firstID, input, buyer); err != nil {
		t.Fatalf("could not edit the three-specification draft: %v", err)
	}
	input.Lines = orderLines[:2]
	if _, err := svc.UpdateOrder(ctx, tenant, firstID, input, buyer); err == nil {
		t.Fatal("editing silently dropped an assigned specification")
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, batch); err == nil {
		t.Fatal("reusing the preview token created duplicate drafts")
	}
}
