package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
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
	// 取消的单要分两种，因为草稿也能取消。真下出去过的（ordered_at 非空）
	// 必须收进来——「订金付了、单取消了、那笔订金还没录」正是这时候要有人
	// 了结它，而如果按「已付非零」筛，那笔钱就永远找不到入口录。
	// 取消掉的草稿（ordered_at 为空）不收，否则队列里全是没意义的行。
	// seedReconOrder 写的 ordered_at = now()，所以这张就是「真下出去过、
	// 后来取消了」；下面那张把 ordered_at 清空，模拟取消掉的草稿。
	seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-CXL", "4000", "CANCELLED")
	seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-CXL-DRAFT", "7000", "CANCELLED")
	if _, err := svc.pool.Exec(ctx, `
		UPDATE purchase_orders SET ordered_at = NULL
		 WHERE tenant_id=$1 AND po_no='PO-RC-CXL-DRAFT'`, tenantID); err != nil {
		t.Fatal(err)
	}

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
	if _, ok := pending["PO-RC-CXL"]; !ok {
		t.Fatal("真下出去过、后来取消的单必须在待核销页上——" +
			"订金付了单取消了，那笔钱要有地方录、要有人了结")
	}
	if _, ok := pending["PO-RC-CXL-DRAFT"]; ok {
		t.Fatal("取消掉的草稿不该出现：从来没有钱离开过")
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
//
// 供应商付款页下线之后这条尤其要紧：对账页是**唯一**的入口了，它要是不接
// 这活，所有历史 payment-backed 核销行就变成谁也动不了的死行。
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

	// 冲销老行：付款单的未分配余额必须弹回 3000。弹不回去 = 没走老路，
	// 那笔钱会在采购单上已经退回、在付款单上却还占着。
	if _, err := svc.ReversePOPayment(ctx, tenantID, poID, oldRow.AllocationID, "记错了", op); err != nil {
		t.Fatal(err)
	}
	after, err := svc.GetSupplierPayment(ctx, tenantID, adv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Unallocated != "3000.00" && after.Unallocated != "3000" {
		t.Fatalf("冲销挂付款单的行之后，付款单未分配余额应弹回 3000，实际 %q——"+
			"没弹回去说明没有转交给 ReverseSupplierPaymentAllocation", after.Unallocated)
	}
	// 冲销手填行：不该去碰任何付款单。
	if _, err := svc.ReversePOPayment(ctx, tenantID, poID, newRow.AllocationID, "也记错了", op); err != nil {
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

// **走发票那条路的钱也得算进已付。**
//
// payment_allocations 上有 CHECK((invoice_id IS NOT NULL) <> (po_id IS NOT NULL))：
// 挂发票的核销行 po_id 必然为空。只按 po_id 求和的话，一张通过「录发票 →
// 付款单核销到发票」付清的采购单，在对账页上恒显示「一分未付」。
//
// 而这条路并没有被这次改造取消——供应商发票页和供应商付款页都还在，改造前
// 的存量数据更是全走这条路。后果不只是数字难看：员工照着那个 0 再手填一遍，
// 同一笔钱在账上出现两次；「确认完成」还会把那个错的差额永久快照进记录。
func TestInvoicePathMoneyCountsAsPaid(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-INV", "5000", "ORDERED")

	// 老路走全程：录一张挂着这张采购单的发票，建付款单，全额核销到发票。
	var invID int64
	if err := svc.pool.QueryRow(ctx, `
		INSERT INTO supplier_invoices
		  (tenant_id, supplier_id, supplier_name, invoice_no, currency, total_amount, invoice_date)
		VALUES ($1, 9, 'Mill', 'INV-RC-1', 'USD', 5000, '2026-08-01')
		RETURNING id`, tenantID).Scan(&invID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx, `
		INSERT INTO supplier_invoice_lines (tenant_id, invoice_id, po_id, description, amount)
		VALUES ($1, $2, $3, '全额', 5000)`, tenantID, invID, poID); err != nil {
		t.Fatal(err)
	}
	settle, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "5000", PaidAt: "2026-08-20", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, settle.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "5000"}}, op); err != nil {
		t.Fatal(err)
	}

	row, ok := reconRows(ctx, t, svc, tenantID, false, op)["PO-RC-INV"]
	if !ok {
		t.Fatal("这张单该在待核销页上——没人确认过完成")
	}
	if got := row.PaidAmount; got != "5000.00" && got != "5000" {
		t.Fatalf("已付应为 5000（钱走的是发票那条路，但它确实付了），实际 %q——"+
			"显示 0 的话员工会照着再手填一遍，同一笔钱记两次", got)
	}
	if got := row.OpenAmount; got != "0.00" && got != "0" {
		t.Fatalf("未付应为 0，实际 %q", got)
	}
	// 单列出来，让这个数解释得清：员工在明细里只看得见手填行，剩下的差额
	// 不说明来处就会被当成漏记。
	if got := row.InvoicePaidAmount; got != "5000.00" && got != "5000" {
		t.Fatalf("其中走发票的应为 5000，实际 %q", got)
	}

	// 跨采购单的发票按行的归属分摊，不整笔算给其中一张。
	poA := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-SPLIT-A", "3000", "ORDERED")
	poB := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-SPLIT-B", "1000", "ORDERED")
	var splitInv int64
	if err := svc.pool.QueryRow(ctx, `
		INSERT INTO supplier_invoices
		  (tenant_id, supplier_id, supplier_name, invoice_no, currency, total_amount, invoice_date)
		VALUES ($1, 9, 'Mill', 'INV-RC-2', 'USD', 4000, '2026-08-02')
		RETURNING id`, tenantID).Scan(&splitInv); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx, `
		INSERT INTO supplier_invoice_lines (tenant_id, invoice_id, po_id, description, amount)
		VALUES ($1,$2,$3,'A',3000), ($1,$2,$4,'B',1000)`,
		tenantID, splitInv, poA, poB); err != nil {
		t.Fatal(err)
	}
	part, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "2000", PaidAt: "2026-08-21", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, part.ID,
		[]PaymentAllocationInput{{InvoiceID: splitInv, Amount: "2000"}}, op); err != nil {
		t.Fatal(err)
	}
	after := reconRows(ctx, t, svc, tenantID, false, op)
	if got := after["PO-RC-SPLIT-A"].PaidAmount; got != "1500.00" && got != "1500" {
		t.Fatalf("A 单应分到 2000 × 3000/4000 = 1500，实际 %q", got)
	}
	if got := after["PO-RC-SPLIT-B"].PaidAmount; got != "500.00" && got != "500" {
		t.Fatalf("B 单应分到 2000 × 1000/4000 = 500，实际 %q", got)
	}
}

