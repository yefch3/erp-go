package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 供应商退款的符号。
//
// supplier_payments.amount 有 CHECK(amount>0)，退款只能记成「REFUND 类型 +
// 正数」。于是每一处把付款金额直接加起来的地方，都会把「供应商退我们 2000」
// 读成「我们又付了 2000」——方向整个反了。这个文件钉住三道闸：
//
//  1. 供应商对账的「未核销付款」把 REFUND 取负
//  2. 退款核销走标准目标校验（负行方案的完整生命周期在
//     payment_refund_alloc_integration_test.go）
//  3. 流水匹配和自动建议都要求方向自洽：出账↔预付/结算，进账↔退款

func refundTestPool(t *testing.T) (context.Context, *Service, int64, func()) {
	t.Helper()
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed refund sign test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM payment_allocations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_payments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_invoices WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
		pool.Close()
	}
	return ctx, New(pool, Deps{}), tenantID, cleanup
}

func TestARefundCountsAgainstNotTowardTheSupplierBalance(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}

	// 付出去 3000，供应商退回来 2000。
	if _, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "3000", PaidAt: "2026-08-20", Method: "WIRE",
	}, op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "REFUND",
		Currency: "USD", Amount: "2000", PaidAt: "2026-08-22", Method: "WIRE",
	}, op); err != nil {
		t.Fatal(err)
	}

	rows, err := svc.ListSupplierStatements(ctx, tenantID, "", op)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, r := range rows {
		if r.SupplierID == 9 && r.Currency == "USD" {
			got = r.UnallocatedAmount
		}
	}
	// 3000 付出 − 2000 退回 = 还有 1000 停在供应商那里没核销。
	// 直接求和会得出 5000——把退款当成了又一笔付出去的钱，而这一列正是
	// 采购经理决定「还要不要再付」时看的数。
	if got != "1000" && got != "1000.00" {
		t.Fatalf("未核销付款 = %q，应为 1000（退款要取负；5000 就是把方向读反了）", got)
	}
}

// 阶段 0 曾把「退款单不能核销」整个封死（PAY_ALLOC_REFUND）。退款改走
// 负核销行之后那道闸拆了，但拆闸不等于不设防：退款核销走的是和付款核销
// 同一套目标校验。这条钉住换闸后的门牌——胡乱指一张不存在的发票，得到的
// 是 NOT_FOUND，而不是当年的一刀切拒绝，更不是静默成功。
func TestARefundAllocationStillValidatesItsTarget(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}

	refund, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "REFUND",
		Currency: "USD", Amount: "2000", PaidAt: "2026-08-22", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AllocateSupplierPayment(ctx, tenantID, refund.ID, []PaymentAllocationInput{
		{InvoiceID: 424242, Amount: "2000"},
	}, op)
	if err == nil {
		t.Fatal("退款核销到不存在的发票居然成功了")
	}
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Code != "PAY_ALLOC_INVOICE_NOT_FOUND" {
		t.Fatalf("拒绝的理由不对：%v（要 PAY_ALLOC_INVOICE_NOT_FOUND）", err)
	}
}

func TestMatchingRefusesADirectionTypeMismatch(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}

	// 同一家供应商，同一笔金额：付出去（出账）和退回来（进账）几天内成对
	// 出现是常态——付错了当天退回。币种金额日期全同，人眼分不出方向。
	settle, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "2000", PaidAt: "2026-08-21", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	refund, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "REFUND",
		Currency: "USD", Amount: "2000", PaidAt: "2026-08-22", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	ref := itoa64(tenantID)
	csv := "交易日期,借贷,金额,币种,对方户名,流水号\n" +
		"2026-08-21,借,2000.00,USD,Mill,RS-" + ref + "-OUT\n" +
		"2026-08-22,贷,2000.00,USD,Mill,RS-" + ref + "-IN\n"
	if _, err := svc.ImportBankStatement(ctx, tenantID, "aug.csv", []byte(csv), "", op); err != nil {
		t.Fatal(err)
	}
	list, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 50, op)
	if err != nil {
		t.Fatal(err)
	}
	var out, in BankTransactionView
	for _, v := range list {
		if strings.HasSuffix(v.BankRef, "-OUT") {
			out = v
		}
		if strings.HasSuffix(v.BankRef, "-IN") {
			in = v
		}
	}
	if out.ID == 0 || in.ID == 0 {
		t.Fatalf("导入的两行没找齐：out=%d in=%d", out.ID, in.ID)
	}

	// 自动建议也要方向自洽：两张单金额币种日期全部命中，唯一能分开它们的
	// 就是方向。建议一旦指反，界面上的「采纳」是一键的。
	if out.SuggestedPaymentID != settle.ID {
		t.Fatalf("出账行建议了 %d，应建议结算单 %d", out.SuggestedPaymentID, settle.ID)
	}
	if in.SuggestedPaymentID != refund.ID {
		t.Fatalf("进账行建议了 %d，应建议退款单 %d", in.SuggestedPaymentID, refund.ID)
	}

	// 交叉配对两个方向都错，必须拒绝。
	if err := svc.MatchBankTransaction(ctx, tenantID, out.ID, refund.ID, op); err == nil {
		t.Fatal("一笔出账被对到了退款单上")
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, in.ID, settle.ID, op); err == nil {
		t.Fatal("一笔进账被对到了结算单上")
	}
	// 顺向的两对照常能配。
	if err := svc.MatchBankTransaction(ctx, tenantID, out.ID, settle.ID, op); err != nil {
		t.Fatalf("出账对结算被拒了：%v", err)
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, in.ID, refund.ID, op); err != nil {
		t.Fatalf("进账对退款被拒了：%v", err)
	}
}
