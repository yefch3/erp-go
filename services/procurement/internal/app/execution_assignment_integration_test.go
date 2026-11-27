package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
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

type assignmentTestApproval struct {
	next, withdrawn int64
	summary         string
}

func (a *assignmentTestApproval) Withdraw(_ context.Context, instanceID int64, _ string) error {
	a.withdrawn = instanceID
	return nil
}

func (a *assignmentTestApproval) Submit(_ context.Context, submission ApprovalSubmission) (int64, error) {
	a.summary = submission.Summary
	a.next++
	return a.next, nil
}

type restrictedDraftSupplierDirectory struct {
	active bool
}

func (d *restrictedDraftSupplierDirectory) Get(context.Context, int64) (Supplier, error) {
	return Supplier{}, apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在或无权访问")
}

func (d *restrictedDraftSupplierDirectory) IsActive(_ context.Context, _, id int64) (bool, error) {
	return id == 6 && d.active, nil
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
	approval := &assignmentTestApproval{next: 7000}
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{6: {ID: 6, Code: "S6", Name: "工厂", Currency: "CNY", Status: "ACTIVE"}},
		Scopes: access, Permissions: access, People: access, Approvals: approval})
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
	if linked, err := svc.OrderFromExecutionNegotiation(ctx, tenant, po); err != nil || !linked {
		t.Fatalf("negotiated draft source: linked=%v err=%v", linked, err)
	}
	if linked, err := svc.OrderFromExecutionNegotiation(ctx, tenant+1, po); err != nil || linked {
		t.Fatalf("unexpected draft source for another tenant: linked=%v err=%v", linked, err)
	}
	head, err := svc.GetOrder(ctx, tenant, po)
	if err != nil || head.BuyerID != buyer.ID || head.Remark != "" {
		t.Fatalf("owner=%d remark=%q err=%v", head.BuyerID, head.Remark, err)
	}
	items, err := svc.OrderItems(ctx, tenant, po)
	if err != nil || len(items) != 1 || items[0].NegotiationRemark != "议价备注" {
		t.Fatalf("specification remarks = %+v, error = %v", items, err)
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
	checkDraftList := func(wantStatus, reason string) {
		t.Helper()
		rows, total, err := svc.ListOrders(ctx, tenant, OrderFilter{Status: "DRAFT", Keyword: head.PoNo}, 1, 100, buyer)
		if wantStatus == "" {
			if err != nil || total != 0 || len(rows) != 0 {
				t.Fatalf("draft list should be empty: total=%d rows=%+v err=%v", total, rows, err)
			}
			return
		}
		if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != po || rows[0].Status != wantStatus || rows[0].RejectReason != reason {
			t.Fatalf("draft list: want status=%s reason=%q, total=%d rows=%+v err=%v", wantStatus, reason, total, rows, err)
		}
	}
	checkDraftList("DRAFT", "")
	state, instance, err := svc.SubmitOrder(ctx, tenant, po, manager)
	if err != nil || state != "PENDING_APPROVAL" {
		t.Fatalf("submit: %s %v", state, err)
	}
	checkDraftList("", "")
	if err := svc.WithdrawOrderApproval(ctx, tenant, po, instance, ""); err != nil || approval.withdrawn != instance {
		t.Fatalf("withdraw original draft: %d, %v", approval.withdrawn, err)
	}
	checkDraftList("DRAFT", "")
	if reopened, err := svc.GetOrder(ctx, tenant, po); err != nil || reopened.ApprovalInstanceID != 0 || reopened.PoNo != head.PoNo {
		t.Fatalf("reopened draft: %+v, %v", reopened, err)
	}
	if files, err := svc.ListOrderDraftFiles(ctx, tenant, po); err != nil || len(files) != 1 || files[0].FileName != "quote.pdf" {
		t.Fatalf("reopened draft lost attachments: %+v, %v", files, err)
	}
	withdrawnInstance := instance
	state, instance, err = svc.SubmitOrder(ctx, tenant, po, manager)
	if err != nil || state != "PENDING_APPROVAL" || instance == withdrawnInstance {
		t.Fatalf("resubmit after withdrawal: %s, %d, %v", state, instance, err)
	}
	if err := svc.WithdrawOrderApproval(ctx, tenant, po, withdrawnInstance, ""); apierr.CodeFromError(err) != "PO_WITHDRAW_CHANGED" {
		t.Fatalf("stale withdrawal: %v", err)
	}
	if state, err := svc.ApplyApprovalDecision(ctx, tenant, po, withdrawnInstance, "APPROVED", "", noopClaim); err != nil || state != "PENDING_APPROVAL" {
		t.Fatalf("stale decision: %s, %v", state, err)
	}
	state, err = svc.ApplyApprovalDecision(ctx, tenant, po, instance, "REJECTED", "补交原件", noopClaim)
	if err != nil || state != "REJECTED" {
		t.Fatalf("return: %s %v", state, err)
	}
	checkDraftList("REJECTED", "补交原件")
	returned := run(buyer, ExecutionAssignmentInput{Action: "inbox"}).Assignments
	if len(returned) != 1 || returned[0].OrderID != po || returned[0].OrderStatus != "REJECTED" {
		t.Fatalf("return inbox: %+v", returned)
	}
	directory := &restrictedDraftSupplierDirectory{active: true}
	svc.suppliers = directory
	blankTerms := ""
	update := CreateOrderInput{
		SupplierID: 6, SupplierCode: "untrusted", SupplierName: "untrusted", KeepCurrentSupplier: true,
		Currency: "CNY", ExpectedDate: "2027-01-02", PaymentTerms: &blankTerms,
		FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "PORT", DeliveryPortName: "自填测试港",
		Lines: []OrderLine{{RequirementID: requirement, Qty: "100", UnitPrice: "3.6"}},
	}
	stale := update
	stale.SupplierID = 7
	if _, err := svc.UpdateOrder(ctx, tenant, po, stale, buyer); apierr.CodeFromError(err) != "PO_SUPPLIER_CHANGED" {
		t.Fatalf("stale supplier: %v", err)
	}
	if _, err := svc.UpdateOrder(ctx, tenant, po, update, buyer); err != nil {
		t.Fatalf("save returned draft: %v", err)
	}
	if _, _, err := svc.SubmitOrder(ctx, tenant, po, buyer); apierr.CodeFromError(err) != "PO_PAYMENT_TERMS_REQUIRED" {
		t.Fatalf("submitted without payment terms: %v", err)
	}
	terms = "30%预付款，70%发货前付清"
	update.PaymentTerms = &terms
	if _, err := svc.UpdateOrder(ctx, tenant, po, update, buyer); err != nil {
		t.Fatalf("save payment terms: %v", err)
	}
	update.PaymentTerms = nil
	if _, err := svc.UpdateOrder(ctx, tenant, po, update, buyer); err != nil {
		t.Fatalf("save without changing payment terms: %v", err)
	}
	if updated, err := svc.GetOrder(ctx, tenant, po); err != nil || updated.DeliveryPortID != 0 ||
		updated.DeliveryPortName != "自填测试港" || updated.DeliveryLocationType != "PORT" {
		t.Fatalf("saved manual port: id=%d name=%q type=%q err=%v", updated.DeliveryPortID, updated.DeliveryPortName, updated.DeliveryLocationType, err)
	} else if updated.SupplierID != 6 || updated.SupplierCode != "S6" || updated.SupplierName != "工厂" {
		t.Fatalf("draft supplier snapshot or terms changed unexpectedly: %+v", updated)
	}
	if state, err := svc.OrderContractState(ctx, tenant, po); err != nil || state.PaymentTerms != terms {
		t.Fatalf("draft payment terms: %q, %v", state.PaymentTerms, err)
	}
	if items, err := svc.OrderItems(ctx, tenant, po); err != nil || len(items) != 1 ||
		items[0].RequirementID != requirement || items[0].UnitPrice != "3.6000" {
		t.Fatalf("saved draft items: %+v err=%v", items, err)
	}
	checkDraftList("DRAFT", "")
	if _, err = svc.UploadOrderDraftFile(ctx, tenant, po, "extra.pdf", "application/pdf", []byte("extra"), buyer); err != nil {
		t.Fatal(err)
	}
	directory.active = false
	if _, _, err := svc.SubmitOrder(ctx, tenant, po, buyer); apierr.CodeFromError(err) != "PO_SUPPLIER_INACTIVE" {
		t.Fatalf("submitted with an inactive supplier: %v", err)
	}
	directory.active = true
	next, nextInstance, err := svc.SubmitOrder(ctx, tenant, po, buyer)
	if err != nil || next != "PENDING_APPROVAL" || nextInstance == instance {
		t.Fatalf("resubmit: %s %d %v", next, nextInstance, err)
	}
	if !strings.Contains(approval.summary, terms) {
		t.Fatalf("approval did not include updated payment terms: %s", approval.summary)
	}
	checkDraftList("", "")
}