// 金额是员工手打的，两位小数之外的输入不能静默改数，也不能变成一个 500。
func TestAmountInputIsCheckedBeforeItReachesTheColumn(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-PREC", "1000", "ORDERED")

	for _, bad := range []struct{ amount, code string }{
		// 0.004 舍成 0.00 会撞 CHECK (amount <> 0)，0.006 会被静默存成 0.01。
		{"0.004", "PR_POPAY_AMOUNT_PRECISION"},
		{"0.006", "PR_POPAY_AMOUNT_PRECISION"},
		{"1e20", "PR_POPAY_AMOUNT_TOO_LARGE"},
		{"0", "PR_POPAY_AMOUNT_INVALID"},
		{"-5", "PR_POPAY_AMOUNT_INVALID"},
		{"abc", "PR_POPAY_AMOUNT_INVALID"},
	} {
		_, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
			POID: poID, Amount: bad.amount, PaidAt: "2026-08-20",
		}, op)
		wantAllocErr(t, err, bad.code)
	}
	// 正好两位小数照常收。
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID, Amount: "12.34", PaidAt: "2026-08-20",
	}, op); err != nil {
		t.Fatalf("两位小数应当放行：%v", err)
	}
}

// 手填在**取消掉的**采购单上的钱，不能从供应商往来汇总里整行消失。
//
// 那张汇总的 keys 原来只从「已下单/部分收货/已收货」的采购单、非作废发票、
// 付款单三处取键。手填行三处都不产生键——于是这家供应商在汇总里根本不出现，
// 不是算少，是整行没有。
func TestHandEnteredMoneyOnACancelledOrderStillShowsInTheLedger(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-GHOST", "4000", "CANCELLED")
	if _, err := svc.RecordPOPayment(ctx, tenantID, POPaymentInput{
		POID: poID, Amount: "3000", PaidAt: "2026-08-20", Note: "订金，单后来取消了",
	}, op); err != nil {
		t.Fatal(err)
	}

	all, err := svc.ListSupplierStatements(ctx, tenantID, "", op)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, v := range all {
		if v.SupplierID == 9 && v.Currency == "USD" {
			found = true
			if v.AdvanceAmount != "3000.00" && v.AdvanceAmount != "3000" {
				t.Fatalf("预付应为 3000，实际 %q", v.AdvanceAmount)
			}
		}
	}
	if !found {
		t.Fatal("这家供应商必须出现在往来汇总里——3000 块真的付出去了，" +
			"不能因为那张单被取消、又没有付款单抬头就整行消失")
	}
}

