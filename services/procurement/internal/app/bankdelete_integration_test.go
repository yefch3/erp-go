package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 删一条流水 = 归档，不是抹掉。
//
// 登记流水这一步原来没有回头路：填错一行就一直挂在列表里，只能在备注里写
// 一句「作废」。
//
// 这条测试钉住四件事，其中第三件是这个功能真正的边界：
//
//	· 删要填理由，空的拒掉
//	· 删完不在日常列表里，在「已删除」里，而且看得到谁删的、为什么
//	· **已经被认领的行不许删**——核销那几张表确实不指向流水，但反过来有：
//	  claimed_amount 这一列由客户核销那条线写回来。删掉一行已认领的流水，
//	  合同那边照样算收到了钱，而钱的来源进了已删除
//	· 归档的行还占着 bank_ref 的唯一键，所以重新登记同一笔会被挡——这正是
//	  「恢复」那条路存在的理由
func TestDeletingABankTransactionArchivesItInsteadOfErasingIt(t *testing.T) {
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
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, Deps{})
	op := Operator{ID: 88, Name: "财务小李"}

	mk := func(ref string, ownership string) int64 {
		t.Helper()
		v, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
			BankRef: ref, Direction: "CREDIT", Amount: "1000.00", Currency: "USD",
			TxnDate: "2026-08-20", Counterparty: "ACME", Ownership: ownership,
		}, op)
		if err != nil {
			t.Fatalf("登记 %s：%v", ref, err)
		}
		return v.ID
	}
	live := mk("REF-KEEP", OwnershipOther)
	doomed := mk("REF-TYPO", OwnershipOther)

	// 理由是必填的。一条没有理由的删除记录，事后翻到它还是不知道发生了什么。
	if err := svc.DeleteBankTransaction(ctx, tenantID, doomed, "   ", op); err == nil {
		t.Fatal("没填理由竟然删掉了")
	} else if !strings.Contains(err.Error(), "删除原因") {
		t.Errorf("要说清楚缺的是理由，拿到：%v", err)
	}

	const why = "同一笔录了两遍，这条是重复的"
	if err := svc.DeleteBankTransaction(ctx, tenantID, doomed, why, op); err != nil {
		t.Fatalf("删除：%v", err)
	}

	// 日常列表里没有它了。
	listed, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 50, op)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range listed {
		if v.ID == doomed {
			t.Error("删掉的流水还在日常列表里")
		}
	}
	if len(listed) != 1 || listed[0].ID != live {
		t.Errorf("没删的那条应该还在，实际列表 %d 条", len(listed))
	}

	// 「已删除」里有它，而且说得出谁删的、为什么。这是「作为存档」的全部意思：
	// 行还在，理由还在，署名还在。
	archived, _, err := svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{Deleted: true}, 1, 50, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].ID != doomed {
		t.Fatalf("已删除里应该正好是那一条，实际 %d 条", len(archived))
	}
	if archived[0].DeleteReason != why {
		t.Errorf("理由没留住，拿到 %q", archived[0].DeleteReason)
	}
	if archived[0].DeletedBy != op.Name {
		t.Errorf("删的人没留住，拿到 %q", archived[0].DeletedBy)
	}
	if archived[0].DeletedAt == "" {
		t.Error("删的时间没留住")
	}

	// 行还在，所以 bank_ref 的唯一键还挡着——重新导入同一份对账单不会让
	// 它悄悄长回来，这正是做成归档而不是真删的第三条理由。
	if _, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: "REF-TYPO", Direction: "CREDIT", Amount: "1000.00", Currency: "USD",
		TxnDate: "2026-08-20", Counterparty: "ACME", Ownership: OwnershipOther,
	}, op); err == nil {
		t.Error("归档的行还占着流水号，重新登记同号不该成功")
	}

	// 删过的不能再删一次——第二次会把第一次的理由和署名盖掉。
	if err := svc.DeleteBankTransaction(ctx, tenantID, doomed, "再删一次", op); err == nil {
		t.Error("已经在已删除里的还能再删一次")
	}

	// 恢复：放回列表，重新登记同号也就通了。
	if err := svc.RestoreBankTransaction(ctx, tenantID, doomed, op); err != nil {
		t.Fatalf("恢复：%v", err)
	}
	back, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 50, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Errorf("恢复之后列表该有两条，实际 %d 条", len(back))
	}
}

