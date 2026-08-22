package app

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// The matcher: the judgement CreateSupplierInvoice deliberately refused to
// make.
//
// Recording accepts the factory's claim verbatim; this compares it against
// the other two legs — our order and our receipt — and writes a verdict. The
// arithmetic never uses the invoice's own unit price: judging a claim by the
// numbers inside the claim is using the suspect's testimony as evidence.
//
//	payable qty    = received qty − Σ unresolved goods exceptions
//	payable amount = payable qty × OUR order unit price
//	difference     = claimed line amount − payable amount
//
// A verdict of EXCEPTION blocks nothing. "The factory added 2% because
// freight went up" is an everyday event the buyer decides on; the system's
// job is to say exactly which line differs, by how much, and why it thinks
// so — and then get out of the way. Same stance as Odoo's Should-be-paid
// flag, and the same lesson the mail-tracking work taught three times over:
// an honest status beats a confident guess.

// goodsExceptionTypes are the receipt exceptions that reduce what a delivery
// is worth paying for. WRONG_PRODUCT and UNIT_MISMATCH are deliberately
// absent: they mean the receipt itself is in doubt, and a number in doubt
// should surface as a difference, not silently shrink the payable side.
const goodsExceptionTypes = `('SHORT_SHIPMENT','DAMAGE','QUALITY_DISPUTE','RETURN')`

// UseMatchTolerance widens what still counts as MATCHED.
//
// The tolerance for a line is min(payable × pct, abs) over whichever of the
// two are positive — the ERPNext convention. Zero (the default) means
// to-the-cent: a pilot should first find out how often reality differs
// before deciding how much difference to stop looking at.
func (s *Service) UseMatchTolerance(pct, abs decimal.Decimal) {
	s.matchTolPct, s.matchTolAbs = pct, abs
}

func (s *Service) matchTolerance(base decimal.Decimal) decimal.Decimal {
	tol := decimal.Zero
	if s.matchTolPct.IsPositive() {
		tol = base.Mul(s.matchTolPct)
	}
	if s.matchTolAbs.IsPositive() && (tol.IsZero() || s.matchTolAbs.LessThan(tol)) {
		tol = s.matchTolAbs
	}
	return tol
}

// MatchSupplierInvoice re-runs the verdict on demand — after a receipt lands,
// an exception is resolved, or somebody simply wants to check again.
func (s *Service) MatchSupplierInvoice(ctx context.Context, tenantID, invoiceID int64, op Operator) (SupplierInvoice, error) {
	if err := s.AuthorizeSupplierInvoice(ctx, tenantID, invoiceID, op); err != nil {
		return SupplierInvoice{}, err
	}
	if err := s.matchInvoice(ctx, tenantID, invoiceID); err != nil {
		return SupplierInvoice{}, err
	}
	return s.GetSupplierInvoice(ctx, tenantID, invoiceID)
}

// matchInvoice computes and stores the verdict. Internal: authorization is
// the caller's business, and CreateSupplierInvoice calls this on its own
// output where no second check is owed.
func (s *Service) matchInvoice(ctx context.Context, tenantID, invoiceID int64) error {
	inv, err := s.GetSupplierInvoice(ctx, tenantID, invoiceID)
	if err != nil {
		return err
	}
	if inv.Status != "OPEN" {
		return apierr.Conflict("INV_NOT_OPEN", "只有待处理的发票才能核对")
	}

	var problems []string
	if len(inv.Lines) == 0 {
		problems = append(problems, "发票没有明细，无法与订单核对；付款前需人工确认")
	}
	for i, l := range inv.Lines {
		lineNo := itoa(i + 1)
		claimed, err := decimal.NewFromString(l.Amount)
		if err != nil {
			return err
		}
		if l.POItemID == 0 {
			problems = append(problems, "第 "+lineNo+" 行是订单外费用 "+claimed.String()+"（"+firstNonEmpty(l.Description, "无摘要")+"），无采购单可核")
			continue
		}

		var unitPrice, receivedQty, excQty, othersBilled string
		err = s.pool.QueryRow(ctx, `
			SELECT i.unit_price::text, i.received_qty::text,
			       (SELECT coalesce(sum(e.qty),0)::text
			          FROM purchase_receipt_exceptions e
			         WHERE e.tenant_id = i.tenant_id AND e.po_item_id = i.id
			           AND e.status = 'OPEN'
			           AND e.exception_type IN `+goodsExceptionTypes+`),
			       (SELECT coalesce(sum(l2.amount),0)::text
			          FROM supplier_invoice_lines l2
			          JOIN supplier_invoices i2 ON i2.id = l2.invoice_id
			         WHERE l2.tenant_id = i.tenant_id AND l2.po_item_id = i.id
			           AND i2.status <> 'VOID' AND i2.id <> $3)
			  FROM purchase_order_items i
			 WHERE i.tenant_id = $1 AND i.id = $2`,
			tenantID, l.POItemID, invoiceID,
		).Scan(&unitPrice, &receivedQty, &excQty, &othersBilled)
		if err != nil {
			return err
		}
		price := decimal.RequireFromString(unitPrice)
		received := decimal.RequireFromString(receivedQty)
		exc := decimal.RequireFromString(excQty)
		others := decimal.RequireFromString(othersBilled)

		payableQty := received.Sub(exc)
		if payableQty.IsNegative() {
			payableQty = decimal.Zero
		}
		payable := payableQty.Mul(price)
		tol := s.matchTolerance(payable)

		if diff := claimed.Sub(payable); diff.Abs().GreaterThan(tol) {
			problems = append(problems,
				"第 "+lineNo+" 行应付 "+payable.String()+
					"（已收 "+received.String()+" − 异常 "+exc.String()+" = "+payableQty.String()+
					" × 订单单价 "+price.String()+"），发票开 "+claimed.String()+
					"，差 "+diff.String())
		}
		// The classic double-billing hole: each paper within bounds, the sum
		// over. Judged against the same payable, counting every live invoice.
		if cum := others.Add(claimed); cum.GreaterThan(payable.Add(tol)) && others.IsPositive() {
			problems = append(problems,
				"第 "+lineNo+" 行累计开票 "+cum.String()+" 超出应付 "+payable.String()+
					"（其他未作废发票已开 "+others.String()+"）")
		}
	}

	status, note := "MATCHED", "三单一致：明细逐行对上应付金额"
	if len(problems) > 0 {
		status, note = "EXCEPTION", strings.Join(problems, "；")
		// The column is TEXT, but a note nobody can read to the end has
		// stopped being a note.
		if len(note) > 2000 {
			note = note[:2000] + "…"
		}
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE supplier_invoices SET match_status = $3, match_note = $4
		 WHERE tenant_id = $1 AND id = $2 AND status = 'OPEN'`,
		tenantID, invoiceID, status, note)
	if err == nil {
		s.nudge(ctx, tenantID)
	}
	return err
}

func firstNonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
