package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// 供应商退款走负核销行。钉的是七条，逐条对着客户侧（export 的
// refund_queue_integration_test.go）的镜像：
//
//	· 付 5000 结清发票，退 2000 → 发票翻回 OPEN、净核销 7000→3000——
//	  对账和结清判定的求和一个字没改，负行自动把数拉下来
//	· 退款单自己的「未分配」按量算：退出去 2000、说清 2000 → 余 0
//	· 退款不能超过发票的净核销额（连续退两笔也框得住）
//	· 退款行不收手续费
//	· 挂着退款时不许先冲销付款核销（净额会变负）；发票又被补核销到满后，
//	  也不许冲销退款（净额会超票面）
//	· 付 2000 退 2000 净额为 0 的发票**不能作废**——两笔真钱都动过；
//	  逐笔冲销干净之后才能作废
//	· 取消的采购单能收预付款退款（订金退回的主场），同样按净预付框天花板

func seedRefundInvoice(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tenantID int64, no, total string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO supplier_invoices
		  (tenant_id, supplier_id, supplier_name, invoice_no, currency, total_amount, invoice_date)
		VALUES ($1, 9, 'Mill', $2, 'USD', $3::numeric, '2026-08-01')
		RETURNING id`, tenantID, no, total).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func invoiceStatus(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tenantID, id int64) string {
	t.Helper()
	var st string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM supplier_invoices WHERE tenant_id=$1 AND id=$2`,
		tenantID, id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func pay(ctx context.Context, t *testing.T, svc *Service, tenantID int64, ptype, amount string, op Operator) SupplierPayment {
	t.Helper()
	p, err := svc.CreateSupplierPayment(ctx, tenantID, SupplierPaymentInput{
		SupplierID: 9, SupplierName: "Mill", PaymentType: ptype,
		Currency: "USD", Amount: amount, PaidAt: "2026-08-20", Method: "WIRE",
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func wantAllocErr(t *testing.T, err error, code string) {
	t.Helper()
	var ae *apierr.Error
	if err == nil || !errors.As(err, &ae) || ae.Code != code {
		t.Fatalf("要 %s，实际 %v", code, err)
	}
}

func TestSupplierRefundLifecycle(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	invID := seedRefundInvoice(ctx, t, svc.pool, tenantID, "INV-RL-1", "5000")

	// 付 5000 结清。
	settle := pay(ctx, t, svc, tenantID, "SETTLEMENT", "5000", op)
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, settle.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "5000"}}, op); err != nil {
		t.Fatal(err)
	}
	if st := invoiceStatus(ctx, t, svc.pool, tenantID, invID); st != "SETTLED" {
		t.Fatalf("付满后发票应为 SETTLED，实际 %s", st)
	}

	// 退款行不收手续费。
	refund := pay(ctx, t, svc, tenantID, "REFUND", "2000", op)
	_, err := svc.AllocateSupplierPayment(ctx, tenantID, refund.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "100", FeeAmount: "5"}}, op)
	wantAllocErr(t, err, "PAY_ALLOC_REFUND_NO_FEE")

	// 退 2000：界面填正数，落库为负；已结清的发票收下退款并翻回 OPEN。
	view, err := svc.AllocateSupplierPayment(ctx, tenantID, refund.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "2000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if view.Unallocated != "0.00" && view.Unallocated != "0" {
		t.Fatalf("退清的退款单未分配应为 0，实际 %s（负行没翻号就会算成 4000）", view.Unallocated)
	}
	if len(view.Allocations) != 1 || !strings.HasPrefix(view.Allocations[0].Amount, "-2000") {
		t.Fatalf("退款核销行应落 -2000，实际 %+v", view.Allocations)
	}
	if st := invoiceStatus(ctx, t, svc.pool, tenantID, invID); st != "OPEN" {
		t.Fatalf("退款后发票应翻回 OPEN，实际 %s", st)
	}

	// 再退 4000：净核销只剩 3000，框住；退 3000 可以。
	refund2 := pay(ctx, t, svc, tenantID, "REFUND", "4000", op)
	_, err = svc.AllocateSupplierPayment(ctx, tenantID, refund2.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "4000"}}, op)
	wantAllocErr(t, err, "PAY_ALLOC_REFUND_EXCEEDS_SETTLED")
	view, err = svc.AllocateSupplierPayment(ctx, tenantID, refund2.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "3000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	// 退 4000 说清 3000，余量报 1000。
	if view.Unallocated != "1000.00" && view.Unallocated != "1000" {
		t.Fatalf("退款单余量应为 1000，实际 %s", view.Unallocated)
	}

	// 净核销已是 0：这时不许先冲销付款核销（净额会变 -5000）。
	full, err := svc.GetSupplierPayment(ctx, tenantID, settle.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ReverseSupplierPaymentAllocation(ctx, tenantID, full.Allocations[0].ID, "想撤", op)
	wantAllocErr(t, err, "PAY_REVERSE_REFUND_FIRST")

	// 冲销第二笔退款（+3000 回来），净核销回到 3000。
	if _, err := svc.ReverseSupplierPaymentAllocation(ctx, tenantID, view.Allocations[0].ID, "退错了", op); err != nil {
		t.Fatal(err)
	}
	// 冲销干净不等于能作废：发票上还挂着 5000 核销和 -2000 退款两笔活行。
	_, err = svc.VoidSupplierInvoice(ctx, tenantID, invID, "不要了", op)
	wantAllocErr(t, err, "INV_HAS_ALLOCATIONS")
}

// 付 2000 又退 2000：净额为 0，但两笔真钱都动过——不能当「从没结算过」作废。
// 逐笔冲销之后才放行。这是负行方案顺手补上的真洞：旧判据「净额=0」会放它过。
func TestNetZeroInvoiceStillRefusesToVoid(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	invID := seedRefundInvoice(ctx, t, svc.pool, tenantID, "INV-VZ-1", "2000")

	settle := pay(ctx, t, svc, tenantID, "SETTLEMENT", "2000", op)
	sview, err := svc.AllocateSupplierPayment(ctx, tenantID, settle.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "2000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	refund := pay(ctx, t, svc, tenantID, "REFUND", "2000", op)
	rview, err := svc.AllocateSupplierPayment(ctx, tenantID, refund.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "2000"}}, op)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.VoidSupplierInvoice(ctx, tenantID, invID, "不要了", op)
	wantAllocErr(t, err, "INV_HAS_ALLOCATIONS")

	// 先冲退款，再冲付款（反过来先冲付款会把净额压成负数，上面那条测过）。
	if _, err := svc.ReverseSupplierPaymentAllocation(ctx, tenantID, rview.Allocations[0].ID, "冲退款", op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReverseSupplierPaymentAllocation(ctx, tenantID, sview.Allocations[0].ID, "冲付款", op); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VoidSupplierInvoice(ctx, tenantID, invID, "不要了", op); err != nil {
		t.Fatalf("冲销干净的发票应能作废：%v", err)
	}
}

// 发票被退款后又补核销到满，这时冲销那笔退款会把净核销顶破票面——拒绝。
func TestReversingARefundCannotOversettleTheInvoice(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}
	invID := seedRefundInvoice(ctx, t, svc.pool, tenantID, "INV-OS-1", "5000")

	settle := pay(ctx, t, svc, tenantID, "SETTLEMENT", "5000", op)
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, settle.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "5000"}}, op); err != nil {
		t.Fatal(err)
	}
	refund := pay(ctx, t, svc, tenantID, "REFUND", "2000", op)
	rview, err := svc.AllocateSupplierPayment(ctx, tenantID, refund.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "2000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	// 缺口 2000 由另一笔付款补上，发票重新结清。
	topup := pay(ctx, t, svc, tenantID, "SETTLEMENT", "2000", op)
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, topup.ID,
		[]PaymentAllocationInput{{InvoiceID: invID, Amount: "2000"}}, op); err != nil {
		t.Fatal(err)
	}
	if st := invoiceStatus(ctx, t, svc.pool, tenantID, invID); st != "SETTLED" {
		t.Fatalf("补付后发票应为 SETTLED，实际 %s", st)
	}
	// 现在冲销退款 = 净核销 5000 + 2000 = 7000 > 票面 5000。
	_, err = svc.ReverseSupplierPaymentAllocation(ctx, tenantID, rview.Allocations[0].ID, "想撤退款", op)
	wantAllocErr(t, err, "PAY_REVERSE_EXCEEDS_INVOICE")
}

