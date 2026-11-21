package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type assignmentTestAccess struct {
	allQuotes map[int64]bool
	selectors map[int64]bool
}

func (a assignmentTestAccess) VisibleEmployees(_ context.Context, id int64, module string) (Visibility, error) {
	return Visibility{All: module != factoryQuoteScopeModule || a.allQuotes[id], EmployeeIDs: []int64{id}}, nil
}

func (a assignmentTestAccess) Allowed(_ context.Context, id int64, code string) (bool, error) {
	if code == "procurement:execution-quote:select" {
		return a.selectors[id], nil
	}
	return id >= 21 && id <= 23 && (code == "procurement:order:write" || (id == 23 && code == "procurement:sourcing:approve")), nil
}
func (assignmentTestAccess) Employee(_ context.Context, id int64) (EmployeeIdentity, error) {
	return EmployeeIdentity{ID: id, Name: fmt.Sprintf("buyer-%d", id)}, nil
}

type assignmentTestApproval struct{ next int64 }

func (a *assignmentTestApproval) Submit(context.Context, ApprovalSubmission) (int64, error) {
	a.next++
	return a.next, nil
}

func TestExecutionAssignmentDispatchAndDraft(t *testing.T) {
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
	var requirement int64
	err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_name,uom_id,uom_code,required_qty,status)
		VALUES($1,1201,'CT-DISPATCH',1,1201,1,'板材',1,'MT',100,'WAITING_REQUOTE') RETURNING id`, tenant).Scan(&requirement)
	if err != nil {
		t.Fatal(err)
	}
	buyer, other, manager := Operator{21, "buyer-21"}, Operator{22, "buyer-22"}, Operator{23, "manager"}
	access := assignmentTestAccess{allQuotes: map[int64]bool{other.ID: true}, selectors: map[int64]bool{}}
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{6: {ID: 6, Code: "S6", Name: "工厂", Currency: "CNY", Status: "ACTIVE"}},
		Scopes: access, Permissions: access, People: access, Approvals: &assignmentTestApproval{next: 7000}})
	save := func(op Operator, price string) ExecutionSupplierQuote {
		t.Helper()
		q, err := svc.SaveExecutionSupplierQuote(ctx, tenant, SaveExecutionSupplierQuoteInput{RequirementID: requirement, SupplierID: 6, Currency: "CNY", UnitPrice: price, PaymentTerms: "T/T"}, op)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	mine, colleague := save(buyer, "3.8"), save(other, "3.9")
	for _, tc := range []struct {
		op   Operator
		want int
	}{{buyer, 1}, {other, 2}, {manager, 0}} {
		if err := svc.AuthorizeRequirement(ctx, tenant, requirement, tc.op); err != nil {
			t.Fatalf("buyer %d lost requirement access: %v", tc.op.ID, err)
		}
		rows, err := svc.ListVisibleExecutionSupplierQuotes(ctx, tenant, requirement, tc.op)
		if err != nil || len(rows) != tc.want || (tc.op == buyer && rows[0].ID != mine.ID) {
			t.Fatalf("buyer %d sees unexpected quotes: %+v (%v)", tc.op.ID, rows, err)
		}
	}
	if svc.AuthorizeExecutionQuote(ctx, tenant, colleague.ID, buyer) == nil {
		t.Fatal("buyer can modify a peer quote")
	}
	if svc.AuthorizeExecutionQuote(ctx, tenant, mine.ID, other) == nil {
		t.Fatal("ALL scope alone allows editing a peer quote")
	}
	if svc.AuthorizeExecutionQuote(ctx, tenant, mine.ID, manager) == nil {
		t.Fatal("approval permission alone bypasses scope")
	}
	access.allQuotes[manager.ID] = true
	if err := svc.AuthorizeExecutionQuote(ctx, tenant, colleague.ID, manager); err != nil {
		t.Fatal(err)
	}
	assign := ExecutionAssignmentInput{Action: "assign", RequirementID: requirement, QuoteID: mine.ID, Instructions: "先确认交期"}
	run := func(op Operator, in ExecutionAssignmentInput) ExecutionAssignmentResult {
		t.Helper()
		r, e := svc.ManageExecutionAssignment(ctx, tenant, op, in)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, buyer, assign); err == nil {
		t.Fatal("buyer can dispatch")
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, other, assign); err == nil {
		t.Fatal("ALL scope alone allows dispatch")
	}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, assign); err == nil {
		t.Fatal("cost approval alone allows dispatch")
	}
	access.selectors[other.ID] = true
	if err := svc.AuthorizeExecutionQuoteSelection(ctx, tenant, mine.ID, other); err != nil {
		t.Fatal(err)
	}
	id := run(other, assign).ID
	if got := run(other, ExecutionAssignmentInput{Action: "list", RequirementID: requirement}).Assignments; len(got) != 1 || got[0].QuoteID != mine.ID {
		t.Fatalf("selection-only user cannot see their selection: %+v", got)
	}
	access.selectors[manager.ID] = true
	if _, err := svc.SaveExecutionSupplierQuote(ctx, tenant, SaveExecutionSupplierQuoteInput{
		ID: mine.ID, RequirementID: requirement, SupplierID: 6, Currency: "USD", UnitPrice: "3.8", PaymentTerms: "T/T",
	}, manager); err == nil {
		t.Fatal("an assigned quote changed currency")
	}
	assign.QuoteID = colleague.ID
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, manager, assign); err == nil {
		t.Fatal("issued a second task for the same remaining quantity")
	}
	tasks := run(buyer, ExecutionAssignmentInput{Action: "inbox"}).Assignments
	if len(tasks) != 1 || tasks[0].ID != id || tasks[0].BuyerID != buyer.ID || tasks[0].AssignedBy != other.ID || tasks[0].Instructions != "先确认交期" || tasks[0].Quantity != "100.000000" {
		t.Fatalf("inbox: %+v", tasks)
	}
	if tasks := run(other, ExecutionAssignmentInput{Action: "inbox"}).Assignments; len(tasks) != 0 {
		t.Fatalf("peer inbox: %+v", tasks)
	}
	negotiate := ExecutionAssignmentInput{ID: id, RequirementID: requirement, Action: "negotiate", Price: "3.5", Date: "2027-01-01", Terms: "T/T", Remark: "议价备注"}
	if _, err := svc.ManageExecutionAssignment(ctx, tenant, other, negotiate); err == nil {
		t.Fatal("peer can negotiate")
	}
	run(manager, negotiate)
	uploaded := run(buyer, ExecutionAssignmentInput{ID: id, RequirementID: requirement, Action: "upload", FileName: "quote.pdf", ContentType: "application/pdf", FileData: []byte("quote")})
	draft := ExecutionAssignmentInput{ID: id, RequirementID: requirement, Action: "draft", FileIDs: []int64{uploaded.ID}}
	po := run(manager, draft).OrderID
	if run(manager, draft).OrderID != po {
		t.Fatal("draft retry created another order")
	}
	head, err := svc.GetOrder(ctx, tenant, po)
	if err != nil || head.BuyerID != buyer.ID || head.Remark != "议价备注" {
		t.Fatalf("owner=%d remark=%q err=%v", head.BuyerID, head.Remark, err)
	}
	var terms string
	if err := pool.QueryRow(ctx, "SELECT payment_terms FROM purchase_orders WHERE tenant_id=$1 AND id=$2", tenant, po).Scan(&terms); err != nil || terms != "T/T" {
		t.Fatalf("payment terms=%q err=%v", terms, err)
	}
	picked := run(buyer, ExecutionAssignmentInput{Action: "list", RequirementID: requirement}).Assignments
	if len(picked) != 1 || picked[0].ChangedBy != manager.ID {
		t.Fatalf("actor: %+v", picked)
	}
	files, err := svc.ListOrderDraftFiles(ctx, tenant, po)
	if err != nil || len(files) != 1 || files[0].FileName != "quote.pdf" {
		t.Fatalf("draft files: %+v %v", files, err)
	}
	state, instance, err := svc.SubmitOrder(ctx, tenant, po, manager)
	if err != nil || state != "PENDING_APPROVAL" {
		t.Fatalf("submit: %s %v", state, err)
	}
	state, err = svc.ApplyApprovalDecision(ctx, tenant, po, instance, "REJECTED", "补交原件", noopClaim)
	if err != nil || state != "REJECTED" {
		t.Fatalf("return: %s %v", state, err)
	}
	returned := run(buyer, ExecutionAssignmentInput{Action: "inbox"}).Assignments
	if len(returned) != 1 || returned[0].OrderID != po || returned[0].OrderStatus != "REJECTED" {
		t.Fatalf("return inbox: %+v", returned)
	}
	if _, err = svc.UploadOrderDraftFile(ctx, tenant, po, "extra.pdf", "application/pdf", []byte("extra"), buyer); err != nil {
		t.Fatal(err)
	}
	next, nextInstance, err := svc.SubmitOrder(ctx, tenant, po, buyer)
	if err != nil || next != "PENDING_APPROVAL" || nextInstance == instance {
		t.Fatalf("resubmit: %s %d %v", next, nextInstance, err)
	}
}
