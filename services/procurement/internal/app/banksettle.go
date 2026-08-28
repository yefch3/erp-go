package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 付款对账的直接核销：一条归属=供应商的银行流水，当场说清它结算/退了
// 哪些发票或采购单。
//
// 核销记录必须挂在一张付款单上（payment_allocations.payment_id 非空外键，
// 对账五组数字、发票结清判定、汇率快照全部经由付款单），所以这里在同一个
// 事务里自动建一张**影子付款单**：source='BANK'，号段、汇率快照、全部
// 核销守门与手工单完全相同——然后走 allocateLinesTx，一道闸不少。
//
// 与「匹配付款单」互斥：匹配是「这张已有的单就是这笔钱」，直核是「这笔钱
// 直接说清去向」。两条路都把认领写在 claimed_amount 上，一行流水只能走
// 一条。supplier_payments.bank_txn_id 的部分唯一索引是并发时的最后兜底。

// SettleBankTransactionToSupplier 直核一条银行流水。
func (s *Service) SettleBankTransactionToSupplier(ctx context.Context, tenantID, txnID int64, lines []PaymentAllocationInput, op Operator) (SupplierPayment, error) {
	if len(lines) == 0 {
		return SupplierPayment{}, apierr.Invalid("PAY_ALLOC_EMPTY", "请至少分配一笔")
	}
	var paymentID int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// 银行行加锁读：认领判定和写入必须在同一个事务里，否则两个人同时
		// 直核一行，各自都以为这行还没人认。
		var direction, amountText, currency, txnDate, bankRef, ownership, claimedText string
		err := tx.QueryRow(ctx, `
			SELECT direction, amount::text, currency, txn_date::text, bank_ref,
			       ownership, claimed_amount::text
			  FROM bank_transactions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
			tenantID, txnID).Scan(&direction, &amountText, &currency, &txnDate,
			&bankRef, &ownership, &claimedText)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
		}
		if err != nil {
			return err
		}
		// 归属的闸和匹配那条路同一句话：写着别人的不能核，空着的可以——
		// 直核这个动作本身就说明它是供应商那条线上的，下面顺手补上归属。
		if ownership != "" && ownership != OwnershipSupplier {
			return apierr.Invalid("BANK_TXN_OWNERSHIP",
				"这条流水的归属不是「供应商」，不能在付款对账核销。要改先在归属那一列改。")
		}
		if claimed, ok := normalizeBankAmount(claimedText); ok && claimed.IsPositive() {
			return apierr.Conflict("BANK_SETTLE_ALREADY_CLAIMED",
				"这条流水已经被认领（已认 "+claimed.String()+"）——"+
					"匹配过付款单的先取消匹配，直核过的先在付款单上冲销")
		}

		// 影子单的类型由方向和去向定：出账给发票是结算、全给采购单是预付；
		// 进账只能是退款（负行那套守门在 allocateLinesTx 里等着它）。
		ptype := "SETTLEMENT"
		if direction == "CREDIT" {
			ptype = "REFUND"
		} else {
			allPO := true
			for _, l := range lines {
				if l.POID == 0 {
					allPO = false
					break
				}
			}
			if allPO {
				ptype = "ADVANCE"
			}
		}

		// 供应商从第一笔去向上取；后面每一笔和付款单供应商的一致性由
		// allocateLinesTx 里现成的 SUPPLIER_MISMATCH 闸把住。
		var supplierID int64
		var supplierName string
		switch first := lines[0]; {
		case first.InvoiceID != 0:
			err = tx.QueryRow(ctx, `
				SELECT supplier_id, supplier_name FROM supplier_invoices
				 WHERE tenant_id=$1 AND id=$2`,
				tenantID, first.InvoiceID).Scan(&supplierID, &supplierName)
		case first.POID != 0:
			err = tx.QueryRow(ctx, `
				SELECT supplier_id, supplier_name FROM purchase_orders
				 WHERE tenant_id=$1 AND id=$2`,
				tenantID, first.POID).Scan(&supplierID, &supplierName)
		default:
			return apierr.Invalid("PAY_ALLOC_TARGET_INVALID",
				"第 1 笔必须指定发票或采购单中的恰好一个")
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("PAY_ALLOC_TARGET_INVALID", "第 1 笔的发票/采购单不存在")
		}
		if err != nil {
			return err
		}

		no, err := s.paymentNo(ctx)
		if err != nil {
			return err
		}
		amount := decimal.RequireFromString(amountText)
		// 汇率快照取建单当日牌价——和手工建单同一个函数、同一个精度；
		// 没采到就是 0，诚实地坐在汇兑损益外（P6 的规矩）。
		fxRate, baseAmount := s.fxSnapshot(ctx, currency, amount)
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_payments
			  (tenant_id, supplier_id, supplier_name, payment_no, payment_type,
			   currency, amount, paid_at, method, bank_ref, remark,
			   created_by_id, created_by_name,
			   base_currency, base_amount, fx_rate, bank_txn_id, source)
			VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8::date,'WIRE',$9,'',
			        $10,$11,$12,$13::numeric,$14::numeric,$15,'BANK')
			RETURNING id`,
			tenantID, supplierID, supplierName, no, ptype,
			currency, amount.String(), txnDate, bankRef,
			op.ID, op.Name,
			s.bookCurrency(), baseAmount.String(), fxRate.String(), txnID,
		).Scan(&paymentID); err != nil {
			return err
		}

		if err := allocateLinesTx(ctx, tx, tenantID, paymentID, lines, op); err != nil {
			return err
		}

		// 认领记全额，和匹配同一条规矩：金额可以对不上（中间行扣手续费），
		// 但认领这件事没有一半——claimed_amount < amount 就是还没处理完，
		// 客户和供应商两条线同一个定义。归属空着的顺手补上。
		if _, err := tx.Exec(ctx, `
			UPDATE bank_transactions
			   SET ownership = CASE WHEN ownership='' THEN $3 ELSE ownership END,
			       claimed_amount = amount
			 WHERE tenant_id=$1 AND id=$2`,
			tenantID, txnID, OwnershipSupplier); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return SupplierPayment{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetSupplierPayment(ctx, tenantID, paymentID)
}