// 取消的采购单正是退订金的主场：预付 1000 → 单取消 → 厂里退 600 可以，
// 再退 600 超出净预付 400，框住。挂着退款时预付核销不许先冲。
func TestAdvanceRefundOnACancelledPO(t *testing.T) {
	ctx, svc, tenantID, cleanup := refundTestPool(t)
	defer cleanup()
	op := Operator{ID: 77, Name: "Finance"}

	var poID int64
	if err := svc.pool.QueryRow(ctx, `
		INSERT INTO purchase_orders
		  (tenant_id, po_no, supplier_id, supplier_name, currency, total_amount, status)
		VALUES ($1, 'PO-RF-1', 9, 'Mill', 'USD', 3000, 'ORDERED')
		RETURNING id`, tenantID).Scan(&poID); err != nil {
		t.Fatal(err)
	}

	advance := pay(ctx, t, svc, tenantID, "ADVANCE", "1000", op)
	aview, err := svc.AllocateSupplierPayment(ctx, tenantID, advance.ID,
		[]PaymentAllocationInput{{POID: poID, Amount: "1000"}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx, `
		UPDATE purchase_orders SET status='CANCELLED' WHERE tenant_id=$1 AND id=$2`,
		tenantID, poID); err != nil {
		t.Fatal(err)
	}

	refund := pay(ctx, t, svc, tenantID, "REFUND", "600", op)
	if _, err := svc.AllocateSupplierPayment(ctx, tenantID, refund.ID,
		[]PaymentAllocationInput{{POID: poID, Amount: "600"}}, op); err != nil {
		t.Fatalf("取消的采购单应能收预付款退款：%v", err)
	}
	refund2 := pay(ctx, t, svc, tenantID, "REFUND", "600", op)
	_, err = svc.AllocateSupplierPayment(ctx, tenantID, refund2.ID,
		[]PaymentAllocationInput{{POID: poID, Amount: "600"}}, op)
	wantAllocErr(t, err, "PAY_ALLOC_REFUND_EXCEEDS_ADVANCE")

	_, err = svc.ReverseSupplierPaymentAllocation(ctx, tenantID, aview.Allocations[0].ID, "想撤预付", op)
	wantAllocErr(t, err, "PAY_REVERSE_REFUND_FIRST")
}
