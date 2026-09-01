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
