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

// 认差结清。钉的是六条：
//
//	· 核不满的行认了差就算完成（Disposition 翻到已核销），并且全额报回账本
//	· 认下的差额 = 认那一刻的未核余额，视图里未分配清零
//	· 核满的行没差可认；同一行不许认两次
//	· 认过差的行：再核销、冲销都被挡住——先撤销结清
//	· 撤销要理由；撤销后行回到待处理，报回账本的是真实已核数
//	· 差额类别只认那四种；补足差额（fee）的类别同样只认那四种
func TestSettleVarianceLifecycle(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed variance test")
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

	contractID := seedContract(ctx, t, pool, tenantID, "CT-VAR-1", "USD", "10000")
	ledger := newFakeLedger(
		// 到账 5000，合同只欠 4800 的场景由核销上限管；这里演的是
		// 「核了 4800，剩 200 是客户多付/损耗，认掉」。
		BankRow{ID: 8001, BankRef: "VAR-A", Direction: "CREDIT", Amount: "5000.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer},
		BankRow{ID: 8002, BankRef: "VAR-FULL", Direction: "CREDIT", Amount: "1000.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer},
		BankRow{ID: 8003, BankRef: "VAR-OUT", Direction: "DEBIT", Amount: "300.00",
			Currency: "USD", ValueDate: "2026-08-20", Ownership: OwnershipCustomer},
	)
	svc := New(pool, Deps{Bank: ledger})
	op := Operator{ID: 5, Name: "Finance"}

	// 核 4800，剩 200。
	if _, err := svc.Allocate(ctx, tenantID, 8001,
		[]AllocationLine{{ContractID: contractID, Amount: "4800"}}, op); err != nil {
		t.Fatal(err)
	}

	// 差额类别不认识的词要拒绝。
	if _, err := svc.SettleTransaction(ctx, tenantID, 8001, "WHATEVER", "", op); err == nil ||
		!strings.Contains(err.Error(), "EX_SETTLE_CATEGORY_INVALID") {
		t.Fatalf("胡写的类别应该被拒绝，实际 %v", err)
	}

	// 认差：200 按「多收」结清。
	view, err := svc.SettleTransaction(ctx, tenantID, 8001, "OVERPAY", "客户多付了", op)
	if err != nil {
		t.Fatal(err)
	}
	if view.VarianceAmount != "200.00" || view.VarianceCategory != "OVERPAY" {
		t.Fatalf("差额记的不对：%q %q，应为 200.00 OVERPAY", view.VarianceAmount, view.VarianceCategory)
	}
	if view.Disposition() != DispositionAllocated {
		t.Fatalf("认过差的行应该是已核销，实际 %s", view.Disposition())
	}
	if view.UnallocatedAmount != "0.00" {
		t.Fatalf("认过差之后未分配应清零，实际 %s", view.UnallocatedAmount)
	}
	// 全额报回账本：队列上「处理完了没有」两条线一个定义。
	claims := ledger.claimLog()
	if len(claims) == 0 || !strings.Contains(claims[len(claims)-1], "5000.00") {
		t.Fatalf("结清后应全额报回账本，实际 %v", claims)
	}

	// 认过差的行：再核、再认、冲销，三条路都关。
	if _, err := svc.Allocate(ctx, tenantID, 8001,
		[]AllocationLine{{ContractID: contractID, Amount: "100"}}, op); err == nil ||
		!strings.Contains(err.Error(), "EX_TX_SETTLED") {
		t.Fatalf("结清的行不该还能核销，实际 %v", err)
	}
	if _, err := svc.SettleTransaction(ctx, tenantID, 8001, "LOSS", "", op); err == nil {
		t.Fatal("同一行认了两次差")
	}
	allocs, _ := svc.q.ListAllocationsOfTransaction(ctx, store.ListAllocationsOfTransactionParams{
		TenantID: tenantID, TransactionID: 8001,
	})
	if len(allocs) != 1 {
		t.Fatalf("准备冲销用的核销行数不对：%d", len(allocs))
	}
	if _, err := svc.ReverseAllocation(ctx, tenantID, allocs[0].ID, "试试", op); err == nil ||
		!strings.Contains(err.Error(), "EX_TX_SETTLED") {
		t.Fatalf("结清的行不该还能冲销，实际 %v", err)
	}

	// 撤销：没理由不行，有理由之后行回到待处理，账本报回真实已核 4800。
	if _, err := svc.RevokeSettlement(ctx, tenantID, 8001, "", op); err == nil {
		t.Fatal("没有理由的撤销被放行了")
	}
	view, err = svc.RevokeSettlement(ctx, tenantID, 8001, "认错了", op)
	if err != nil {
		t.Fatal(err)
	}
	if view.VarianceAmount != "" || view.Disposition() != DispositionUnprocessed {
		t.Fatalf("撤销后应回到待处理：variance=%q disp=%s", view.VarianceAmount, view.Disposition())
	}
	claims = ledger.claimLog()
	if !strings.Contains(claims[len(claims)-1], "4800.00") {
		t.Fatalf("撤销后应报回真实已核 4800，实际 %v", claims)
	}

	// 核满的行没差可认。
	if _, err := svc.Allocate(ctx, tenantID, 8002,
		[]AllocationLine{{ContractID: contractID, Amount: "1000"}}, op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SettleTransaction(ctx, tenantID, 8002, "ROUNDING", "", op); err == nil ||
		!strings.Contains(err.Error(), "EX_SETTLE_NOTHING") {
		t.Fatalf("核满的行不该有差可认，实际 %v", err)
	}

	// 出账没有应收差额可认。
	if _, err := svc.SettleTransaction(ctx, tenantID, 8003, "LOSS", "", op); err == nil ||
		!strings.Contains(err.Error(), "EX_TX_NOT_CREDIT") {
		t.Fatalf("出账认差应该被拒绝，实际 %v", err)
	}

	// 补足差额的类别：损耗如实记，不再谎报成手续费；胡写的拒绝。
	if _, err := svc.Allocate(ctx, tenantID, 8001,
		[]AllocationLine{{ContractID: contractID, Amount: "100", FeeAmount: "50", FeeCategory: "LOSS"}}, op); err != nil {
		t.Fatalf("损耗类别应该被接受：%v", err)
	}
	if _, err := svc.Allocate(ctx, tenantID, 8001,
		[]AllocationLine{{ContractID: contractID, Amount: "50", FeeCategory: "GIFT"}}, op); err == nil ||
		!strings.Contains(err.Error(), "EX_ALLOC_FEE_CATEGORY_INVALID") {
		t.Fatalf("胡写的补差类别应该被拒绝，实际 %v", err)
	}
}
