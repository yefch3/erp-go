package app

import (
	"strings"
	"testing"
)

// 付款对账的直接核销。钉的是六条：
//
//	· 出账直核到发票 → 自动建影子付款单（source=BANK、共用号段、带流水号），
//	  发票结清，认领全额写回流水
//	· 直核过的行「匹配付款单」进不来，反之亦然——一行只能走一条路
//	· 同一行不能直核两次
//	· 进账直核 = 退款影子单，负行那套守门原样生效（发票翻回 OPEN、不收手续费）
//	· 归属写着别人的行不能在付款对账核销
//	· 出账全给采购单 → 影子单类型是预付
func TestDirectSettleFromBankRow(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	invID := seedRefundInvoice(ctx, t, svc.pool, tenantID, "INV-DS-1", "5000")

	ref := itoa64(tenantID)
	csv := "交易日期,借贷,金额,币种,对方户名,流水号\n" +
		"2026-08-20,借,5000.00,USD,Mill,DS-" + ref + "-OUT\n" +
		"2026-08-22,贷,2000.00,USD,Mill,DS-" + ref + "-IN\n" +
		"2026-08-23,借,1000.00,USD,Mill,DS-" + ref + "-ADV\n" +
		"2026-08-23,借,300.00,USD,Cafe,DS-" + ref + "-LUNCH\n"
	if _, err := svc.ImportBankStatement(ctx, tenantID, "aug.csv", []byte(csv), "", op); err != nil {
		t.Fatal(err)
	}
	byRef := map[string]BankTransactionView{}
	list, _, err := svc.ListBankTransactions(ctx, tenantID, BankTransactionFilter{}, 1, 50, op)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list {
		byRef[strings.TrimPrefix(v.BankRef, "DS-"+ref+"-")] = v
	}

	// 出账 5000 直核到发票：影子单 + 结清 + 认领。
	shadow, err := svc.SettleBankTransactionToSupplier(ctx, tenantID, byRef["OUT"].ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "5000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if shadow.Source != "BANK" || shadow.PaymentType != "SETTLEMENT" {
		t.Fatalf("影子单不对：source=%q type=%q", shadow.Source, shadow.PaymentType)
	}
	if shadow.BankRef != "DS-"+ref+"-OUT" || shadow.PaymentNo == "" {
		t.Fatalf("影子单没带上流水号或单号：ref=%q no=%q", shadow.BankRef, shadow.PaymentNo)
	}
	if st := invoiceStatus(ctx, t, svc.pool, tenantID, invID); st != "SETTLED" {
		t.Fatalf("直核后发票应为 SETTLED，实际 %s", st)
	}
	list, _, err = svc.ListBankTransactions(ctx, tenantID,
		BankTransactionFilter{ClaimStatus: ClaimClaimed}, 1, 50, op)
	if err != nil {
		t.Fatal(err)
	}
	var claimedRow BankTransactionView
	for _, v := range list {
		if v.ID == byRef["OUT"].ID {
			claimedRow = v
		}
	}
	if claimedRow.ID == 0 || claimedRow.MatchedPaymentID != shadow.ID {
		t.Fatalf("直核的行应出现在「已核销」档并指着影子单：%+v", claimedRow)
	}

	// 一行只能走一条路：直核过的不能再匹配，也不能再直核。
	manual := pay(ctx, t, svc, tenantID, "SETTLEMENT", "5000", op)
	if err := svc.MatchBankTransaction(ctx, tenantID, byRef["OUT"].ID, manual.ID, op); err == nil {
		t.Fatal("直核过的行又被匹配了一张付款单——认领被记了两遍")
	}
	_, err = svc.SettleBankTransactionToSupplier(ctx, tenantID, byRef["OUT"].ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "1"}}, op)
	wantAllocErr(t, err, "BANK_SETTLE_ALREADY_CLAIMED")

	// 进账直核 = 退款影子单：负行守门原样生效。
	_, err = svc.SettleBankTransactionToSupplier(ctx, tenantID, byRef["IN"].ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "2000", FeeAmount: "5"}}, op)
	wantAllocErr(t, err, "PAY_ALLOC_REFUND_NO_FEE")
	refundShadow, err := svc.SettleBankTransactionToSupplier(ctx, tenantID, byRef["IN"].ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "2000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if refundShadow.PaymentType != "REFUND" || refundShadow.Source != "BANK" {
		t.Fatalf("进账的影子单应为 REFUND：%+v", refundShadow.PaymentType)
	}
	if len(refundShadow.Allocations) != 1 || !strings.HasPrefix(refundShadow.Allocations[0].Amount, "-2000") {
		t.Fatalf("退款影子单的核销行应为 -2000：%+v", refundShadow.Allocations)
	}
	if st := invoiceStatus(ctx, t, svc.pool, tenantID, invID); st != "OPEN" {
		t.Fatalf("退款直核后发票应翻回 OPEN，实际 %s", st)
	}

	// 出账全给采购单 → 预付影子单。
	var poID int64
	if err := svc.pool.QueryRow(ctx, `
		INSERT INTO purchase_orders
		  (tenant_id, po_no, supplier_id, supplier_name, currency, total_amount, status)
		VALUES ($1, 'PO-DS-1', 9, 'Mill', 'USD', 3000, 'ORDERED')
		RETURNING id`, tenantID).Scan(&poID); err != nil {
		t.Fatal(err)
	}
	advShadow, err := svc.SettleBankTransactionToSupplier(ctx, tenantID, byRef["ADV"].ID,
		[]PaymentAllocationInput{{POID: poID, Amount: "1000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if advShadow.PaymentType != "ADVANCE" {
		t.Fatalf("全给采购单的影子单应为 ADVANCE，实际 %s", advShadow.PaymentType)
	}

	// 归属写着别人的行，付款对账不收。
	if err := svc.SetBankTransactionOwnership(ctx, tenantID, byRef["LUNCH"].ID, OwnershipOther, "餐费", op); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SettleBankTransactionToSupplier(ctx, tenantID, byRef["LUNCH"].ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "300"}}, op)
	wantAllocErr(t, err, "BANK_TXN_OWNERSHIP")
}