// 已经被认领的流水不许删。
//
// 需求上说的是「核销和流水不强绑定」，那句话对着一半：核销那几张表里确实
// 没有任何一列指向流水（查过 information_schema，零个外键、零个 bank 列）。
// 但反过来有——bank_transactions.claimed_amount 由客户核销那条线写回来
// （bankledger.go 的 SetBankTransactionClaim）。
//
// 所以删掉一行已认领的流水，合同那边照样算收到了钱，而钱的来源进了已删除。
// 这和 SetBankTransactionOwnership 上那道闸是同一个形状，那里的注释记着它
// 是怎么被发现的：「两步就能把一笔已核销的钱变成孤儿」。
func TestAClaimedBankTransactionCannotBeDeleted(t *testing.T) {
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
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, Deps{})
	op := Operator{ID: 88, Name: "财务小李"}

	v, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: "REF-CLAIMED", Direction: "CREDIT", Amount: "5000.00", Currency: "USD",
		TxnDate: "2026-08-20", Counterparty: "BUYER", Ownership: OwnershipCustomer,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	// 收款对账那边认领了其中一部分。
	if err := svc.SetBankTransactionClaim(ctx, tenantID, v.ID, "3000.00", op); err != nil {
		t.Fatalf("认领：%v", err)
	}

	err = svc.DeleteBankTransaction(ctx, tenantID, v.ID, "想删掉", op)
	if err == nil {
		t.Fatal("已认领的流水被删掉了——合同那边照样算收到钱，" +
			"而钱的来源进了已删除，账面上对不上而且不报错")
	}
	if !strings.Contains(err.Error(), "取消认领") {
		t.Errorf("要说清楚先做哪一步，拿到：%v", err)
	}

	// 取消认领之后就删得掉了——这道闸是「先做那一步」，不是「永远不许」。
	if err := svc.SetBankTransactionClaim(ctx, tenantID, v.ID, "0", op); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBankTransaction(ctx, tenantID, v.ID, "确实录错了", op); err != nil {
		t.Errorf("取消认领之后该删得掉：%v", err)
	}
}

