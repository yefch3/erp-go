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
//	· 重号在导入里是「重复，跳过」，在手工登记里是**报错**
//	· 金额只记大小，方向说明去向；负数要被拒绝
//	· 指定了收款账户就必须真的存在、而且没停用
//	· 账户名要跟着流水一起读出来，不能让页面每行再查一次
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

	// 不给归属条件时**必须出全部**。
	//
	// 这条是踩过坑才加的：按一组归属筛是用 cardinality($n::text[]) 实现的，
	// 而 Go 的 nil 切片到了 Postgres 是 NULL，cardinality(NULL) 是 NULL 不是
	// 0——条件整个变成 NULL，于是一行都不出，**而且不报错**。空列表看起来
	// 就像「这个月没有流水」。
	all, total, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 50, op)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) == 0 || total == 0 {
		t.Fatal("不给筛选条件时应该出全部，实际一行都没有")
	}

	// 按一组归属筛：只要客户那一档和还没人认过的。
	mine, _, err := svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{OwnershipIn: []string{OwnershipCustomer, ""}}, 1, 50, op)
	if err != nil {
		t.Fatalf("list by ownership set: %v", err)
	}
	for _, r := range mine {
		if r.Ownership != OwnershipCustomer && r.Ownership != "" {
			t.Fatalf("按组筛漏了别的归属进来: %+v", r)
		}
	}
	if len(mine) == 0 {
		t.Fatal("刚登记的那笔归属是客户，应该在里面")
	}

	// 认领状态：刚登记还没核销，应该在「还没认领完」那一档里。
	open, _, err := svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{ClaimStatus: ClaimOpen, Keyword: ref}, 1, 50, op)
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("刚登记的应该在待认领里，实际 %d 条", len(open))
	}
	if err := svc.SetBankTransactionClaim(ctx, tenantID, view.ID, view.Amount, op); err != nil {
		t.Fatalf("set claim: %v", err)
	}
	open, _, err = svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{ClaimStatus: ClaimOpen, Keyword: ref}, 1, 50, op)
	if err != nil {
		t.Fatalf("list open again: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("认领满之后就不该在待认领里了，实际 %d 条", len(open))
	}

	// 认领不能超过这一行本身——账上出现「认领 6 万、到账 5 万」比报错难查得多。
	if err := svc.SetBankTransactionClaim(ctx, tenantID, view.ID, "99999", op); err == nil ||
		!strings.Contains(err.Error(), "BANK_CLAIM_EXCEEDS") {
		t.Fatalf("超额认领应该被拒绝，实际 %v", err)
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

// **银行流水不看采购订单的数据范围。**
//
// 这条是修过一次才写下来的。原来这几个方法都要求「采购订单全量范围」，而
// 生产上有那个范围的只有超级管理员——FINANCE 和 PROCUREMENT_MANAGER 的
// procurement_order 都是 SELF。于是银行流水这个页面对财务是打不开的，
// 报的还是一句「对账视图需要采购订单的全量数据范围」，财务看不懂也没法处理。
//
// 数据范围回答的是「这些行归谁」。银行流水没有归属人：一笔汇款不是某个业务员
// 的，它就是公司账上的一笔钱。谁能看由权限决定，而权限归网关管。
//
// 反过来，供应商对账那边的全量要求必须还在——它算的是按供应商汇总的采购订单
// 金额，那些行确实有主人，按人截断的合计会像完整余额一样被当真。
func TestBankLedgerDoesNotUseOrderScope(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed scope test")
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

	// 一个只看得见自己那部分采购订单的人——生产上的财务就是这样。
	// 不给 views，scopeStub 兜底就是 SELF——生产上的财务正是这样。
	svc := New(pool, Deps{Scopes: &scopeStub{views: map[int64]Visibility{}}})
	op := Operator{ID: 42, Name: "财务小王"}

	acctID, err := svc.CreateBankAccount(ctx, tenantID, BankAccountView{
		AccountNo: "S-" + itoa64(tenantID), AccountName: "对公户", Currency: "USD",
	}, op)
	if err != nil {
		t.Fatalf("SELF 范围的人应该能建收款账户: %v", err)
	}
	if _, err := svc.ListBankAccounts(ctx, tenantID, false, op); err != nil {
		t.Fatalf("SELF 范围的人应该能看账户清单: %v", err)
	}
	row, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		AccountID: acctID, BankRef: "S-" + itoa64(tenantID), Direction: "CREDIT",
		Amount: "100", Currency: "USD", TxnDate: "2026-08-22",
		Ownership: OwnershipCustomer,
	}, op)
	if err != nil {
		t.Fatalf("SELF 范围的人应该能登记流水: %v", err)
	}
	if _, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 20, op); err != nil {
		t.Fatalf("SELF 范围的人应该能看银行流水: %v", err)
	}
	if _, err := svc.GetBankTransaction(ctx, tenantID, row.ID, op); err != nil {
		t.Fatalf("SELF 范围的人应该能看单行: %v", err)
	}
	// 改归属放在报认领之前：认领之后归属就该被锁住了（见
	// TestOwnershipCannotBeMovedAwayFromAllocatedRow），那是另一条规矩，
	// 和数据范围没关系。这个测试问的只是「范围拦不拦人」。
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, row.ID, OwnershipTaxRefund, "", op); err != nil {
		t.Fatalf("SELF 范围的人应该能改归属: %v", err)
	}
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, row.ID, OwnershipCustomer, "", op); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetBankTransactionClaim(ctx, tenantID, row.ID, "100", op); err != nil {
		t.Fatalf("SELF 范围的人应该能报认领: %v", err)
	}

	// 另一边：供应商对账仍然要全量，理由不一样，闸也不该跟着一起拆。
	if _, err := svc.ListSupplierStatements(ctx, tenantID, "", op); err == nil ||
		!strings.Contains(err.Error(), "PR_RECON_SCOPE_LIMITED") {
		t.Fatalf("供应商对账应该仍然要求全量范围，实际 %v", err)
	}
}

