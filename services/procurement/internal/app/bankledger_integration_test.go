package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 手工登记这条入口是 F2 第二步加的。它和 CSV 导入共用一张表，但**规矩不
// 一样**，这个测试钉的就是那些不一样的地方：
//
//   · 重号在导入里是「重复，跳过」，在手工登记里是**报错**
//   · 金额只记大小，方向说明去向；负数要被拒绝
//   · 指定了收款账户就必须真的存在、而且没停用
//   · 账户名要跟着流水一起读出来，不能让页面每行再查一次
func TestRecordBankTransactionByHand(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed bank ledger test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_accounts WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Finance"}

	acctID, err := svc.CreateBankAccount(ctx, tenantID, BankAccountView{
		AccountNo: "622848-" + itoa64(tenantID), AccountName: "宁波信达外币户",
		BankName: "中行", Currency: "USD",
	}, op)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	ref := "MANUAL-" + itoa64(tenantID) + "-1"
	view, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		AccountID: acctID, BankRef: ref, Direction: "CREDIT",
		Amount: "49,975.00", Currency: "usd", TxnDate: "2026/08/22",
		Counterparty: "SANTOS TRADING", CounterpartyAccount: "BR-99887",
		RemittanceInfo: "PAYMENT FOR EXP-2026-0031",
		Ownership:      OwnershipCustomer,
	}, op)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	// 输入被规整过：币种大写、日期统一、金额去掉千分位。原样存进去的话，
	// 同一笔钱会因为写法不同在筛选里漏掉。
	// 金额读回来是 49975.00 而不是 49975——列是 NUMERIC(18,2)，两位小数是
	// 它的形状，不是格式化出来的。
	if view.Currency != "USD" || view.TxnDate != "2026-08-22" || view.Amount != "49975.00" {
		t.Fatalf("规整没生效: currency=%q date=%q amount=%q", view.Currency, view.TxnDate, view.Amount)
	}
	if view.Source != "MANUAL" {
		t.Fatalf("手工登记的来源应该是 MANUAL，实际 %q", view.Source)
	}
	// 账户名跟着流水一起出来，页面不必每行再查一次。
	if view.AccountID != acctID || view.AccountName != "宁波信达外币户" {
		t.Fatalf("账户没带出来: id=%d name=%q", view.AccountID, view.AccountName)
	}
	if view.RemittanceInfo != "PAYMENT FOR EXP-2026-0031" {
		t.Fatalf("汇款附言丢了: %q", view.RemittanceInfo)
	}
	if view.Ownership != OwnershipCustomer {
		t.Fatalf("归属没写上: %q", view.Ownership)
	}

	// 同一个流水号再登记一次：**必须报错**。导入里这算「重复，跳过」，
	// 因为重传上周的文件是正常操作；手工敲进来的重号只可能是记过了或者
	// 敲错了，默默吞掉会让人以为钱记上了而账上根本没有。
	_, err = svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: ref, Direction: "CREDIT", Amount: "1", Currency: "USD",
		TxnDate: "2026-08-23",
	}, op)
	if err == nil || !strings.Contains(err.Error(), "BANK_REF_EXISTS") {
		t.Fatalf("重号应该被拒绝，实际 %v", err)
	}

	// 负数金额：方向已经说明了钱往哪走，再带一个符号就是双重否定。
	_, err = svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: ref + "-neg", Direction: "DEBIT", Amount: "-100",
		Currency: "USD", TxnDate: "2026-08-23",
	}, op)
	if err == nil || !strings.Contains(err.Error(), "BANK_AMOUNT_INVALID") {
		t.Fatalf("负数金额应该被拒绝，实际 %v", err)
	}

	// 没有流水号就没有任何东西拦得住同一笔钱记两遍。
	_, err = svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		Direction: "CREDIT", Amount: "100", Currency: "USD", TxnDate: "2026-08-23",
	}, op)
	if err == nil || !strings.Contains(err.Error(), "BANK_REF_REQUIRED") {
		t.Fatalf("空流水号应该被拒绝，实际 %v", err)
	}

	// 指定了一个不存在的账户：拒绝。编出来的账户号会让「内部划转认成客户
	// 打款」那道防线失效。
	_, err = svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		AccountID: acctID + 999_999, BankRef: ref + "-x", Direction: "CREDIT",
		Amount: "100", Currency: "USD", TxnDate: "2026-08-23",
	}, op)
	if err == nil || !strings.Contains(err.Error(), "BANK_ACCOUNT_NOT_FOUND") {
		t.Fatalf("不存在的账户应该被拒绝，实际 %v", err)
	}

	// 归属不是「不用核销」却填了二级分类：和改归属那一步同一条规矩。
	_, err = svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: ref + "-d", Direction: "CREDIT", Amount: "100", Currency: "USD",
		TxnDate: "2026-08-23", Ownership: OwnershipCustomer, OwnershipDetail: "INTEREST",
	}, op)
	if err == nil || !strings.Contains(err.Error(), "BANK_TXN_OWNERSHIP_DETAIL") {
		t.Fatalf("客户归属不该能填二级分类，实际 %v", err)
	}

	// 单行读取拿到的东西必须和列表里那一行一致——核销靠它拿金额和币种。
	got, err := svc.GetBankTransaction(ctx, tenantID, view.ID, op)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Amount != view.Amount || got.Currency != view.Currency || got.BankRef != ref {
		t.Fatalf("单行读取对不上: %+v", got)
	}
	if _, err := svc.GetBankTransaction(ctx, tenantID, view.ID+999_999, op); err == nil ||
		!strings.Contains(err.Error(), "BANK_TXN_NOT_FOUND") {
		t.Fatalf("不存在的行应该报 NOT_FOUND，实际 %v", err)
	}

	// 手工登记的行要能在列表里按附言搜到——附言是客户写合同号的地方。
	items, _, err := svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{Keyword: "EXP-2026-0031"}, 1, 20, op)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].ID != view.ID {
		t.Fatalf("按附言没搜到: %+v", items)
	}
	if items[0].AccountName != "宁波信达外币户" || items[0].Source != "MANUAL" {
		t.Fatalf("列表里少了新字段: %+v", items[0])
	}
}

