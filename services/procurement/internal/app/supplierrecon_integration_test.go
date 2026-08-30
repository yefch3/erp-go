package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 供应商对账（手工核销）。需求的原话是：
//
//	「供应商核销里面列出所有的采购订单，然后员工手动核销，系统不需要做
//	 任何核对步骤，供应商发票只是员工用来上传留凭证用的」
//	「待核销的都放入待核销页面，核销完的转入已完成页面。是否核销完需要
//	 员工手动设置确认，不一定数字对不上就不能核销完成，也不一定数字一样
//	 就可以核销完了」
//
// 逐句翻译成断言，六条：
//
//	1. 一张采购单没人确认过 → 在待核销页，无论钱付没付、付了多少
//	2. 付满了也**不会**自动进已完成——数字一样不等于可以核销完成
//	3. 差得老远也能确认完成——数字对不上不等于不能核销完成
//	4. 确认完成之后照样能继续记付款；钱真的又付了就撤销完成，回到待核销
//	5. 整条路径一次都不碰银行账本（服务不带任何依赖，碰了就 nil panic）
//	6. 发票在这条路上完全不出现——不读、不引用、不校验

func reconTestPool(t *testing.T) (context.Context, *Service, int64, func()) {
	t.Helper()
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed recon test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	// Deps{} 是这条路径的核心断言之一，不是省事：手工核销不该碰银行账本、
	// 不该碰审批、不该碰供应商主数据。真碰了就是 nil panic，比事后靠人眼
	// 审查可靠得多。
	svc := New(pool, Deps{})
	cleanup := func() {
		for _, table := range []string{
			"purchase_order_payment_closures", "payment_allocations",
			"supplier_payments", "supplier_invoice_lines", "supplier_invoices",
			"purchase_order_items", "purchase_orders", "purchase_requirements",
		} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
		pool.Close()
	}
	return ctx, svc, tenantID, cleanup
}

