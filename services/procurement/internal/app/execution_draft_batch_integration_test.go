package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"

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
		for _, table := range []string{"purchase_execution_assignment_files", "purchase_execution_assignments", "purchase_order_draft_files", "purchase_order_submission_activity", "purchase_order_items", "purchase_orders", "purchase_execution_supplier_quotes", "purchase_requirements", "outbox_events"} {
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
	remarksByRequirement := make(map[int64]string)
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
		if index == 1 {
			if _, err := pool.Exec(ctx, `UPDATE purchase_requirements SET source='CUSTOMER_QUOTATION',status='PENDING' WHERE tenant_id=$1 AND id=$2`, tenant, requirementID); err != nil {
				t.Fatal(err)
			}
		}
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
		switch index {
		case 1:
			line.Remark = "按首件样品验收\n随货附材质单"
		case 2:
			line.Remark = "每捆单独贴标"
		}
		remarksByRequirement[requirementID] = line.Remark
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
	var reservedDraft int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_orders
		(tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name)
		VALUES ($1,'PO-EXISTING-DRAFT',6,'工厂','CNY',3.5,'DRAFT',$2,$3) RETURNING id`, tenant, other.ID, other.Name).Scan(&reservedDraft); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_order_items
		(tenant_id,po_id,requirement_id,product_id,product_name,uom_code,qty,unit_price,amount)
		VALUES ($1,$2,$3,1,'板材','MT',1,3.5,3.5)`, tenant, reservedDraft, lastRequirementID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, batch); err == nil {
		t.Fatal("batch generated despite a competing draft reserving quantity")
	}
	if err := svc.CancelOrder(ctx, tenant, reservedDraft, "", other); err != nil {
		t.Fatal(err)
	}
	result := run(manager, batch)
	for index, group := range result.DraftGroups {
		owner := []Operator{buyer, other}[index]
		peer := []Operator{other, buyer}[index]
		head, err := svc.GetOrder(ctx, tenant, group.OrderID)
		if err != nil || head.BuyerID != owner.ID || head.Remark != "" {
			t.Fatalf("draft owner = %d, expected %d, order remark = %q, err = %v", head.BuyerID, owner.ID, head.Remark, err)
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
	input := CreateOrderInput{SupplierID: 6, Currency: "CNY", ExpectedDate: "2027-01-02", PaymentTerms: &terms, Remark: "订单统一交货要求",
		FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "CUSTOM", DeliveryAddress: "按合同交货", Lines: orderLines}
	if _, err := svc.UpdateOrder(ctx, tenant, firstID, input, buyer); err != nil {
		t.Fatalf("could not edit the three-specification draft: %v", err)
	}
	head, err := svc.GetOrder(ctx, tenant, firstID)
	if err != nil || head.Remark != input.Remark {
		t.Fatalf("edited order remark = %q, error = %v", head.Remark, err)
	}
	items, err := svc.OrderItems(ctx, tenant, firstID)
	if err != nil || len(items) != len(orderLines) {
		t.Fatalf("edited draft has %d items, expected %d, error = %v", len(items), len(orderLines), err)
	}
	for _, item := range items {
		if want := remarksByRequirement[item.RequirementID]; item.NegotiationRemark != want {
			t.Errorf("requirement %d remark = %q, expected %q", item.RequirementID, item.NegotiationRemark, want)
		}
	}
	input.Lines = orderLines[:2]
	if _, err := svc.UpdateOrder(ctx, tenant, firstID, input, buyer); err == nil {
		t.Fatal("editing silently dropped an assigned specification")
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, batch); err == nil {
		t.Fatal("reusing the preview token created duplicate drafts")
	}
	if err := svc.CancelOrder(ctx, tenant, firstID, "", buyer); err != nil {
		t.Fatal(err)
	}
	head, err = svc.GetOrder(ctx, tenant, firstID)
	if err != nil || head.Status != "CANCELLED" || head.CancelReason != "" {
		t.Fatalf("withdrawn draft status = %q, reason = %q, err = %v", head.Status, head.CancelReason, err)
	}
	for _, line := range result.DraftGroups[0].Lines {
		list := ExecutionAssignmentInput{Action: "list", RequirementID: line.RequirementID}
		if got := run(buyer, list); len(got.Assignments) != 0 {
			t.Fatalf("cancelled draft still has visible tasks for requirement %d: %+v", line.RequirementID, got.Assignments)
		}
		selected := run(manager, ExecutionAssignmentInput{Action: "assign", RequirementID: line.RequirementID, QuoteID: line.QuoteID})
		got := run(buyer, list)
		if len(got.Assignments) != 1 || got.Assignments[0].ID != selected.ID || selected.ID == line.AssignmentID || got.Assignments[0].Remark != "" || got.Assignments[0].Quantity != line.Quantity {
			t.Fatalf("reselecting did not create a fresh task for requirement %d: %+v", line.RequirementID, got.Assignments)
		}
	}
	items, err = svc.OrderItems(ctx, tenant, firstID)
	if err != nil || len(items) != len(orderLines) {
		t.Fatalf("cancelled draft items = %+v, error = %v", items, err)
	}
	for _, item := range items {
		if want := remarksByRequirement[item.RequirementID]; item.NegotiationRemark != want {
			t.Errorf("cancelled draft requirement %d remark = %q, expected %q", item.RequirementID, item.NegotiationRemark, want)
		}
	}
	otherLine := result.DraftGroups[1].Lines[0]
	if got := run(other, ExecutionAssignmentInput{Action: "list", RequirementID: otherLine.RequirementID}); len(got.Assignments) != 1 || got.Assignments[0].OrderID != result.DraftGroups[1].OrderID {
		t.Fatalf("cancelling one draft changed another draft's tasks: %+v", got.Assignments)
	}

	otherOrderID := result.DraftGroups[1].OrderID
	input.Lines = []OrderLine{
		{RequirementID: otherLine.RequirementID, Qty: "12", UnitPrice: "3.5"},
		{RequirementID: result.DraftGroups[1].Lines[1].RequirementID, Qty: "20", UnitPrice: "3.5"},
	}
	if _, err := svc.UpdateOrder(ctx, tenant, otherOrderID, input, other); err != nil {
		t.Fatal(err)
	}
	svc.approvals = &d7ApprovalStub{}
	_, instance, err := svc.SubmitOrder(ctx, tenant, otherOrderID, other)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, otherOrderID, instance, "APPROVED", "", noopClaim); err != nil || state != "ORDERED" {
		t.Fatalf("approval status=%q err=%v", state, err)
	}
	checkExecutionLine := func(requirementID int64, wantStatus string, available decimal.Decimal) {
		t.Helper()
		rows, _, _, err := svc.ListExecutionBatchRequirements(ctx, tenant, manager, "", batch.BatchKey, "", 1, 20)
		if err != nil || len(rows) != len(batch.DraftLines) {
			t.Fatalf("expected all five contract lines, got %d, err=%v", len(rows), err)
		}
		for _, row := range rows {
			if row.ID == requirementID && row.Status == wantStatus && decimal.RequireFromString(row.RequiredQty).Equal(decimal.NewFromInt(20)) && decimal.RequireFromString(row.AvailableQty).Equal(available) {
				return
			}
		}
		t.Fatalf("requirement %d missing or incorrect, want %s with %s available", requirementID, wantStatus, available)
	}
	checkExecutionLine(otherLine.RequirementID, "PARTIALLY_ORDERED", decimal.NewFromInt(8))
	checkExecutionLine(input.Lines[1].RequirementID, "ORDERED", decimal.Zero)
	newTask := run(manager, ExecutionAssignmentInput{Action: "assign", RequirementID: otherLine.RequirementID, QuoteID: otherLine.QuoteID})
	if got := run(other, ExecutionAssignmentInput{Action: "list", RequirementID: otherLine.RequirementID}); len(got.Assignments) != 2 || got.Assignments[1].ID != newTask.ID || !decimal.RequireFromString(got.Assignments[1].Quantity).Equal(decimal.NewFromInt(8)) {
		t.Fatalf("partial remainder did not produce an 8-unit assignment: %+v", got.Assignments)
	}
	run(manager, ExecutionAssignmentInput{Action: "cancel", ID: newTask.ID, RequirementID: otherLine.RequirementID})
	if err := svc.CancelOrder(ctx, tenant, otherOrderID, "工厂无法交货", other); err != nil {
		t.Fatal(err)
	}
	for _, line := range result.DraftGroups[1].Lines {
		checkExecutionLine(line.RequirementID, "PENDING", decimal.NewFromInt(20))
		if got := run(other, ExecutionAssignmentInput{Action: "list", RequirementID: line.RequirementID}); len(got.Assignments) != 0 {
			t.Fatalf("cancelled approved order still has an active task: %+v", got.Assignments)
		}
		run(manager, ExecutionAssignmentInput{Action: "assign", RequirementID: line.RequirementID, QuoteID: line.QuoteID})
	}
}