// 挂在发票上的核销行，也得能从这一页冲掉。
//
// 供应商付款页下线之后，这一页是唯一的入口。而这类行有两个性质叠在一起：
//
//	· 它的钱**算进本页的已付**（reconFrom 的 ip 那条腿按发票行分摊回来）
//	· payment_allocations 上 CHECK((invoice_id IS NOT NULL) <> (po_id IS NOT
//	  NULL)) 保证它的 po_id 是空的
//
// 所以「明细按 po_id 过滤」会让它一条都不出现——钱算进去了，却既解释不清，
// 也没有任何冲销入口。一笔记错的发票核销会被永久焊死在已付里，还会被
// 「确认完成」快照进 closure 记录。
func TestInvoiceBackedEntriesAreVisibleAndReversibleHere(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-RC-INVREV", "5000", "ORDERED")

	var invID int64
	if err := svc.pool.QueryRow(ctx, `
		INSERT INTO supplier_invoices
		  (tenant_id, supplier_id, supplier_name, invoice_no, currency, total_amount, invoice_date)
		VALUES ($1, 9, 'Mill', 'INV-REV-1', 'USD', 5000, '2026-08-01')
		RETURNING id`, tenantID).Scan(&invID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx, `
		INSERT INTO supplier_invoice_lines (tenant_id, invoice_id, po_id, description, amount)
		VALUES ($1, $2, $3, '全额', 5000)`, tenantID, invID, poID); err != nil {
		t.Fatal(err)
	}
	pay, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "5000", PaidAt: "2026-08-20", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, pay.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "5000"}}, op); err != nil {
		t.Fatal(err)
	}

	// 明细里必须看得见它，而且标着是哪张发票——不然那 5000 从哪来说不清。
	entries, err := svc.ListPOPayments(ctx, tenantID, poID, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("挂发票的核销行也要出现在明细里（它算进了本单的已付），实际 %d 条", len(entries))
	}
	if entries[0].InvoiceNo != "INV-REV-1" {
		t.Fatalf("要标出它核销在哪张发票上，实际 %q", entries[0].InvoiceNo)
	}

	// 而且必须冲得掉。冲完之后本单的已付回到 0。
	row, err := svc.ReversePOPayment(ctx, tenantID, poID, entries[0].AllocationID, "发票选错了", op)
	if err != nil {
		t.Fatalf("挂发票的行必须能从这一页冲掉——付款页已经下线，这是唯一入口：%v", err)
	}
	if row.PaidAmount != "0.00" && row.PaidAmount != "0" {
		t.Fatalf("冲销之后本单已付应回到 0，实际 %q", row.PaidAmount)
	}
	// 走的是老路，所以付款单的未分配余额也要弹回去。
	after, err := svc.GetSupplierPayment(ctx, tenantID, pay.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Unallocated != "5000.00" && after.Unallocated != "5000" {
		t.Fatalf("付款单未分配余额应弹回 5000，实际 %q", after.Unallocated)
	}
}

