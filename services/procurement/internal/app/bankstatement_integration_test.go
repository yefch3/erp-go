package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// The bank statement is the one voice in the reconciliation written by a
// machine that does not care what anybody meant. What this test pins down:
// imports dedupe on the bank's own reference so re-uploads are harmless,
// the suggestion finds the obvious counterpart without acting on it, the
// match guards hold (currency, direction, one-payment-per-row), and
// unmatching touches only our side of the story.
func TestBankStatementLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed bank statement test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_payments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Finance"}

	// A payment recorded three days before the wire shows on the statement.
	pay, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "10000", PaidAt: "2026-08-17", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	csv := "交易日期,借贷,金额,币种,对方户名,流水号\n" +
		"2026-08-20,借,10000.00,USD,鞍山钢厂,BK-" + itoa64(tenantID) + "-1\n" +
		"2026-08-21,贷,5000.00,USD,海外客户,BK-" + itoa64(tenantID) + "-2\n" +
		"坏日期,借,1,USD,x,BK-" + itoa64(tenantID) + "-3\n"
	summary, err := svc.ImportBankStatement(ctx, tenantID, "aug.csv", []byte(csv), "", op)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if summary.Imported != 2 || summary.Duplicates != 0 || len(summary.Errors) != 1 {
		t.Fatalf("first import: %+v", summary)
	}

	// Last week's file again: nothing doubles, nothing breaks.
	again, err := svc.ImportBankStatement(ctx, tenantID, "aug.csv", []byte(csv), "", op)
	if err != nil || again.Imported != 0 || again.Duplicates != 2 {
		t.Fatalf("re-upload must be harmless: %+v %v", again, err)
	}

	items, total, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op)
	if err != nil || total != 2 {
		t.Fatalf("list: %v total=%d", err, total)
	}
	var debit, credit *BankTransactionView
	for i := range items {
		if items[i].Direction == "DEBIT" {
			debit = &items[i]
		} else {
			credit = &items[i]
		}
	}
	// The 10000 USD wire three days from the payment is the obvious
	// counterpart — suggested, never auto-acted.
	if debit == nil || debit.SuggestedPaymentID != pay.ID {
		t.Fatalf("the wire should suggest its payment: %+v", debit)
	}
	if debit.MatchedPaymentID != 0 {
		t.Fatalf("a suggestion is not a match: %+v", debit)
	}
	// 这笔 5000 的进账金额和付款单对不上，所以不该被建议——**但理由是金额，
	// 不是方向**。见下。
	if credit.SuggestedPaymentID != 0 {
		t.Fatalf("incoming money suggests no supplier payment: %+v", credit)
	}

	// 这里原来断言的是「进账一律不许匹配付款单」（BANK_TXN_DIRECTION）。
	// 那条规矩本身就是 F2 要修的 bug：**供应商退款是钱进来的**，却必须对到
	// 采购的付款单上，按方向拦等于这类钱永远记不上账。
	//
	// 现在拦的是**归属**：这笔进账已经归给客户了，所以它不该出现在供应商
	// 匹配里——理由从「它是进账」换成了「它不是供应商那条线上的」。
	// 归属为空或为供应商的进账现在能匹配，见
	// TestSupplierRefundIsAnIncomingRowAndMustStillMatch。
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, credit.ID, OwnershipCustomer, "", op); err != nil {
		t.Fatalf("set ownership: %v", err)
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, credit.ID, pay.ID, op); err == nil ||
		!strings.Contains(err.Error(), "BANK_TXN_OWNERSHIP") {
		t.Fatalf("归属是客户的流水必须拒绝匹配供应商付款，实际 %v", err)
	}

	if err := svc.MatchBankTransaction(ctx, tenantID, debit.ID, pay.ID, op); err != nil {
		t.Fatalf("match: %v", err)
	}
	got, err := svc.GetSupplierPayment(ctx, tenantID, pay.ID)
	if err != nil || got.BankRef != debit.BankRef {
		t.Fatalf("the payment should carry the bank's reference: %+v %v", got, err)
	}

	// One payment per bank row, one bank row per payment.
	pay2, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "10000", PaidAt: "2026-08-19", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, debit.ID, pay2.ID, op); err == nil {
		t.Fatal("a claimed bank row must refuse a second payment")
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, credit.ID, pay.ID, op); err == nil {
		t.Fatal("a matched payment must refuse a second row")
	}

	items, _, _ = svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{Status: "MATCHED"}, 1, 20, op)
	if len(items) != 1 || items[0].MatchedPaymentNo != got.PaymentNo {
		t.Fatalf("matched filter should show the claim: %+v", items)
	}

	// Withdrawing the judgement frees both sides; the bank row is untouched.
	if err := svc.UnmatchBankTransaction(ctx, tenantID, debit.ID, op); err != nil {
		t.Fatalf("unmatch: %v", err)
	}
	got, _ = svc.GetSupplierPayment(ctx, tenantID, pay.ID)
	if got.BankRef != "" {
		t.Fatalf("unmatch should release the payment, got %q", got.BankRef)
	}
	if err := svc.UnmatchBankTransaction(ctx, tenantID, debit.ID, op); err == nil ||
		!strings.Contains(err.Error(), "BANK_TXN_NOT_MATCHED") {
		t.Fatalf("double unmatch must say so, got %v", err)
	}

	// 这里原来断言的是「按人截断的范围一行也看不见」。**那条断言把一个 bug
	// 写成了规矩**，所以改了——理由见 TestBankLedgerDoesNotUseOrderScope：
	//
	// 「一个银行账户不能按人切」这句话是对的，但它推不出「要有采购订单的全量
	// 范围」。数据范围回答的是「这些行归谁」，而银行流水没有归属人。用那把尺
	// 量的结果是：生产上只有超级管理员的 procurement_order 是 ALL，
	// FINANCE 和 PROCUREMENT_MANAGER 都是 SELF——**银行流水这个给财务做的
	// 页面，财务打不开**。
	//
	// 谁能看，由权限决定，而权限归网关管（/api/bank-transactions* 要
	// procurement:payment:read|write）。
	fenced := New(pool, Deps{Scopes: fixedScope{Visibility{All: false, ScopeType: "SELF"}}})
	if _, _, err := fenced.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op); err != nil {
		t.Fatalf("按人截断的范围也该看得见银行流水（谁能看归权限管），实际 %v", err)
	}
}