// 已经核销给出口合同的流水，不许把归属改走。
//
// 这条以前只写了供应商那一半：付款单认领了就不许改。客户那一半漏了，于是
// **两步就能把一笔已核销的钱变成孤儿**——在收款对账核销满，再到银行流水页
// 把归属改成「不用核销」，后端放行，而出口库里那几条核销记录还指着它，
// 合同上那笔钱照样算收到了。
//
// 两条线现在一边一句，形状一样。
func TestOwnershipCannotBeMovedAwayFromAllocatedRow(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed ownership guard test")
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
	}()

	svc := New(pool, Deps{})
	op := Operator{ID: 9, Name: "财务"}

	row, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: "OWN-" + itoa64(tenantID), Direction: "CREDIT",
		Amount: "10000", Currency: "USD", TxnDate: "2026-08-22",
		Ownership: OwnershipCustomer,
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	// 还没核销：归属随便改。
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, row.ID, OwnershipTaxRefund, "", op); err != nil {
		t.Fatalf("没核销过的行应该能改归属: %v", err)
	}
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, row.ID, OwnershipCustomer, "", op); err != nil {
		t.Fatal(err)
	}

	// 核销满了。
	if err := svc.SetBankTransactionClaim(ctx, tenantID, row.ID, "10000", op); err != nil {
		t.Fatal(err)
	}

	// 现在改到别的档：拒绝，而且要说清楚去哪儿冲销。
	for _, target := range []string{OwnershipTaxRefund, OwnershipOther, OwnershipSupplier, ""} {
		err := svc.SetBankTransactionOwnership(ctx, tenantID, row.ID, target, "", op)
		if err == nil || !strings.Contains(err.Error(), "BANK_TXN_OWNERSHIP_ALLOCATED") {
			t.Fatalf("已核销的行改归属到 %q 应该被拒绝，实际 %v", target, err)
		}
		if !strings.Contains(err.Error(), "收款对账") {
			t.Fatalf("拒绝的话要说去哪儿冲销，实际 %q", err.Error())
		}
	}

	// 改回「客户收款」是空操作，应该放行——否则连纠正 detail 都做不了。
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, row.ID, OwnershipCustomer, "", op); err != nil {
		t.Fatalf("改回客户那一档应该放行: %v", err)
	}

	// 冲销到 0 之后又能自由改了。
	if err := svc.SetBankTransactionClaim(ctx, tenantID, row.ID, "0", op); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, row.ID, OwnershipTaxRefund, "", op); err != nil {
		t.Fatalf("冲销之后应该又能改: %v", err)
	}
}
