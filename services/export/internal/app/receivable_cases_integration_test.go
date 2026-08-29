package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 待核销 / 已完成两页。
//
// 需求原话：「客户方面只选择合同，进行核销时数字都是员工手动填写，不需要
// 连接到流水数据……是否核销完需要员工手动设置确认，不一定数字对不上就不能
// 核销完成，也不一定是数字一样就可以核销完了。」
//
// 这条把那两句话逐字翻译成断言：
//
//	· 记一笔钱不挂任何银行流水，合同已收照样加对
//	· **收满了也不会自动跑到已完成页**——没人点确认就一直待着
//	· **差额还在也能确认完成**——点了就走
//	· 确认完成之后照样能继续记钱（钱又来了），撤销完成能回到待核销页
//	· 退款记负数，唯一保留的天花板是「退不出没收到过的钱」
//	· 冲销写反向记录、不删原记录，且不能冲两次
func TestReceivableCasesAreDrivenByPeopleNotByArithmetic(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed receivable cases test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM contract_receivable_closures WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "CT-CASE-1", "USD", "10000")
	// 关键：服务**不带** Bank 依赖。整条路径一次都不该碰银行账本——
	// 碰了就会在这里 nil panic，比事后靠人眼审查可靠。
	svc := New(pool, Deps{})
	op := Operator{ID: 5, Name: "Finance"}

	inPending := func(t *testing.T) bool {
		t.Helper()
		rows, _, err := svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, op)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ContractID == contractID {
				return true
			}
		}
		return false
	}
	inDone := func(t *testing.T) bool {
		t.Helper()
		rows, _, err := svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{ClosedOnly: true}, 1, 50, op)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ContractID == contractID {
				return true
			}
		}
		return false
	}

	// 一分钱没收也在待核销页上——队列由「有没有人确认过」决定，不是由
	// 「欠不欠钱」决定。
	if !inPending(t) || inDone(t) {
		t.Fatal("新合同应该在待核销页、不在已完成页")
	}

	// 记 4000。不挂流水。
	progress, err := svc.RecordContractReceipt(ctx, tenantID, ContractReceiptInput{
		ContractID: contractID, Amount: "4000", ReceivedAt: "2026-08-20", Note: "首款",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if progress.ReceivedAmount != "4000.00" && progress.ReceivedAmount != "4000" {
		t.Fatalf("已收应为 4000，实际 %s", progress.ReceivedAmount)
	}
	// 落库的核销行**不挂任何流水**。
	var txnNull bool
	if err := pool.QueryRow(ctx, `
		SELECT bool_and(transaction_id IS NULL) FROM receipt_allocations
		 WHERE tenant_id=$1 AND contract_id=$2`, tenantID, contractID).Scan(&txnNull); err != nil {
		t.Fatal(err)
	}
	if !txnNull {
		t.Fatal("手工记的收款不该挂银行流水")
	}

	// 收满 10000——**这里是整条测试的重点**：数字对上了，页面上也不该动。
	if _, err := svc.RecordContractReceipt(ctx, tenantID, ContractReceiptInput{
		ContractID: contractID, Amount: "6000", ReceivedAt: "2026-08-25",
	}, op); err != nil {
		t.Fatal(err)
	}
	if !inPending(t) || inDone(t) {
		t.Fatal("收满了但没人点确认——必须仍然留在待核销页（「不一定数字一样就可以核销完了」）")
	}

	// 人点了确认，才走。
	if err := svc.CloseReceivable(ctx, tenantID, contractID, "SETTLED", "收齐了", op); err != nil {
		t.Fatalf("「正常收完」这一档必须能用：%v", err)
	}
	if inPending(t) || !inDone(t) {
		t.Fatal("确认之后应该只出现在已完成页")
	}

	// 撤销完成，回到待核销页。
	if err := svc.ReopenReceivable(ctx, tenantID, contractID, "客户又退回来一笔", op); err != nil {
		t.Fatal(err)
	}
	if !inPending(t) || inDone(t) {
		t.Fatal("撤销完成之后应该回到待核销页")
	}

	// 退款：界面填正数，落库为负；已收跟着变小。
	if _, err := svc.RecordContractReceipt(ctx, tenantID, ContractReceiptInput{
		ContractID: contractID, Amount: "20000", IsRefund: true,
	}, op); err == nil || !strings.Contains(err.Error(), "EX_REFUND_EXCEEDS_RECEIVED") {
		t.Fatalf("退超过已收应被拒绝（退不出没收到过的钱），实际 %v", err)
	}
	progress, err = svc.RecordContractReceipt(ctx, tenantID, ContractReceiptInput{
		ContractID: contractID, Amount: "3000", IsRefund: true, ReceivedAt: "2026-08-28",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if progress.ReceivedAmount != "7000.00" && progress.ReceivedAmount != "7000" {
		t.Fatalf("退款后已收应为 7000，实际 %s", progress.ReceivedAmount)
	}

	// 差额还有 3000，照样能确认完成——「不一定数字对不上就不能核销完成」。
	if err := svc.CloseReceivable(ctx, tenantID, contractID, "LOSS", "尾款不追了", op); err != nil {
		t.Fatalf("有差额也必须能确认完成：%v", err)
	}
	if !inDone(t) {
		t.Fatal("有差额确认完成之后应该在已完成页")
	}

	// 确认完成之后照样能继续记钱——「只关催收的口，不关钱的门」。
	if _, err := svc.RecordContractReceipt(ctx, tenantID, ContractReceiptInput{
		ContractID: contractID, Amount: "500",
	}, op); err != nil {
		t.Fatalf("已完成的合同应该还能记收款：%v", err)
	}

	// 冲销：写反向记录，不删原记录；不能冲两次。
	_, entries, err := svc.ContractReceipts(ctx, tenantID, contractID)
	if err != nil {
		t.Fatalf("合同收款明细不该因为没有银行流水而失败：%v", err)
	}
	var target int64
	for _, e := range entries {
		if e.ReversalOf == 0 && e.Amount == "500.00" {
			target = e.ID
		}
	}
	if target == 0 {
		t.Fatalf("没找到刚记的那笔：%+v", entries)
	}
	if _, err := svc.ReverseContractReceipt(ctx, tenantID, target, "", op); err == nil ||
		!strings.Contains(err.Error(), "EX_REVERSE_REASON_REQUIRED") {
		t.Fatalf("冲销必须填理由，实际 %v", err)
	}
	after, err := svc.ReverseContractReceipt(ctx, tenantID, target, "记错合同了", op)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReceivedAmount != "7000.00" && after.ReceivedAmount != "7000" {
		t.Fatalf("冲销后已收应回到 7000，实际 %s", after.ReceivedAmount)
	}
	if _, err := svc.ReverseContractReceipt(ctx, tenantID, target, "再冲一次", op); err == nil ||
		!strings.Contains(err.Error(), "EX_ALLOC_ALREADY_REVERSED") {
		t.Fatalf("同一笔不能冲两次，实际 %v", err)
	}
}

// 催收和「待核销页」从此是两个口径，这条钉住那个分歧。
//
// 收齐了但没人点确认的合同：**留在待核销页**（等财务确认），但**不该给销售
// 发催款提醒**——钱已经到账了，催客户要钱是错的。这两句话过去是同一个条件
// （未收 > 0），改造后故意分开，很容易被下一个人"改回一致"，所以钉住。
func TestPaidUpContractStaysInTheQueueButStopsChasing(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed reminder divergence test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receivable_reminders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "CT-CHASE-1", "USD", "10000")
	// 逾期 + 有负责人，否则催收扫描本来就不会看它。
	if _, err := pool.Exec(ctx, `
		UPDATE contracts SET receivable_due_date = current_date - 10, sales_employee_id = 42
		 WHERE tenant_id=$1 AND id=$2`, tenantID, contractID); err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{})
	op := Operator{ID: 5, Name: "Finance"}

	// 还欠着的时候：该发提醒。
	if _, err := svc.SweepReceivableReminders(ctx); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM receivable_reminders WHERE tenant_id=$1`, tenantID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before == 0 {
		t.Fatal("逾期且欠款的合同应该发出提醒，否则这条测试证明不了下面那半")
	}

	// 收齐——但**没有人点确认完成**。
	if _, err := svc.RecordContractReceipt(ctx, tenantID, ContractReceiptInput{
		ContractID: contractID, Amount: "10000",
	}, op); err != nil {
		t.Fatal(err)
	}

	// 待核销页：还在（等人确认）。
	rows, _, err := svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, op)
	if err != nil {
		t.Fatal(err)
	}
	var still bool
	for _, r := range rows {
		if r.ContractID == contractID {
			still = true
		}
	}
	if !still {
		t.Fatal("收齐但没人确认的合同必须留在待核销页")
	}

	// 催收：不再发新的。钱到账了，催客户要钱是错的。
	_, _ = pool.Exec(ctx, `DELETE FROM receivable_reminders WHERE tenant_id=$1`, tenantID)
	if _, err := svc.SweepReceivableReminders(ctx); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM receivable_reminders WHERE tenant_id=$1`, tenantID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("钱已收齐就不该再催客户，实际又发了 %d 条", after)
	}
}