// 应付到期日是**建单的人填的**，不是从供应商账期推出来的。这一组钉的是
// 列表怎么读它：
//
//	· 没填 → 到期日留空 → 「未填」，**不是「今天到期」**。编一个日子会让
//	  「今天该付谁」这句话变成假的
//	· 两个筛子各管一件事：只看逾期的、只看没填的（后者是催人填，不是催钱）
func TestPayableDueIsReadFromTheOrderItself(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}

	// seedReconOrder 直接写库，到期日在这里手工摆布——摆的是「员工填下的
	// 结果」，测的是列表怎么读它。
	overdue := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-DUE-LATE", "1000", "ORDERED")
	soon := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-DUE-SOON", "1000", "ORDERED")
	// 这一张什么都不设：建单的人没填，到期日留空——「未填」。
	seedReconOrder(ctx, t, svc.pool, tenantID, "PO-DUE-NONE", "1000", "ORDERED")
	if _, err := svc.pool.Exec(ctx, `
		UPDATE purchase_orders SET payable_due_date = current_date - 5
		 WHERE tenant_id=$1 AND id=$2`, tenantID, overdue); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx, `
		UPDATE purchase_orders SET payable_due_date = current_date + 10
		 WHERE tenant_id=$1 AND id=$2`, tenantID, soon); err != nil {
		t.Fatal(err)
	}

	all := reconRows(ctx, t, svc, tenantID, false, op)
	if got := all["PO-DUE-LATE"]; got.OverdueDays != 5 || got.DueUnset {
		t.Fatalf("逾期 5 天的单应为 overdueDays=5、dueUnset=false，实际 %+v", got)
	}
	if got := all["PO-DUE-SOON"]; got.OverdueDays != -10 || got.DueUnset {
		t.Fatalf("还有 10 天到期的单应为 overdueDays=-10，实际 %+v", got)
	}
	// **没填不是「今天到期」。** 到期日留空、dueUnset 为真，
	// overdueDays 那个 0 是 coalesce 出来的占位，界面靠 dueUnset 分流。
	if got := all["PO-DUE-NONE"]; !got.DueUnset || got.DueDate != "" {
		t.Fatalf("没填到期日的单应为 dueUnset=true、到期日空串，实际 %+v", got)
	}

	// 两个筛子。
	only := func(f SupplierReconFilter) map[string]SupplierReconRow {
		t.Helper()
		f.Page, f.PageSize = 1, 200
		rows, _, err := svc.ListSupplierRecon(ctx, tenantID, f, op)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]SupplierReconRow{}
		for _, r := range rows {
			out[r.PONo] = r
		}
		return out
	}
	od := only(SupplierReconFilter{OverdueOnly: true})
	if _, ok := od["PO-DUE-LATE"]; !ok {
		t.Fatal("「只看逾期」要收进逾期的那张")
	}
	for _, no := range []string{"PO-DUE-SOON", "PO-DUE-NONE"} {
		if _, ok := od[no]; ok {
			t.Fatalf("「只看逾期」不该收 %s——没到期和没填都不是逾期", no)
		}
	}
	un := only(SupplierReconFilter{UnsetOnly: true})
	if _, ok := un["PO-DUE-NONE"]; !ok {
		t.Fatal("「只看未填到期日」要收进没填的那张")
	}
	if _, ok := un["PO-DUE-LATE"]; ok {
		t.Fatal("「只看未填到期日」不该收已经填了的单——那是催钱，不是催人填")
	}
}

// 审批通过**不许动**员工填下的应付到期日。
//
// 上一版 SetPurchaseOrderOrdered 里有一句无条件赋值：
//
//	payable_due_date = CASE WHEN payment_days > 0
//	    THEN CURRENT_DATE + payment_days ELSE NULL END
//
// 账期那一列停用之后它恒为 0，于是这句会走 ELSE 分支，把填好的日子抹成空。
// 抹的动作发生在 Kafka 审批消费里：**没有人在场、不报错**，页面第二天只是
// 显示「未填」，而钱该什么时候付这件事已经没人知道了。
//
// 所以这条测试钉的不是「下单会算出什么」，而是「下单什么都不该算」。
func TestApprovingAnOrderDoesNotWipeTheDueDateSomebodyTyped(t *testing.T) {
	ctx, svc, tenantID, cleanup := reconTestPool(t)
	defer cleanup()

	poID := seedReconOrder(ctx, t, svc.pool, tenantID, "PO-DUE-KEEP", "1000", "DRAFT")
	var typed string
	if err := svc.pool.QueryRow(ctx, `
		UPDATE purchase_orders SET payable_due_date = current_date + 45, ordered_at = NULL
		 WHERE tenant_id=$1 AND id=$2
		RETURNING payable_due_date::text`, tenantID, poID).Scan(&typed); err != nil {
		t.Fatal(err)
	}

	// 审批通过走的就是这一句。
	if err := svc.q.SetPurchaseOrderOrdered(ctx, store.SetPurchaseOrderOrderedParams{
		TenantID: tenantID, ID: poID,
	}); err != nil {
		t.Fatal(err)
	}

	var after, status string
	if err := svc.pool.QueryRow(ctx, `
		SELECT coalesce(payable_due_date::text, ''), status
		  FROM purchase_orders WHERE id=$1`, poID).Scan(&after, &status); err != nil {
		t.Fatal(err)
	}
	if status != "ORDERED" {
		t.Fatalf("下单应该把状态推到 ORDERED，实际 %q", status)
	}
	if after != typed {
		t.Fatalf("审批通过把员工填的到期日改掉了：填的是 %q，下单之后变成 %q", typed, after)
	}
}