func itoa64(v int64) string { return time.Unix(0, v).Format("150405.000000000") }

// 供应商退款是**钱进来**的，却必须对到采购的付款单上。
//
// 这是 F2 的起点：原来匹配那道闸写死了 `direction != "DEBIT"` 就拒绝，于是
// 这类钱永远对不上号——凡是退过款的供应商，供应商对账那一行的「未付余额」
// 都是多算的，因为退回来的钱没地方记。
//
// 现在闸看的是**归属**而不是方向。这条测试就是那个 bug 的现场。
func TestSupplierRefundIsAnIncomingRowAndMustStillMatch(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_payments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	})

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Finance"}

	// 供应商退回来 2000 美元：类型是 REFUND，钱是**进**账。
	refund, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "鞍山钢厂", PaymentType: "REFUND",
		Currency: "USD", Amount: "2000", PaidAt: "2026-08-20", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	// 对账单上它是一条**贷方**（进账）记录。
	csv := "交易日期,借贷,金额,币种,对方户名,流水号\n" +
		"2026-08-21,贷,2000.00,USD,鞍山钢厂,RF-" + itoa64(tenantID) + "-1\n"
	if _, err := svc.ImportBankStatement(ctx, tenantID, "refund.csv", []byte(csv), "USD", op); err != nil {
		t.Fatal(err)
	}

	rows, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("导入了 %d 条，该是 1 条", len(rows))
	}
	txn := rows[0]
	if txn.Direction != "CREDIT" {
		t.Fatalf("方向 = %q，该是 CREDIT——退款就是钱进来", txn.Direction)
	}
	if txn.Ownership != "" {
		t.Fatalf("刚导进来的归属 = %q，该是空的（待处理）——不替人猜", txn.Ownership)
	}

	// 这一步在改之前会被拒绝：「只有出账流水才能匹配供应商付款」。
	if err := svc.MatchBankTransaction(ctx, tenantID, txn.ID, refund.ID, op); err != nil {
		t.Fatalf("供应商退款对不上号了：%v\n"+
			"退款是进账但归采购那条线——按方向拦，这笔钱就永远记不上，"+
			"供应商对账的欠款余额会一直多算", err)
	}

	// 匹配这个动作本身说明了归属，所以它该被顺手补上。
	rows, _, err = svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Ownership != OwnershipSupplier {
		t.Errorf("匹配之后归属 = %q，该被顺手补成 SUPPLIER", rows[0].Ownership)
	}
	if rows[0].MatchedPaymentID != refund.ID {
		t.Errorf("匹配没记上：matched=%d，该是 %d", rows[0].MatchedPaymentID, refund.ID)
	}
}