// 改一行流水：留痕、要理由、只记真的变了的那几项。
//
// 有了编辑，「某个字段写错了」不用再走「删掉重新登记」——而那条路还会撞上
// bank_ref 的唯一键（号被归档的那一行占着）。这条测试钉住编辑本身，外加
// 那个死角的正解：**删错了就恢复出来再改**，不需要把流水号释放掉。
func TestEditingABankTransactionLeavesATrail(t *testing.T) {
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
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transaction_changes WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, Deps{})
	op := Operator{ID: 91, Name: "财务小王"}

	base := BankTransactionInput{
		BankRef: "REF-EDIT", Direction: "CREDIT", Amount: "1000.00", Currency: "USD",
		TxnDate: "2026-08-20", Counterparty: "ACME", Ownership: OwnershipOther,
	}
	v, err := svc.RecordBankTransaction(ctx, tenantID, base, op)
	if err != nil {
		t.Fatal(err)
	}

	// 理由必填，和删除同一个规矩。
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, base, "  ", op); err == nil {
		t.Fatal("没填理由竟然改成功了")
	}

	// 金额多打了一个零，改回来。
	fixed := base
	fixed.Amount = "100.00"
	const why = "金额多打了一个零，对账单上是 100"
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, fixed, why, op); err != nil {
		t.Fatalf("改金额：%v", err)
	}

	changes, err := svc.ListBankTransactionChanges(ctx, tenantID, v.ID, op)
	if err != nil {
		t.Fatal(err)
	}
	// **只记真的变了的那一项。** 一次「只改了金额」的保存，不该在变更记录里
	// 堆出十行「对方名称 ACME → ACME」。
	if len(changes) != 1 {
		t.Fatalf("只改了金额，该只有一条留痕，实际 %d 条：%+v", len(changes), changes)
	}
	c := changes[0]
	// 留痕存的是规范化之后的形式（decimal 的 String()，去掉末尾的零），
	// 不是界面上那个带两位小数的样子。1000 → 100 对读账的人一样清楚，
	// 而且两边同一个形式才比得出「有没有变」。
	if c.Field != "amount" || c.OldValue != "1000" || c.NewValue != "100" {
		t.Errorf("留痕要说清楚从什么改成什么，拿到 %+v", c)
	}
	if c.Reason != why || c.ChangedBy != op.Name {
		t.Errorf("理由和署名要留住，拿到 %+v", c)
	}

	// 一次改好几项 = 好几条留痕，一个字段一行。
	multi := fixed
	multi.Counterparty = "ACME CORP"
	multi.Note = "补了个备注"
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, multi, "补全对方全称", op); err != nil {
		t.Fatal(err)
	}
	changes, _ = svc.ListBankTransactionChanges(ctx, tenantID, v.ID, op)
	if len(changes) != 3 {
		t.Errorf("再改两项，总共该有三条留痕，实际 %d 条", len(changes))
	}

	// 什么都没改的保存不留痕，也不报错——人点了保存却没动任何东西，
	// 是个正常动作，不该在变更记录里留一条空白。
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, multi, "顺手点了保存", op); err != nil {
		t.Fatal(err)
	}
	changes, _ = svc.ListBankTransactionChanges(ctx, tenantID, v.ID, op)
	if len(changes) != 3 {
		t.Errorf("什么都没改不该留痕，实际变成 %d 条", len(changes))
	}
}

// 流水号被归档的那一行占着时，正解是「恢复出来再改」，不是把号释放掉。
//
// 这是用户提的那个死角：某个字段写错了 → 删掉重新登记 → 流水号已经被占了。
//
// 释放流水号（软删时改名，或者唯一键改成只对未删除的行生效）会把软删的
// 第三条理由破坏掉——「重复导入不会让删掉的行自己长回来」。所以号不释放，
// 而是给两条真正的出路：**直接改**（不用删），或者**恢复出来再改**。
func TestAnArchivedRefIsFreedByRestoringAndEditing(t *testing.T) {
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
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transaction_changes WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, Deps{})
	op := Operator{ID: 92, Name: "财务小王"}

	in := BankTransactionInput{
		BankRef: "REF-OOPS", Direction: "CREDIT", Amount: "1000.00", Currency: "USD",
		TxnDate: "2026-08-20", Counterparty: "ACME", Ownership: OwnershipOther,
	}
	v, err := svc.RecordBankTransaction(ctx, tenantID, in, op)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBankTransaction(ctx, tenantID, v.ID, "以为不能改，先删了", op); err != nil {
		t.Fatal(err)
	}

	// 已删除的行不许直接改：它在存档里，改它等于偷偷篡改一份封存的记录。
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, in, "想直接改", op); err == nil {
		t.Error("已删除的行被直接改掉了——存档不该能偷偷改")
	} else if !strings.Contains(err.Error(), "恢复") {
		t.Errorf("要告诉人先恢复，拿到：%v", err)
	}

	// 正解：恢复出来，再改。
	if err := svc.RestoreBankTransaction(ctx, tenantID, v.ID, op); err != nil {
		t.Fatal(err)
	}
	fixed := in
	fixed.Amount = "100.00"
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, fixed, "恢复之后改对", op); err != nil {
		t.Fatalf("恢复之后该改得动：%v", err)
	}

	// 流水号本身也能改——「号敲错了」这件事直接改就行，不用删。
	renamed := fixed
	renamed.BankRef = "REF-CORRECT"
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, renamed, "流水号敲错了", op); err != nil {
		t.Fatalf("改流水号：%v", err)
	}
	// 改成一个已经被占着的号要被拒——包括被已删除的行占着的。
	other, err := svc.RecordBankTransaction(ctx, tenantID, BankTransactionInput{
		BankRef: "REF-TAKEN", Direction: "DEBIT", Amount: "5.00", Currency: "USD",
		TxnDate: "2026-08-21", Counterparty: "B", Ownership: OwnershipOther,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBankTransaction(ctx, tenantID, other.ID, "不要了", op); err != nil {
		t.Fatal(err)
	}
	clash := renamed
	clash.BankRef = "REF-TAKEN"
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, clash, "换个号", op); err == nil {
		t.Error("改成一个已删除的行占着的号，竟然成功了")
	} else if !strings.Contains(err.Error(), "已删除") {
		t.Errorf("要说清楚号在哪儿被占着，拿到：%v", err)
	}
}

