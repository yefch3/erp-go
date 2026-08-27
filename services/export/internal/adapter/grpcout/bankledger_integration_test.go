package grpcout_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/export/internal/adapter/grpcout"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

// 出口和采购在**网线上**对不对得上。
//
// app 那边的测试用的是假账本，钉的是「金额守不守得住」；假账本永远和接口
// 定义一致，所以它证明不了字段名对不对、日期格式一不一样、错误码传不传得
// 过来。这个测试对着真的采购服务跑，钉的就是那些只有过一次网络才会暴露的
// 东西——比如账本那边叫 txn_date、出口这边叫 value_date。
//
// 跑法（需要本地把采购服务起起来，`make services-up`）：
//
//	PROCUREMENT_GRPC_ADDR=127.0.0.1:9007 \
//	INTERNAL_SIGNING_KEY=<和容器里同一个> \
//	go test ./internal/adapter/grpcout/ -run CrossService
func TestBankLedgerCrossService(t *testing.T) {
	addr := os.Getenv("PROCUREMENT_GRPC_ADDR")
	if addr == "" {
		t.Skip("PROCUREMENT_GRPC_ADDR not set; skipping cross-service bank ledger test")
	}
	if os.Getenv("INTERNAL_SIGNING_KEY") == "" {
		t.Skip("INTERNAL_SIGNING_KEY not set; the callee refuses unsigned calls")
	}
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(grpcx.UnaryClientPropagator()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ledger := grpcout.NewBankLedger(conn)
	tenantID := time.Now().UnixNano()
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{
		TenantID: tenantID, EmployeeID: 1, Name: "Finance",
	})

	acctID, err := ledger.CreateAccount(ctx, app.BankAccount{
		AccountNo: "X-" + itoa(tenantID), AccountName: "跨服务测试账户", Currency: "USD",
	})
	if err != nil {
		t.Fatalf("建账户: %v", err)
	}

	ref := "XS-" + itoa(tenantID)
	row, err := ledger.Record(ctx, app.BankRowInput{
		AccountID: acctID, BankRef: ref, Direction: "CREDIT",
		Amount: "5000.00", Currency: "USD", ValueDate: "2026-08-22",
		Counterparty: "SANTOS", RemittanceInfo: "FOR EXP-2026-0031",
	})
	if err != nil {
		t.Fatalf("登记: %v", err)
	}
	// 日期这一栏两边名字不一样（账本 txn_date / 出口 value_date）。翻译错了
	// 页面上就是一片空白的到账日期，而单元测试永远看不见这种错。
	if row.ValueDate != "2026-08-22" {
		t.Fatalf("到账日期没接上: %q", row.ValueDate)
	}
	if row.Amount != "5000.00" || row.Currency != "USD" {
		t.Fatalf("金额币种对不上: %+v", row)
	}
	if row.RemittanceInfo != "FOR EXP-2026-0031" {
		t.Fatalf("汇款附言丢了: %q", row.RemittanceInfo)
	}
	// 从收款对账登记的，归属应该当场就是「客户收款」。
	if row.Ownership != app.OwnershipCustomer {
		t.Fatalf("归属应该是客户，实际 %q", row.Ownership)
	}
	if row.AccountName != "跨服务测试账户" {
		t.Fatalf("账户名没跟着出来: %q", row.AccountName)
	}
	if row.Source != "MANUAL" {
		t.Fatalf("来源应该是 MANUAL，实际 %q", row.Source)
	}

	// 收款对账那个队列：归属客户 + 还没核完，这一笔该在里面。
	items, total, err := ledger.List(ctx, app.BankLedgerQuery{
		OwnershipIn: []string{app.OwnershipCustomer, app.OwnershipPending},
		ClaimStatus: "OPEN", Direction: "CREDIT", Keyword: ref, Page: 1, Size: 20,
	})
	if err != nil {
		t.Fatalf("队列: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != row.ID {
		t.Fatalf("刚登记的这笔该在待处理队列里，实际 total=%d items=%d", total, len(items))
	}

	// 报认领：核满之后就该从待处理里消失。
	if err := ledger.SetClaim(ctx, row.ID, "5000.00"); err != nil {
		t.Fatalf("报认领: %v", err)
	}
	items, _, err = ledger.List(ctx, app.BankLedgerQuery{
		OwnershipIn: []string{app.OwnershipCustomer, app.OwnershipPending},
		ClaimStatus: "OPEN", Direction: "CREDIT", Keyword: ref, Page: 1, Size: 20,
	})
	if err != nil {
		t.Fatalf("队列二次: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("核满之后不该还在待处理里，实际 %d 条", len(items))
	}

	// 错误码要能原样传回来——页面靠它决定给用户看哪句话。
	//
	// 码走的是 gRPC 的 ErrorInfo.Reason，**不在 err.Error() 里**（那里只有
	// 给人看的那句话）。所以要用 CodeFromStatus 取，直接 strings.Contains
	// 是取不到的。
	err = ledger.SetClaim(ctx, row.ID, "999999")
	if code := apierr.CodeFromStatus(err); code != "BANK_CLAIM_EXCEEDS" {
		t.Fatalf("超额认领的错误码没传过来: code=%q err=%v", code, err)
	}
	// 那句给人看的话也得过来，别在网线上变成一句 "rpc error"。
	if err == nil || !strings.Contains(err.Error(), "超过这一行") {
		t.Fatalf("错误的正文没传过来: %v", err)
	}

	// 已经核满的行不许把归属改走——这道闸也得在网线上拦得住，
	// 而且错误码要能原样传回来。
	err = ledger.SetOwnership(ctx, row.ID, app.OwnershipTaxRefund, "")
	if code := apierr.CodeFromStatus(err); code != "BANK_TXN_OWNERSHIP_ALLOCATED" {
		t.Fatalf("已核销的行改归属应该被拒绝: code=%q err=%v", code, err)
	}

	// 冲销回 0 之后才能改。改完这一笔从客户那一档消失。
	if err := ledger.SetClaim(ctx, row.ID, "0"); err != nil {
		t.Fatalf("冲销: %v", err)
	}
	if err := ledger.SetOwnership(ctx, row.ID, app.OwnershipTaxRefund, ""); err != nil {
		t.Fatalf("改归属: %v", err)
	}
	got, err := ledger.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("取单行: %v", err)
	}
	if got.Ownership != app.OwnershipTaxRefund {
		t.Fatalf("归属没改成: %q", got.Ownership)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