// 账户清单默认只给启用的：登记流水的下拉框里不该出现已经销户的账户，
// 否则钱会被记到一个不存在的地方去。
func TestListBankAccountsHidesInactive(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed bank account test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_accounts WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 77, Name: "Finance"}

	live, err := svc.CreateBankAccount(ctx, tenantID, BankAccountView{
		AccountNo: "L-" + itoa64(tenantID), AccountName: "在用", Currency: "USD",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	dead, err := svc.CreateBankAccount(ctx, tenantID, BankAccountView{
		AccountNo: "D-" + itoa64(tenantID), AccountName: "已销户", Currency: "USD",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE bank_accounts SET status='INACTIVE' WHERE id=$1`, dead); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ListBankAccounts(ctx, tenantID, false, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != live {
		t.Fatalf("默认应该只给启用的，实际 %+v", got)
	}
	all, err := svc.ListBankAccounts(ctx, tenantID, true, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("要停用的时候应该都给，实际 %+v", all)
	}

	// 同一个账号登记两次：拒绝。两条记录指同一个账户，对账时会看见
	// 同一笔钱进了「两个」账户。
	_, err = svc.CreateBankAccount(ctx, tenantID, BankAccountView{
		AccountNo: "L-" + itoa64(tenantID), AccountName: "又来一次", Currency: "USD",
	}, op)
	if err == nil || !strings.Contains(err.Error(), "BANK_ACCOUNT_EXISTS") {
		t.Fatalf("重复账号应该被拒绝，实际 %v", err)
	}
}