// 编辑表单里的归属和附件是真的会保存的。
//
// 界面上「改归属」和「传对账单」两个独立按钮撤掉了——那两件事本来就在编辑
// 表单里。撤按钮之前先得确认表单真的管用：**第一版不管用**，UPDATE 语句里
// 压根没写 ownership，表单送上去了后端忽略；附件那一路更明显，编辑分支在
// 上传之前就 return 了。两个都是「送上去了、没生效、不报错」。
//
// 归属的规则不走这里的通用比对，走 validateOwnershipChange——和单独改归属
// 那条路**同一份**。抄一遍的话两套迟早各长各的（比如一边查了核销另一边没查），
// 而走哪条取决于用户点了哪个按钮。
func TestEditingSavesOwnershipToo(t *testing.T) {
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
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transaction_changes WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_transactions WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, Deps{})
	op := Operator{ID: 93, Name: "财务小王"}

	in := BankTransactionInput{
		BankRef: "REF-OWN", Direction: "CREDIT", Amount: "1000.00", Currency: "USD",
		TxnDate: "2026-08-20", Counterparty: "BUYER",
	}
	v, err := svc.RecordBankTransaction(ctx, tenantID, in, op)
	if err != nil {
		t.Fatal(err)
	}
	if v.Ownership != "" {
		t.Fatalf("刚登记时归属该是空的（待处理），拿到 %q", v.Ownership)
	}

	// 在编辑表单里把归属改成「客户往来」。
	withOwner := in
	withOwner.Ownership = OwnershipCustomer
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, withOwner, "认出来是客户回款", op); err != nil {
		t.Fatalf("改归属：%v", err)
	}
	got, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 10, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Ownership != OwnershipCustomer {
		t.Fatalf("编辑表单里的归属没保存下来——表单送上去了，后端忽略了，"+
			"而且不报错。拿到 %+v", got)
	}

	// 二级分类只跟着「不用核销」走。这条规则住在 validateOwnershipChange
	// 里，编辑这条路必须也过它——不过的话，同一份数据从两个按钮进来会得到
	// 两种结果。
	bad := withOwner
	bad.OwnershipDetail = "运费"
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, bad, "试试", op); err == nil {
		t.Error("归属不是「不用核销」却带着二级分类，编辑这条路放行了——" +
			"说明它没走和改归属同一份规则")
	}

	// 已核销到合同的行，编辑表单里也不能把归属改走。同一道闸。
	if err := svc.SetBankTransactionClaim(ctx, tenantID, v.ID, "500.00", op); err != nil {
		t.Fatal(err)
	}
	moved := in
	moved.Ownership = OwnershipOther
	moved.OwnershipDetail = "运费"
	if err := svc.UpdateBankTransaction(ctx, tenantID, v.ID, moved, "改成不用核销", op); err == nil {
		t.Error("已核销到合同的行，从编辑表单里把归属改走了——" +
			"合同上那笔钱就指着一条写着「我不是你那条线上的」的流水")
	}
}