// 归属是判断，可以改；但改错方向会让别的记录变成谎话，所以有两道闸。
func TestBankTransactionOwnership(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_payments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	})

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Finance"}

	// 三条：一笔客户汇款、一笔银行利息、一笔付给供应商的。
	csv := "交易日期,借贷,金额,币种,对方户名,流水号\n" +
		"2026-08-20,贷,50000.00,USD,ACME TRADING,OW-" + itoa64(tenantID) + "-1\n" +
		"2026-08-20,贷,12.35,USD,利息,OW-" + itoa64(tenantID) + "-2\n" +
		"2026-08-20,借,8000.00,USD,鞍山钢厂,OW-" + itoa64(tenantID) + "-3\n"
	if _, err := svc.ImportBankStatement(ctx, tenantID, "mixed.csv", []byte(csv), "USD", op); err != nil {
		t.Fatal(err)
	}
	rows, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("导入了 %d 条，该是 3 条", len(rows))
	}
	byRef := map[string]BankTransactionView{}
	for _, r := range rows {
		byRef[r.BankRef[len(r.BankRef)-1:]] = r
	}

	// ---- 三条全都是待处理，一条都不许自动归 ----
	pending, _, err := svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{OwnershipPending: true}, 1, 20, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("待处理 %d 条，该是 3 条——导入不替人猜归属", len(pending))
	}

	// ---- 归给客户 ----
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, byRef["1"].ID, OwnershipCustomer, "", op); err != nil {
		t.Fatal(err)
	}
	// ---- 利息归「不用核销」，带二级分类 ----
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, byRef["2"].ID, OwnershipOther, "INTEREST", op); err != nil {
		t.Fatal(err)
	}

	got, _, err := svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{Ownership: OwnershipCustomer}, 1, 20, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].BankRef != byRef["1"].BankRef {
		t.Fatalf("按「客户」筛出 %d 条，该只有那一笔客户汇款", len(got))
	}
	got, _, _ = svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{Ownership: OwnershipOther}, 1, 20, op)
	if len(got) != 1 || got[0].OwnershipDetail != "INTEREST" {
		t.Fatalf("「不用核销」那一档的二级分类没留住：%+v", got)
	}

	// ---- 二级分类只有「不用核销」才有意义 ----
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, byRef["3"].ID, OwnershipSupplier, "INTEREST", op); err == nil {
		t.Error("归属是供应商却让填二级分类——那个值会躺在库里没人看，等于悄悄丢了")
	}

	// ---- 取值不认识就要说出来，不能悄悄存进去 ----
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, byRef["3"].ID, "随便写的", "", op); err == nil {
		t.Error("归属写了个不认识的值却放行")
	}

	// ---- 归属不是供应商的，不许匹配供应商付款 ----
	pay, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "鞍山钢厂", PaymentType: "SETTLEMENT",
		Currency: "USD", Amount: "50000", PaidAt: "2026-08-20", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MatchBankTransaction(ctx, tenantID, byRef["1"].ID, pay.ID, op); err == nil {
		t.Error("一笔已经归给客户的流水，被允许匹配成供应商付款")
	}

	// ---- 已匹配的流水不许把归属改走 ----
	if err := svc.MatchBankTransaction(ctx, tenantID, byRef["3"].ID, pay.ID, op); err != nil {
		t.Fatalf("归属待处理的出账该能匹配：%v", err)
	}
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, byRef["3"].ID, OwnershipCustomer, "", op); err == nil {
		t.Error("流水已被付款单认领，却允许把归属改成客户——那张付款单就指着" +
			"一笔写着「我不是供应商的钱」的流水了")
	}
}