func seedReconOrder(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	tenantID int64, no, total, status string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO purchase_orders
		  (tenant_id, po_no, supplier_id, supplier_code, supplier_name, currency,
		   total_amount, expected_date, status, buyer_id, buyer_name, ordered_at)
		VALUES ($1, $2, 9, 'SUP-9', 'Mill', 'USD', $3::numeric,
		        current_date + 10, $4, 77, 'Buyer', now())
		RETURNING id`, tenantID, no, total, status).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// reconRows 把一页读成 po_no → 行，避免按下标断言：同一天下的两张单排序
// 平手，位置不是确定的。
func reconRows(ctx context.Context, t *testing.T, svc *Service, tenantID int64,
	closedOnly bool, op Operator) map[string]SupplierReconRow {
	t.Helper()
	rows, _, err := svc.ListSupplierRecon(ctx, tenantID,
		SupplierReconFilter{ClosedOnly: closedOnly, Page: 1, PageSize: 200}, op)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]SupplierReconRow, len(rows))
	for _, r := range rows {
		out[r.PONo] = r
	}
	return out
}

func TestSupplierReconIsDrivenByPeopleNotByArithmetic(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}

	paidUp := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-EXACT", "5000", "ORDERED")
	wayOff := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-SHORT", "8000", "ORDERED")
	untouched := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-ZERO", "3000", "RECEIVED")
	// 意向不是承诺：草稿从来没有钱离开过，不该出现在核销页上。
	seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-DRAFT", "99999", "DRAFT")

	// 手填两笔：一张付得分毫不差，一张只付了个订金。
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: paidUp, Amount: "5000", PaidAt: "2026-08-20", Note: "电汇全款",
	}, op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: wayOff, Amount: "1000", PaidAt: "2026-08-20", Note: "订金",
	}, op); err != nil {
		t.Fatal(err)
	}

	pending := reconRows(ctx, t, svc, tenantID, false, op)
	// 1 + 2：三张都还在待核销页，包括那张付得分毫不差的。
	for _, no := range []string{"PO-RC-EXACT", "PO-RC-SHORT", "PO-RC-ZERO"} {
		if _, ok := pending[no]; !ok {
			t.Fatalf("%s 应该在待核销页——没人确认过就是没完成", no)
		}
	}
	if _, ok := pending["PO-RC-DRAFT"]; ok {
		t.Fatal("草稿单不该出现在核销页上：从来没有钱离开过")
	}
	if got := pending["PO-RC-EXACT"].PaidAmount; got != "5000.00" && got != "5000" {
		t.Fatalf("已付应为 5000，实际 %q", got)
	}
	if got := pending["PO-RC-EXACT"].OpenAmount; got != "0.00" && got != "0" {
		t.Fatalf("未付应为 0，实际 %q", got)
	}
	if done := reconRows(ctx, t, svc, tenantID, true, op); len(done) != 0 {
		t.Fatalf("还没人点过确认，已完成页必须是空的，实际 %d 行", len(done))
	}

	// 3：差 7000 也能确认完成——认下这笔亏。
	if _, err := svc.ClosePOPayment(ctx, tenantID, wayOff, "LOSS", "厂里倒闭，剩下的收不回来了", op); err != nil {
		t.Fatal(err)
	}
	done := reconRows(ctx, t, svc, tenantID, true, op)
	row, ok := done["PO-RC-SHORT"]
	if !ok {
		t.Fatal("确认完成之后应该进已完成页")
	}
	if row.ClosedCategory != "LOSS" || row.ClosedByName != "Finance" {
		t.Fatalf("完成记录应带类别和确认人，实际 %+v", row)
	}
	if _, still := reconRows(ctx, t, svc, tenantID, false, op)["PO-RC-SHORT"]; still {
		t.Fatal("确认完成之后不该还留在待核销页")
	}

	// 4：完成之后钱又付了——先记得进去，再撤销完成，回到待核销页。
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: wayOff, Amount: "7000", PaidAt: "2026-09-01", Note: "破产清算追回",
	}, op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReopenPOPayment(ctx, tenantID, wayOff, "钱后来追回来了", op); err != nil {
		t.Fatal(err)
	}
	back := reconRows(ctx, t, svc, tenantID, false, op)
	if _, ok := back["PO-RC-SHORT"]; !ok {
		t.Fatal("撤销完成之后应该回到待核销页")
	}
	if got := back["PO-RC-SHORT"].PaidAmount; got != "8000.00" && got != "8000" {
		t.Fatalf("已付应为 8000（1000 + 7000），实际 %q", got)
	}

	// 6：整条路上一张发票都没有，明细里也不该冒出发票字段。
	entries, err := svc.ListPOPayments(ctx, tenantID, wayOff, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("明细应有两笔，实际 %d", len(entries))
	}
	for _, e := range entries {
		if e.PaymentNo != "" {
			t.Fatalf("手填行不挂付款单，付款单号应为空，实际 %q", e.PaymentNo)
		}
	}
	_ = untouched
}

// 「数字一样就可以核销完了」这句话被明确否掉了，单独钉一条：付满不等于完成，
// 而且付满之后**还能再记**——多付一笔也不该被拦。
func TestPaidUpOrderStaysInTheQueueUntilSomebodyConfirms(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-OVER", "1000", "ORDERED")

	for _, amt := range []string{"1000", "250"} {
		if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
			POID: poID, Amount: amt, PaidAt: "2026-08-20",
		}, op); err != nil {
			t.Fatalf("多付不该被拦（%s）：%v", amt, err)
		}
	}
	row, ok := reconRows(ctx, t, svc, tenantID, false, op)["PO-RC-OVER"]
	if !ok {
		t.Fatal("付超了也还在待核销页——完成与否只看有没有人确认")
	}
	if got := row.OpenAmount; got != "-250.00" && got != "-250" {
		t.Fatalf("未付应为 -250（多付），实际 %q", got)
	}
	// 负的未付数照样能确认完成，且快照就是那个负数。
	if _, err := svc.ClosePOPayment(ctx, tenantID, poID, "ROUNDING", "多付的算尾差不追", op); err != nil {
		t.Fatal(err)
	}
	var snapshot string
	if err := svc.pool.QueryRow(ctx, `
		SELECT open_amount::text FROM purchase_order_payment_closures
		 WHERE tenant_id=$1 AND po_id=$2 AND revoked_at IS NULL`,
		tenantID, poID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot != "-250.00" && snapshot != "-250" {
		t.Fatalf("完成快照应为 -250，实际 %q", snapshot)
	}
}

// 退款不能超过这张采购单的已付净额。这是新路径上**唯一**一道闸——它不是
// 「数字对不上」，是退一笔从来没付出去过的钱，物理上不成立。
func TestRefundCannotExceedWhatWasActuallyPaid(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-REF", "5000", "ORDERED")

	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID, Amount: "2000", PaidAt: "2026-08-20",
	}, op); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID, Amount: "2001", IsRefund: true, PaidAt: "2026-08-21",
	}, op)
	wantAllocErr(t, err, "PR_POPAY_REFUND_EXCEEDS_PAID")

	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID, Amount: "2000", IsRefund: true, PaidAt: "2026-08-21",
	}, op); err != nil {
		t.Fatalf("退到刚好等于已付应当放行：%v", err)
	}
	row := reconRows(ctx, t, svc, tenantID, false, op)["PO-RC-REF"]
	if got := row.PaidAmount; got != "0.00" && got != "0" {
		t.Fatalf("退完之后已付应为 0，实际 %q", got)
	}
}

// 明细里同时住着两种行：手填行和改造前从付款单分配出来的预付行。冲销**必须
// 按行的出身走不同的路**——老行要回填付款单的未分配余额，走错路会留下
// 「付款单以为钱还占着、采购单上钱已经退回」的烂账，而且不报错。
func TestReversingAPaymentBackedEntryGoesThroughTheOldPath(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-MIX", "6000", "ORDERED")

	// 老路：建一张付款单，把 3000 作为预付分配到采购单上。
	adv, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "ADVANCE",
		Currency: "USD", Amount: "3000", PaidAt: "2026-08-10", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, adv.ID,
		[]PaymentAllocationInput{{POID: poID, Amount: "3000"}}, op); err != nil {
		t.Fatal(err)
	}
	// 新路：再手填一笔 1000。
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID, Amount: "1000", PaidAt: "2026-08-20",
	}, op); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.ListPOPayments(ctx, tenantID, poID, op)
	if err != nil {
		t.Fatal(err)
	}
	var oldRow, newRow POPaymentEntry
	for _, e := range entries {
		if e.PaymentNo != "" {
			oldRow = e
		} else {
			newRow = e
		}
	}
	if oldRow.AllocationID == 0 || newRow.AllocationID == 0 {
		t.Fatalf("两种行都该出现在明细里，实际 %+v", entries)
	}

	// 冲销老行：付款单的未分配余额必须弹回 3000。弹不回去 = 没走老路。
	if _, err := svc.ReversePOPayment(ctx, tenantID, oldRow.AllocationID, "记错了", op); err != nil {
		t.Fatal(err)
	}
	after, err := svc.GetSupplierPayment(ctx, tenantID, adv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Unallocated != "3000.00" && after.Unallocated != "3000" {
		t.Fatalf("冲销挂付款单的行之后，付款单未分配余额应弹回 3000，实际 %q——"+
			"没弹回去说明没有转交给 ReverseSupplierPaymentAllocation",
			after.Unallocated)
	}
	// 冲销手填行：不该去碰任何付款单。
	if _, err := svc.ReversePOPayment(ctx, tenantID, newRow.AllocationID, "也记错了", op); err != nil {
		t.Fatal(err)
	}
	row := reconRows(ctx, t, svc, tenantID, false, op)["PO-RC-MIX"]
	if got := row.PaidAmount; got != "0.00" && got != "0" {
		t.Fatalf("两笔都冲掉之后已付应为 0，实际 %q", got)
	}
	// 反过来：手填行送进老接口要说人话，不能是驱动层的裸错误。
	poID2 := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-MIX2", "1000", "ORDERED")
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID2, Amount: "500", PaidAt: "2026-08-20",
	}, op); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListPOPayments(ctx, tenantID, poID2, op)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ReverseSupplierPaymentAllocation(ctx, tenantID, list[0].AllocationID, "试试", op)
	wantAllocErr(t, err, "PAY_ALLOC_NOT_PAYMENT_BACKED")
}

// 就地扩列（而不是新建一张手工核销表）的红利，值得单独钉一条：老路上的
// 退款天花板天然把手填的钱算了进去。哪天有人想「重构」成两张表，这条会翻车。
func TestManualEntriesCountTowardTheOldRefundCeiling(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-CEIL", "20000", "ORDERED")

	// 只手填，付款单一张都没有。
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID, Amount: "10000", PaidAt: "2026-08-20",
	}, op); err != nil {
		t.Fatal(err)
	}
	refund, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "REFUND",
		Currency: "USD", Amount: "12000", PaidAt: "2026-08-25", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	// 退 12000 超过手填的 10000 → 老路的天花板必须拦住。拦不住就说明那道闸
	// 看不见手填的钱，两条路各算各的账。
	_, err = svc.AllocateSupplierPayment(ctx, tenantID, refund.ID,
		[]PaymentAllocationInput{{POID: poID, Amount: "12000"}}, op)
	wantAllocErr(t, err, "PAY_ALLOC_REFUND_EXCEEDS_ADVANCE")

	// 退 10000 正好等于手填的已付，放行。
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, refund.ID,
		[]PaymentAllocationInput{{POID: poID, Amount: "10000"}}, op); err != nil {
		t.Fatalf("退到刚好等于手填的已付应当放行：%v", err)
	}
}

func TestClosingTwiceIsAConflictNotACrash(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-TWICE", "1000", "ORDERED")

	if _, err := svc.ClosePOPayment(ctx, tenantID, poID, "SETTLED", "", op); err != nil {
		t.Fatal(err)
	}
	wantAllocErr(t, svc.closeErr(ctx, tenantID, poID, "SETTLED", op), "PR_POCLOSE_TWICE")

	if _, err := svc.ReopenPOPayment(ctx, tenantID, poID, "点错了", op); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ReopenPOPayment(ctx, tenantID, poID, "再撤一次", op)
	wantAllocErr(t, err, "PR_POCLOSE_NONE")

	// 撤销之后能重新确认——部分唯一索引只管「活着的」那一条。
	if _, err := svc.ClosePOPayment(ctx, tenantID, poID, "SETTLED", "这回是真的", op); err != nil {
		t.Fatalf("撤销之后应当能重新确认：%v", err)
	}

	// 类别白名单：不认的档要被挡住，而不是写进库里变成一个没人看得懂的值。
	poID2 := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-CAT", "1000", "ORDERED")
	wantAllocErr(t, svc.closeErr(ctx, tenantID, poID2, "WHATEVER", op), "PR_POCLOSE_CATEGORY_INVALID")
}

// closeErr 只取错误，让上面的断言读起来是一句话。
func (s *Service) closeErr(ctx context.Context, tenantID, poID int64, category string, op Operator) error {
	_, err := s.ClosePOPayment(ctx, tenantID, poID, category, "", op)
	return err
}

// 数据范围：按人截断的核销清单是**静默错误**——页面打得开、表头和按钮都在、
// 里面一张单都没有，没有一行报错。所以这里和供应商往来汇总同一个立场：
// 拿不到全量范围就明说，不悄悄缩水。
func TestSupplierReconRefusesToShrinkQuietly(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-SCOPE", "1000", "ORDERED")

	limited := New(svc.pool, Deps{Scopes: fixedScope{Visibility{
		All: false, EmployeeIDs: []int64{77}, ScopeType: "SELF",
	}}})
	_, _, err := limited.ListSupplierRecon(ctx, tenantID,
		SupplierReconFilter{Page: 1, PageSize: 20}, op)
	wantAllocErr(t, err, "PR_RECON_SCOPE_LIMITED")

	// 写的那头同样拦住：看不全的人不该在这张单上记账。
	full := reconRows(ctx, t, svc, tenantID, false, op)["PO-RC-SCOPE"]
	_, err = limited.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: full.POID, Amount: "100", PaidAt: "2026-08-20",
	}, op)
	wantAllocErr(t, err, "PR_RECON_SCOPE_LIMITED")
}