// 待核销页上的冲销按钮对**老行**也点得到，而老行的冲销要多做三件事。
//
// 合同明细里同时住着两种核销记录：新的手工记账（不挂流水）和 F2 之前从
// 银行流水核出来的老行。界面上它们长得一样、按钮也一样，但老行冲销时必须
// 把重算后的认领量报回采购的账本——不报的话那一行的 claimed_amount 停在
// 旧值，永远回不到收款对账的「待处理」队列：钱在出口这边已经空出来，在账本
// 上却还认领着，全程一行报错都没有。
//
// 所以新入口遇到挂流水的行要原样转给老路径（它带着建议锁、结清守门和账本
// 回写），而不是自己再实现一遍。这条钉住那次转交。
func TestReversingAnOldBankLinkedEntryStillReportsBackToTheLedger(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed delegation test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_line_settlements WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM receipt_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "CT-OLD-1", "USD", "10000")
	const txID = int64(7701)
	ledger := newFakeLedger(BankRow{
		ID: txID, BankRef: "OLD-IN", Direction: "CREDIT", Amount: "10000.00",
		Currency: "USD", ValueDate: "2026-08-10", Ownership: OwnershipCustomer,
	})
	svc := New(pool, Deps{Bank: ledger})
	op := Operator{ID: 5, Name: "Finance"}

	// 老路径核销 6000，账本上认领量记 6000。
	if _, err := svc.Allocate(ctx, tenantID, txID,
		[]AllocationLine{{ContractID: contractID, Amount: "6000"}}, op); err != nil {
		t.Fatal(err)
	}
	_, entries, err := svc.ContractReceipts(ctx, tenantID, contractID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].TransactionID != txID {
		t.Fatalf("应该有一条挂着流水 %d 的老行：%+v", txID, entries)
	}
	oldEntryID := entries[0].ID

	// 这一行「认差结清」之后不许冲销——老路径的守门。新入口必须一起继承，
	// 否则「差额是认下那一刻的余额」这句话会和事实同时成立又互相矛盾。
	if _, err := svc.SettleTransaction(ctx, tenantID, txID, "LOSS", "尾款不追", op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReverseContractReceipt(ctx, tenantID, oldEntryID, "想撤", op); err == nil ||
		!strings.Contains(err.Error(), "EX_TX_SETTLED") {
		t.Fatalf("结清过的老行不该能从待核销页冲掉，实际 %v", err)
	}

	// 撤销结清之后再冲：这次要成功，而且**必须把认领量报回账本**。
	if _, err := svc.RevokeSettlement(ctx, tenantID, txID, "算错了", op); err != nil {
		t.Fatal(err)
	}
	before := len(ledger.claimLog())
	after, err := svc.ReverseContractReceipt(ctx, tenantID, oldEntryID, "记错合同了", op)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReceivedAmount != "0.00" && after.ReceivedAmount != "0" {
		t.Fatalf("冲销后合同已收应为 0，实际 %s", after.ReceivedAmount)
	}
	claims := ledger.claimLog()
	if len(claims) <= before {
		t.Fatal("冲销挂流水的老行必须把重算后的认领量报回账本" +
			"——不报的话那一行永远回不到收款对账的待处理队列，而且没有任何报错")
	}
	if !strings.Contains(claims[len(claims)-1], "0") {
		t.Fatalf("报回去的认领量应该是 0，实际 %q", claims[len(claims)-1])
	}
}
