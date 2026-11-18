package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// holdersStub plays IAM's "who holds this permission" answer.
type holdersStub struct {
	people []EmployeeIdentity
	asked  []string
}

func (h *holdersStub) Holders(_ context.Context, code string) ([]EmployeeIdentity, error) {
	h.asked = append(h.asked, code)
	return h.people, nil
}

// 历史补录单的采购员可以指定、可以换（#643）。
//
// 场景就是那张 issue：公司派一个人把历史采购单统一录进来，单子全记在她名下；
// 真正负责的采购「只看得到自己的单」，一张都找不到。这里钉住的是：
//
//   - 换给谁：只能是持有「维护采购订单」权限的在职同事（IAM 给的名单）；
//   - 谁能换：看得见这张单的人——给出去不要求对方在自己的可见范围内，否则
//     「只看得到自己」的录入人谁也给不了；看不见这张单的人连它存不存在都问不出来；
//   - 换了之后：新采购员的列表里有它，原来的人按自己的可见范围决定还看不看得到；
//   - 只换采购员：状态、供应商、金额、明细一样不动；每一次都留痕；
//   - 只对历史补录的单开放。
func TestHistoricalOrderBuyerCanBeChosenAndChanged(t *testing.T) {
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
	t.Cleanup(func() {
		for _, table := range []string{
			"purchase_order_buyer_changes", "purchase_production_reminders", "purchase_production_milestones",
			"historical_purchase_orders", "purchase_order_items", "purchase_orders", "purchase_requirements",
		} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	})

	const recorderID, buyerID, managerID, outsiderID, formerID = 77, 88, 50, 99, 66
	holders := &holdersStub{people: []EmployeeIdentity{
		{ID: recorderID, Name: "录入人", DepartmentName: "天津"},
		{ID: buyerID, Name: "张晓雅", DepartmentName: "天津"},
		{ID: managerID, Name: "采购经理", DepartmentName: "天津"},
	}}
	svc := New(pool, Deps{
		Numbering: &sequenceNumbering{},
		Suppliers: supplierMapDirectoryStub{9: {ID: 9, Code: "SUP9", Name: "Supplier", Status: "ACTIVE"}},
		// 录入人、采购、外人都是「仅本人」；经理看全部。
		Scopes:  &scopeStub{views: map[int64]Visibility{managerID: {All: true, ScopeType: "ALL"}}},
		Holders: holders,
	})
	recorder := Operator{ID: recorderID, Name: "录入人"}
	buyer := Operator{ID: buyerID, Name: "张晓雅"}
	manager := Operator{ID: managerID, Name: "采购经理"}
	outsider := Operator{ID: outsiderID, Name: "外人"}

	in := HistoricalOrderInput{OriginalDate: "2026-10-20", Currency: "CNY", SupplierID: 9, Confirm: true,
		Lines: []HistoricalOrderLine{
			{ProductName: "镀锌铁丝", UOM: "MT", Qty: "10", UnitPrice: "12"},
			{ProductName: "黑铁丝", UOM: "MT", Qty: "5", UnitPrice: "10"},
		}}
	order, err := svc.SaveHistoricalOrder(ctx, tenant, in, recorder)
	if err != nil || order.Status != "ORDERED" {
		t.Fatal("backfill", order, err)
	}
	// 存量数据里有这种状态：草稿阶段别人替录入人接着录过，那一行支撑需求的负责人
	// 是「最后保存的人」而不是单子上的采购员（SaveHistoricalOrder 以前重建明细时
	// 写的是操作人，见 TestHistoricalDraftResaveKeepsRequirementsWithTheBuyer）。
	// 这里造出来：第二行的需求挂在经理名下。
	if tag, err := pool.Exec(ctx, `UPDATE purchase_requirements r SET owner_id=$3,owner_name='采购经理' FROM purchase_order_items i
		WHERE i.tenant_id=$1 AND i.po_id=$2 AND i.product_name='黑铁丝' AND r.tenant_id=i.tenant_id AND r.id=i.requirement_id`, tenant, order.ID, managerID); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("arrange a requirement left under a third person", tag, err)
	}
	var milestoneOpen, milestoneClosed int64
	for node, id := range map[string]*int64{"SCHEDULED": &milestoneOpen, "IN_PRODUCTION": &milestoneClosed} {
		if err := pool.QueryRow(ctx, `INSERT INTO purchase_production_milestones(tenant_id,po_id,node,planned_date,updated_by_id) VALUES($1,$2,$3,'2026-12-01',$4) RETURNING id`,
			tenant, order.ID, node, recorderID).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_production_reminders(tenant_id,po_id,milestone_id,node,planned_date,buyer_id,buyer_name,status) VALUES
		($1,$2,$3,'SCHEDULED','2026-12-01',$5,'录入人','OPEN'),($1,$2,$4,'IN_PRODUCTION','2026-12-01',$5,'录入人','CLOSED')`,
		tenant, order.ID, milestoneOpen, milestoneClosed, recorderID); err != nil {
		t.Fatal(err)
	}

	listedFor := func(op Operator) bool {
		t.Helper()
		rows, _, err := svc.ListOrders(ctx, tenant, OrderFilter{}, 1, 50, op)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ID == order.ID {
				return true
			}
		}
		return false
	}
	buyerOf := func() (int64, string) {
		t.Helper()
		head, err := svc.GetOrder(ctx, tenant, order.ID)
		if err != nil {
			t.Fatal(err)
		}
		return head.BuyerID, head.BuyerName
	}
	changes := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_order_buyer_changes WHERE tenant_id=$1 AND po_id=$2`, tenant, order.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before, err := svc.GetOrder(ctx, tenant, order.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 起点就是 #643：记在录入人名下，采购同事的列表里没有它。
	if id, _ := buyerOf(); id != recorderID || !listedFor(recorder) || listedFor(buyer) {
		t.Fatal("starting point should reproduce #643: filed under the recorder, invisible to the buyer")
	}

	// 候选名单来自 IAM，问的是「维护采购订单」这项权限。
	candidates, err := svc.ListOrderBuyerCandidates(ctx, tenant, recorder)
	if err != nil || len(candidates) != 3 || candidates[1].Name != "张晓雅" || candidates[1].DepartmentName != "天津" {
		t.Fatal("candidates", candidates, err)
	}
	if len(holders.asked) == 0 || holders.asked[0] != "procurement:order:write" {
		t.Fatalf("candidates must be the holders of procurement:order:write, asked %v", holders.asked)
	}

	// 参数不对：不该动任何东西。
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, 0, "", recorder); apierr.CodeFromError(err) != "PO_BUYER_REQUIRED" {
		t.Fatal("missing buyer", err)
	}
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, buyerID, strings.Repeat("长", 201), recorder); apierr.CodeFromError(err) != "PO_BUYER_REASON_TOO_LONG" {
		t.Fatal("long reason", err)
	}
	// 看不见这张单的人：连它存不存在都问不出来。
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, outsiderID, "", outsider); apierr.CodeFromError(err) != "PO_ORDER_NOT_FOUND" {
		t.Fatal("outsider must get not-found", err)
	}
	if _, err := svc.ChangeOrderBuyer(ctx, tenant+1, order.ID, buyerID, "", recorder); apierr.CodeFromError(err) != "PO_ORDER_NOT_FOUND" {
		t.Fatal("cross tenant must get not-found", err)
	}
	// 没有采购权限（或已离职）的人不能当采购员。
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, outsiderID, "", recorder); apierr.CodeFromError(err) != "PO_BUYER_NOT_ELIGIBLE" {
		t.Fatal("ineligible target", err)
	}
	// 换成现在这位：什么都没变，也不留痕。
	same, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, recorderID, "", recorder)
	if err != nil || same.Changed || same.BuyerID != recorderID {
		t.Fatal("no-op", same, err)
	}
	if id, _ := buyerOf(); id != recorderID || changes() != 0 {
		t.Fatal("refused or no-op changes must leave the order and the log untouched")
	}

	// 录入人把单分给真正负责的采购——对方不在她的可见范围里，照样给得出去。
	moved, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, buyerID, "  按实际负责人分配  ", recorder)
	if err != nil || !moved.Changed || moved.BuyerID != buyerID || moved.BuyerName != "张晓雅" {
		t.Fatal("hand over", moved, err)
	}
	if id, name := buyerOf(); id != buyerID || name != "张晓雅" {
		t.Fatal("order not filed under the new buyer", id, name)
	}
	if !listedFor(buyer) || listedFor(recorder) || !listedFor(manager) {
		t.Fatal("after the hand-over: the new buyer sees it, the SELF-scoped recorder no longer does, the manager still does")
	}
	// 只换了采购员：别的一样没动。
	after, err := svc.GetOrder(ctx, tenant, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.SupplierID != before.SupplierID || after.TotalAmount != before.TotalAmount || after.PoNo != before.PoNo {
		t.Fatalf("changing the buyer touched something else: before %+v after %+v", before, after)
	}
	// 留痕：谁、从谁换到谁、为什么。
	var oldID, newID, byID int64
	var oldName, newName, reason, byName string
	if err := pool.QueryRow(ctx, `SELECT old_buyer_id,old_buyer_name,new_buyer_id,new_buyer_name,reason,changed_by_id,changed_by_name FROM purchase_order_buyer_changes WHERE tenant_id=$1 AND po_id=$2`,
		tenant, order.ID).Scan(&oldID, &oldName, &newID, &newName, &reason, &byID, &byName); err != nil {
		t.Fatal("audit row", err)
	}
	if oldID != recorderID || oldName != "录入人" || newID != buyerID || newName != "张晓雅" || reason != "按实际负责人分配" || byID != recorderID || byName != "录入人" {
		t.Fatal("audit row content", oldID, oldName, newID, newName, reason, byID, byName)
	}
	// 历史单每行带一条「不进待采购」的支撑需求，它们的负责人就是这张单的采购员，
	// 全部跟着走——包括原先挂在第三个人名下的那一行。留下任何一行，那个人就还能
	// 从需求那一头（质检来源）看到这张已经不归他的单。
	var owners, strays int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE r.owner_id=$3 AND r.owner_name='张晓雅'), count(*) FILTER (WHERE r.owner_id<>$3)
		FROM purchase_requirements r JOIN purchase_order_items i ON i.tenant_id=r.tenant_id AND i.requirement_id=r.id
		WHERE i.tenant_id=$1 AND i.po_id=$2`, tenant, order.ID, buyerID).Scan(&owners, &strays); err != nil || owners != 2 || strays != 0 {
		t.Fatal("every supporting requirement must follow the buyer", owners, strays, err)
	}
	// 未关闭的生产提醒是发给采购员的，跟着换；已关闭的是历史，不动。
	var openBuyer, closedBuyer int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT buyer_id FROM purchase_production_reminders WHERE tenant_id=$1 AND milestone_id=$2),(SELECT buyer_id FROM purchase_production_reminders WHERE tenant_id=$1 AND milestone_id=$3)`,
		tenant, milestoneOpen, milestoneClosed).Scan(&openBuyer, &closedBuyer); err != nil || openBuyer != buyerID || closedBuyer != recorderID {
		t.Fatal("reminders", openBuyer, closedBuyer, err)
	}

	// 给出去之后，「只看得到自己」的录入人就不能再动它了。
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, recorderID, "", recorder); apierr.CodeFromError(err) != "PO_ORDER_NOT_FOUND" {
		t.Fatal("the recorder gave it away and must not be able to take it back", err)
	}
	// 经理看得见全部，可以再改派；新采购员也可以把它交回去。
	if back, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, recorderID, "", manager); err != nil || !back.Changed {
		t.Fatal("manager reassign", back, err)
	}
	if again, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, buyerID, "", recorder); err != nil || !again.Changed {
		t.Fatal("recorder hands over again", again, err)
	}
	if changes() != 3 {
		t.Fatalf("every real change leaves one row; got %d", changes())
	}

	// 现任采购员后来被收了权限：单子上还是他，但不能再换「给」他。
	holders.people = holders.people[:1] // 只剩录入人
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, formerID, "", manager); apierr.CodeFromError(err) != "PO_BUYER_NOT_ELIGIBLE" {
		t.Fatal("someone outside the holders list", err)
	}
	if kept, err := svc.ChangeOrderBuyer(ctx, tenant, order.ID, buyerID, "", manager); err != nil || kept.Changed {
		t.Fatal("keeping the current buyer is a no-op even after they lost the permission", kept, err)
	}

	// 只对历史补录的单开放：正常流程的单子换采购员牵扯审批和草稿归属，不在这里。
	var plain int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name)
		VALUES($1,$2,9,'Supplier','CNY',0,'ORDERED',$3,'录入人') RETURNING id`, tenant, "PLAIN-1", recorderID).Scan(&plain); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, plain, recorderID, "", manager); apierr.CodeFromError(err) != "PO_BUYER_HISTORICAL_ONLY" {
		t.Fatal("a normal order must be refused", err)
	}
	// 「只对历史单开放」这句话只说给看得见这张单的人听：看不见的人问到的仍然是
	// 「不存在」，不然这个接口就成了探别人名下有没有某张单的办法。
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, plain, buyerID, "", outsider); apierr.CodeFromError(err) != "PO_ORDER_NOT_FOUND" {
		t.Fatal("an outsider must not learn that a normal order exists", err)
	}

	// 名单取不到（IAM 没接上）：说清楚，而不是当成「没有人可选」。
	bare := New(pool, Deps{Scopes: &scopeStub{views: map[int64]Visibility{managerID: {All: true, ScopeType: "ALL"}}}})
	if _, err := bare.ListOrderBuyerCandidates(ctx, tenant, manager); apierr.CodeFromError(err) != "PO_BUYER_DIRECTORY_UNAVAILABLE" {
		t.Fatal("candidates without a directory", err)
	}
	if _, err := bare.ChangeOrderBuyer(ctx, tenant, order.ID, recorderID, "", manager); apierr.CodeFromError(err) != "PO_BUYER_DIRECTORY_UNAVAILABLE" {
		t.Fatal("change without a directory", err)
	}
	if _, err := svc.ListOrderBuyerCandidates(ctx, tenant, Operator{}); apierr.CodeFromError(err) != "PO_OPERATOR_REQUIRED" {
		t.Fatal("anonymous candidates", err)
	}
}

// 草稿交给同事之后，别人接着录这张草稿：重建出来的支撑需求仍然归单子上的采购员。
//
// 存草稿会把明细连同它们的支撑需求整个重建。重建时负责人以前写的是「这次保存
// 的人」——在采购员永远等于录入人的时候，这只在别人代录时才对不上；采购员可以
// 换之后，「录入人把草稿分给同事、自己又回来接着录」就是日常。对不上的后果是：
// 最后保存的那个人还能从需求那一头（质检来源按需求负责人放行）看到这张已经
// 不归他的单，而 ChangeOrderBuyer 刚刚才把这条路堵上。
func TestHistoricalDraftResaveKeepsRequirementsWithTheBuyer(t *testing.T) {
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
	t.Cleanup(func() {
		for _, table := range []string{
			"purchase_order_buyer_changes", "historical_purchase_orders", "purchase_order_items", "purchase_orders", "purchase_requirements",
		} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	})

	const recorderID, buyerID, managerID = 77, 88, 50
	svc := New(pool, Deps{
		Numbering: &sequenceNumbering{},
		Scopes:    &scopeStub{views: map[int64]Visibility{managerID: {All: true, ScopeType: "ALL"}}},
		Holders: &holdersStub{people: []EmployeeIdentity{
			{ID: recorderID, Name: "录入人"}, {ID: buyerID, Name: "张晓雅"}, {ID: managerID, Name: "采购经理"},
		}},
	})
	recorder := Operator{ID: recorderID, Name: "录入人"}
	buyer := Operator{ID: buyerID, Name: "张晓雅"}
	manager := Operator{ID: managerID, Name: "采购经理"}

	owners := func(orderID int64) map[int64]int {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT r.owner_id,count(*) FROM purchase_requirements r JOIN purchase_order_items i ON i.tenant_id=r.tenant_id AND i.requirement_id=r.id
			WHERE i.tenant_id=$1 AND i.po_id=$2 GROUP BY r.owner_id`, tenant, orderID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[int64]int{}
		for rows.Next() {
			var id int64
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				t.Fatal(err)
			}
			out[id] = n
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}

	in := HistoricalOrderInput{OriginalDate: "2026-10-20", Currency: "CNY",
		Lines: []HistoricalOrderLine{{ProductName: "镀锌铁丝", UOM: "MT", Qty: "10"}}}
	draft, err := svc.SaveHistoricalOrder(ctx, tenant, in, recorder)
	if err != nil || draft.Status != "DRAFT" {
		t.Fatal("draft", draft, err)
	}
	// 新建时照旧：单子和支撑需求都归录入人。
	if got := owners(draft.ID); len(got) != 1 || got[recorderID] != 1 {
		t.Fatalf("a new draft's requirements belong to the recorder, got %v", got)
	}
	if _, err := svc.ChangeOrderBuyer(ctx, tenant, draft.ID, buyerID, "", recorder); err != nil {
		t.Fatal("hand the draft over", err)
	}

	// 经理（看得见全部）接着补录：加一行再存。
	in.ID = draft.ID
	in.Lines = append(in.Lines, HistoricalOrderLine{ProductName: "黑铁丝", UOM: "MT", Qty: "5"})
	if _, err := svc.SaveHistoricalOrder(ctx, tenant, in, manager); err != nil {
		t.Fatal("manager continues the draft", err)
	}
	if got := owners(draft.ID); len(got) != 1 || got[buyerID] != 2 {
		t.Fatalf("rebuilt requirements must stay with the order's buyer (%d), got owners %v", buyerID, got)
	}
	head, err := svc.GetOrder(ctx, tenant, draft.ID)
	if err != nil || head.BuyerID != buyerID || head.BuyerName != "张晓雅" {
		t.Fatal("saving the draft must not move the order itself", head.BuyerID, head.BuyerName, err)
	}

	// 采购员自己确认补录：还是她的。
	in.Confirm = true
	if confirmed, err := svc.SaveHistoricalOrder(ctx, tenant, in, buyer); err != nil || confirmed.Status != "ORDERED" {
		t.Fatal("buyer confirms", confirmed, err)
	}
	if got := owners(draft.ID); len(got) != 1 || got[buyerID] != 2 {
		t.Fatalf("after confirming, got owners %v", got)
	}
}
