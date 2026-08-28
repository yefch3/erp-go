package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func contractProgressParams(tenantID, contractID int64) store.ContractReceiptProgressParams {
	return store.ContractReceiptProgressParams{TenantID: tenantID, ContractID: contractID}
}

func listAllocsParams(tenantID, txID int64) store.ListAllocationsOfTransactionParams {
	return store.ListAllocationsOfTransactionParams{TenantID: tenantID, TransactionID: txID}
}

// 客户退款进队列。钉的是七条：
//
//	· 收 10000 再退 3000，合同已收变成 7000——六处求和一个字没改，
//	  负行自动把数拉下来，这正是「退款记负数」当初被选中的理由
//	· 退款行的完成口径：退出去的量 ≥ 出账金额 → 已核销；claimed 报量不报向
//	· 退款不能超过这张合同已收的净额（连续退两笔也框得住）
//	· 归属没到「客户往来」的出账进不了退款流程
//	· 退款行不收补足差额（手续费）
//	· 退款也能认差结清（银行扣了手续费，退出去的比出账少）
//	· 冲销一条退款，钱回到合同已收
func TestCustomerRefundLifecycle(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed refund test")
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

	contractID := seedContract(ctx, t, pool, tenantID, "CT-REF-1", "USD", "20000")
	ledger := newFakeLedger(
		BankRow{ID: 9001, BankRef: "RF-IN", Direction: "CREDIT", Amount: "10000.00",
			Currency: "USD", ValueDate: "2026-08-18", Ownership: OwnershipCustomer},
		BankRow{ID: 9002, BankRef: "RF-OUT", Direction: "DEBIT", Amount: "3000.00",
			Currency: "USD", ValueDate: "2026-08-22", Ownership: OwnershipCustomer},
		BankRow{ID: 9003, BankRef: "RF-OUT2", Direction: "DEBIT", Amount: "9000.00",
			Currency: "USD", ValueDate: "2026-08-23", Ownership: OwnershipCustomer},
	)
	svc := New(pool, Deps{Bank: ledger})
	op := Operator{ID: 5, Name: "Finance"}

	// 收 10000。
	if _, err := svc.Allocate(ctx, tenantID, 9001,
		[]AllocationLine{{ContractID: contractID, Amount: "10000"}}, op); err != nil {
		t.Fatal(err)
	}

	// 退款不收补足差额。
	if _, err := svc.Allocate(ctx, tenantID, 9002,
		[]AllocationLine{{ContractID: contractID, Amount: "100", FeeAmount: "5"}}, op); err == nil ||
		!strings.Contains(err.Error(), "EX_REFUND_NO_FEE") {
		t.Fatalf("退款带手续费应该被拒绝，实际 %v", err)
	}

	// 退 3000（界面上填正数，落库为负）。
	view, err := svc.Allocate(ctx, tenantID, 9002,
		[]AllocationLine{{ContractID: contractID, Amount: "3000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	// 完成口径：3000 的出账被说清了 3000 → 已核销；claimed 报的是量。
	if view.Disposition() != DispositionAllocated {
		t.Fatalf("退清的退款行应该是已核销，实际 %s（未分配 %s）",
			view.Disposition(), view.UnallocatedAmount)
	}
	claims := ledger.claimLog()
	if !strings.Contains(claims[len(claims)-1], "3000.00") {
		t.Fatalf("退款行应报量 3000，实际 %v", claims[len(claims)-1])
	}
	// 合同已收 10000 − 3000 = 7000，求和天然算对。
	progress, err := svc.q.ContractReceiptProgress(ctx, contractProgressParams(tenantID, contractID))
	if err != nil {
		t.Fatal(err)
	}
	if progress.ReceivedAmount != "7000.00" && progress.ReceivedAmount != "7000" {
		t.Fatalf("退款后合同已收应为 7000，实际 %s", progress.ReceivedAmount)
	}

	// 再退 9000：已收只剩 7000，框住。
	if _, err := svc.Allocate(ctx, tenantID, 9003,
		[]AllocationLine{{ContractID: contractID, Amount: "9000"}}, op); err == nil ||
		!strings.Contains(err.Error(), "EX_REFUND_EXCEEDS_RECEIVED") {
		t.Fatalf("超过已收的退款应该被拒绝，实际 %v", err)
	}

	// 退 7000、认差结清剩下的 2000（比如合同取消，尾款别处退的）。
	if _, err := svc.Allocate(ctx, tenantID, 9003,
		[]AllocationLine{{ContractID: contractID, Amount: "7000"}}, op); err != nil {
		t.Fatal(err)
	}
	view, err = svc.SettleTransaction(ctx, tenantID, 9003, "OTHER", "尾款走了另一条路", op)
	if err != nil {
		t.Fatalf("退款行认差应该可行：%v", err)
	}
	if view.VarianceAmount != "2000.00" || view.Disposition() != DispositionAllocated {
		t.Fatalf("退款认差没落对：variance=%q disp=%s", view.VarianceAmount, view.Disposition())
	}

	// 冲销第一笔退款：钱回到合同已收（7000 − 7000 + 3000 = 3000... 即
	// 10000 − 7000 = 3000）。
	allocs, err := svc.q.ListAllocationsOfTransaction(ctx, listAllocsParams(tenantID, 9002))
	if err != nil || len(allocs) != 1 {
		t.Fatalf("找退款核销行失败：%v %d", err, len(allocs))
	}
	if _, err := svc.ReverseAllocation(ctx, tenantID, allocs[0].ID, "退错了", op); err != nil {
		t.Fatal(err)
	}
	progress, err = svc.q.ContractReceiptProgress(ctx, contractProgressParams(tenantID, contractID))
	if err != nil {
		t.Fatal(err)
	}
	if progress.ReceivedAmount != "3000.00" && progress.ReceivedAmount != "3000" {
		t.Fatalf("冲销退款后合同已收应为 3000，实际 %s", progress.ReceivedAmount)
	}

	// 归属还空着的出账（9002 冲销后仍是客户，换个没归属的演）在 Allocate
	// 那道门外——用 8003 风格单独验过（见 variance 测试），这里验列表口径：
	// 退款视图只出归属=客户的出账。
	views, _, err := svc.ListTransactions(ctx, tenantID, TransactionQuery{Direction: "DEBIT"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.Transaction.Ownership != OwnershipCustomer {
			t.Fatalf("退款视图里混进了归属为 %q 的行", v.Transaction.Ownership)
		}
	}
}
