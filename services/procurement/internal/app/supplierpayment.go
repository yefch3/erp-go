package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// Payments and where they went.
//
// A payment is our assertion that money left; an allocation is the judgement
// about what it settled. The two are kept apart on purpose — the assertion
// is made once, the judgement gets revised — which is the same split the
// receivables side has already proven out in export's receipt.go, down to
// reversals being negative rows rather than deletions.
//
// The one structural difference from receivables: an allocation's target is
// either an invoice (settlement) or a purchase order (deposit), exactly one.
// A 30% deposit leaves before any invoice exists; a model that can only
// settle invoices cannot represent the most common payment in this trade.

var paymentMethods = map[string]bool{
	"WIRE": true, "LC": true, "TT": true, "CASH": true, "OTHER": true,
}
var paymentTypes = map[string]bool{
	"ADVANCE": true, "SETTLEMENT": true, "REFUND": true,
}

// SupplierPaymentInput is the assertion as entered.
type SupplierPaymentInput struct {
	SupplierID   int64
	SupplierName string
	PaymentType  string
	Currency     string
	Amount       string
	PaidAt       string
	Method       string
	BankRef      string
	Remark       string
}

// PaymentAllocationInput is one judgement: this much of the payment went to
// that document.
type PaymentAllocationInput struct {
	InvoiceID int64
	POID      int64
	Amount    string
	FeeAmount string
}

// PaymentAllocationView is one stored judgement with its target resolved for
// display.
type PaymentAllocationView struct {
	ID            int64
	InvoiceID     int64
	InvoiceNo     string
	POID          int64
	PONo          string
	Amount        string
	FeeAmount     string
	Currency      string
	ReversalOf    int64
	ReverseReason string
	AllocatedBy   string
	AllocatedAt   string
}

// SupplierPayment is the stored assertion plus its judgements.
type SupplierPayment struct {
	ID           int64
	SupplierID   int64
	SupplierName string
	PaymentNo    string
	PaymentType  string
	Currency     string
	Amount       string
	// Amount minus every live allocation and fee. A deposit sits here in
	// full until the invoice arrives and it is re-pointed.
	Unallocated string
	PaidAt      string
	Method      string
	BankRef     string
	Remark      string
	CreatedBy   string
	CreatedAt   string
	// Book-currency snapshot from entry time (P6); zero = not captured.
	BaseCurrency string
	BaseAmount   string
	FxRate       string
	Allocations  []PaymentAllocationView
}

// SupplierPaymentFilter narrows the list.
type SupplierPaymentFilter struct {
	SupplierID  int64
	PaymentType string
	Keyword     string
}

// AuthorizeSupplierPayment fences by whoever entered the payment, under the
// same module as orders and invoices: one spend chain, one knob.
func (s *Service) AuthorizeSupplierPayment(ctx context.Context, tenantID, paymentID int64, op Operator) error {
	var createdBy int64
	err := s.pool.QueryRow(ctx,
		`SELECT created_by_id FROM supplier_payments WHERE tenant_id=$1 AND id=$2`,
		tenantID, paymentID).Scan(&createdBy)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("PAY_NOT_FOUND", "付款单不存在")
	}
	if err != nil {
		return err
	}
	visible, err := s.visibleOrdersTo(ctx, op)
	if err != nil {
		return err
	}
	if !ownerVisible(visible, createdBy) {
		return apierr.NotFound("PAY_NOT_FOUND", "付款单不存在")
	}
	return nil
}

// CreateSupplierPayment records that money left.
//
// No allocation happens here. Recording the fact and deciding what it
// settled are different moments — often literally: the deposit is wired
// today, the invoice it will eventually settle does not exist yet.
func (s *Service) CreateSupplierPayment(ctx context.Context, tenantID int64, in SupplierPaymentInput, op Operator) (SupplierPayment, error) {
	if in.SupplierID <= 0 || strings.TrimSpace(in.SupplierName) == "" {
		return SupplierPayment{}, apierr.Invalid("PAY_SUPPLIER_REQUIRED", "请选择供应商")
	}
	ptype := strings.TrimSpace(in.PaymentType)
	if ptype == "" {
		ptype = "SETTLEMENT"
	}
	if !paymentTypes[ptype] {
		return SupplierPayment{}, apierr.Invalid("PAY_TYPE_INVALID", "付款类型无效")
	}
	method := strings.TrimSpace(in.Method)
	if method == "" {
		method = "WIRE"
	}
	if !paymentMethods[method] {
		return SupplierPayment{}, apierr.Invalid("PAY_METHOD_INVALID", "付款方式无效")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" || len(currency) > 8 {
		return SupplierPayment{}, apierr.Invalid("PAY_CURRENCY_REQUIRED", "请填写币种")
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(in.Amount))
	if err != nil || !amount.IsPositive() {
		return SupplierPayment{}, apierr.Invalid("PAY_AMOUNT_INVALID", "付款金额必须大于零")
	}
	if _, err := time.Parse("2006-01-02", in.PaidAt); err != nil {
		return SupplierPayment{}, apierr.Invalid("PAY_DATE_INVALID", "付款日期格式应为 YYYY-MM-DD")
	}

	no, err := s.paymentNo(ctx)
	if err != nil {
		return SupplierPayment{}, err
	}
	// The fx snapshot rides in the same INSERT: "what was this worth in book
	// currency the day it left" only exists if written down then (P6).
	fxRate, baseAmount := s.fxSnapshot(ctx, currency, amount)
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO supplier_payments
		  (tenant_id, supplier_id, supplier_name, payment_no, payment_type,
		   currency, amount, paid_at, method, bank_ref, remark,
		   created_by_id, created_by_name,
		   base_currency, base_amount, fx_rate)
		VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8::date,$9,$10,$11,$12,$13,
		        $14,$15::numeric,$16::numeric)
		RETURNING id`,
		tenantID, in.SupplierID, strings.TrimSpace(in.SupplierName), no, ptype,
		currency, amount.String(), in.PaidAt, method,
		strings.TrimSpace(in.BankRef), strings.TrimSpace(in.Remark),
		op.ID, op.Name,
		s.bookCurrency(), baseAmount.String(), fxRate.String(),
	).Scan(&id)
	if err != nil {
		return SupplierPayment{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetSupplierPayment(ctx, tenantID, id)
}

// paymentNo numbers the slip. The fallback exists for isolated tests, which
// construct the service without the numbering port — the same dispensation
// the scope resolver grants, and no production path reaches it because main
// always wires numbering.
func (s *Service) paymentNo(ctx context.Context) (string, error) {
	if s.numbering == nil {
		return "PAY-" + strconv.FormatInt(time.Now().UnixNano(), 36), nil
	}
	return s.numbering.Next(ctx, "SUPPLIER_PAYMENT")
}

// ListSupplierPayments pages the assertions, fenced like everything else on
// the spend chain.
func (s *Service) ListSupplierPayments(ctx context.Context, tenantID int64, f SupplierPaymentFilter, page, size int32, operators ...Operator) ([]SupplierPayment, int64, error) {
	var op Operator
	if len(operators) > 0 {
		op = operators[0]
	}
	visible, err := s.visibleOrdersTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	page, size = normalizePage(page, size)
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.supplier_id, p.supplier_name, p.payment_no, p.payment_type,
		       p.currency, p.amount::text,
		       (p.amount - coalesce((SELECT sum(a.amount + a.fee_amount)
		          FROM payment_allocations a
		         WHERE a.tenant_id = p.tenant_id AND a.payment_id = p.id), 0))::text,
		       p.paid_at::text, p.method, p.bank_ref, p.remark,
		       p.created_by_name, p.created_at::text,
		       p.base_currency, p.base_amount::text, p.fx_rate::text,
		       count(*) OVER () AS total
		  FROM supplier_payments p
		 WHERE p.tenant_id = $1
		   AND ($2 = 0 OR p.supplier_id = $2)
		   AND ($3 = '' OR p.payment_type = $3)
		   AND ($4 = '' OR p.payment_no ILIKE '%'||$4||'%' OR p.supplier_name ILIKE '%'||$4||'%')
		   AND ($5::bool OR p.created_by_id = ANY($6::bigint[]))
		 ORDER BY p.paid_at DESC, p.id DESC
		 LIMIT $7 OFFSET $8`,
		tenantID, f.SupplierID, strings.TrimSpace(f.PaymentType),
		strings.TrimSpace(f.Keyword), visible.All, visible.EmployeeIDs,
		size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SupplierPayment
	var total int64
	for rows.Next() {
		var v SupplierPayment
		if err := rows.Scan(&v.ID, &v.SupplierID, &v.SupplierName, &v.PaymentNo,
			&v.PaymentType, &v.Currency, &v.Amount, &v.Unallocated,
			&v.PaidAt, &v.Method, &v.BankRef, &v.Remark,
			&v.CreatedBy, &v.CreatedAt,
			&v.BaseCurrency, &v.BaseAmount, &v.FxRate, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// GetSupplierPayment reads one assertion with all its judgements, reversals
// included — the history of how the balance got here is the point.
func (s *Service) GetSupplierPayment(ctx context.Context, tenantID, id int64) (SupplierPayment, error) {
	var v SupplierPayment
	err := s.pool.QueryRow(ctx, `
		SELECT p.id, p.supplier_id, p.supplier_name, p.payment_no, p.payment_type,
		       p.currency, p.amount::text,
		       (p.amount - coalesce((SELECT sum(a.amount + a.fee_amount)
		          FROM payment_allocations a
		         WHERE a.tenant_id = p.tenant_id AND a.payment_id = p.id), 0))::text,
		       p.paid_at::text, p.method, p.bank_ref, p.remark,
		       p.created_by_name, p.created_at::text,
		       p.base_currency, p.base_amount::text, p.fx_rate::text
		  FROM supplier_payments p WHERE p.tenant_id=$1 AND p.id=$2`, tenantID, id,
	).Scan(&v.ID, &v.SupplierID, &v.SupplierName, &v.PaymentNo, &v.PaymentType,
		&v.Currency, &v.Amount, &v.Unallocated, &v.PaidAt, &v.Method,
		&v.BankRef, &v.Remark, &v.CreatedBy, &v.CreatedAt,
		&v.BaseCurrency, &v.BaseAmount, &v.FxRate)
	if err == pgx.ErrNoRows {
		return SupplierPayment{}, apierr.NotFound("PAY_NOT_FOUND", "付款单不存在")
	}
	if err != nil {
		return SupplierPayment{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, coalesce(a.invoice_id,0), coalesce(i.invoice_no,''),
		       coalesce(a.po_id,0), coalesce(o.po_no,''),
		       a.amount::text, a.fee_amount::text, a.currency,
		       coalesce(a.reversal_of,0), a.reverse_reason,
		       a.allocated_by_name, a.allocated_at::text
		  FROM payment_allocations a
		  LEFT JOIN supplier_invoices i ON i.id = a.invoice_id
		  LEFT JOIN purchase_orders o ON o.id = a.po_id
		 WHERE a.tenant_id=$1 AND a.payment_id=$2
		 ORDER BY a.id`, tenantID, id)
	if err != nil {
		return SupplierPayment{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var a PaymentAllocationView
		if err := rows.Scan(&a.ID, &a.InvoiceID, &a.InvoiceNo, &a.POID, &a.PONo,
			&a.Amount, &a.FeeAmount, &a.Currency, &a.ReversalOf, &a.ReverseReason,
			&a.AllocatedBy, &a.AllocatedAt); err != nil {
			return SupplierPayment{}, err
		}
		v.Allocations = append(v.Allocations, a)
	}
	return v, rows.Err()
}

// AllocateSupplierPayment decides what a payment settled.
//
// The payment row is never touched: amount stays what was asserted, and this
// only appends rows saying where it went. Balance is read inside the lock —
// two people allocating the same payment at once must serialize, or the sum
// of their optimism exceeds the money.
func (s *Service) AllocateSupplierPayment(ctx context.Context, tenantID, paymentID int64, lines []PaymentAllocationInput, op Operator) (SupplierPayment, error) {
	if len(lines) == 0 {
		return SupplierPayment{}, apierr.Invalid("PAY_ALLOC_EMPTY", "请至少分配一笔")
	}
	if err := s.AuthorizeSupplierPayment(ctx, tenantID, paymentID, op); err != nil {
		return SupplierPayment{}, err
	}

	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var supplierID int64
		var currency string
		var amount decimal.Decimal
		var amountText string
		err := tx.QueryRow(ctx, `
			SELECT supplier_id, currency, amount::text FROM supplier_payments
			 WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
			tenantID, paymentID).Scan(&supplierID, &currency, &amountText)
		if err != nil {
			return err
		}
		amount = decimal.RequireFromString(amountText)

		var allocatedText string
		if err := tx.QueryRow(ctx, `
			SELECT coalesce(sum(amount + fee_amount),0)::text FROM payment_allocations
			 WHERE tenant_id=$1 AND payment_id=$2`,
			tenantID, paymentID).Scan(&allocatedText); err != nil {
			return err
		}
		remaining := amount.Sub(decimal.RequireFromString(allocatedText))

		adding := decimal.Zero
		touched := map[int64]bool{}
		for i, l := range lines {
			lineNo := itoa(i + 1)
			if (l.InvoiceID != 0) == (l.POID != 0) {
				return apierr.Invalid("PAY_ALLOC_TARGET_INVALID",
					"第 "+lineNo+" 笔必须指定发票或采购单中的恰好一个")
			}
			amt, err := decimal.NewFromString(strings.TrimSpace(l.Amount))
			if err != nil || !amt.IsPositive() {
				return apierr.Invalid("PAY_ALLOC_AMOUNT_INVALID", "第 "+lineNo+" 笔核销金额必须大于零")
			}
			fee, err := decimal.NewFromString(orZero(strings.TrimSpace(l.FeeAmount)))
			if err != nil || fee.IsNegative() {
				return apierr.Invalid("PAY_ALLOC_FEE_INVALID", "第 "+lineNo+" 笔手续费不能为负数")
			}

			if l.InvoiceID != 0 {
				var invSupplier int64
				var invCurrency, invStatus string
				err := tx.QueryRow(ctx, `
					SELECT supplier_id, currency, status FROM supplier_invoices
					 WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
					tenantID, l.InvoiceID).Scan(&invSupplier, &invCurrency, &invStatus)
				if err != nil {
					return apierr.NotFound("PAY_ALLOC_INVOICE_NOT_FOUND", "第 "+lineNo+" 笔的发票不存在")
				}
				if invStatus == "VOID" {
					return apierr.Invalid("PAY_ALLOC_INVOICE_VOID", "第 "+lineNo+" 笔的发票已作废")
				}
				if invSupplier != supplierID {
					return apierr.Invalid("PAY_ALLOC_SUPPLIER_MISMATCH", "第 "+lineNo+" 笔的发票不属于该供应商")
				}
				if invCurrency != currency {
					// Cross-currency settlement produces an exchange gain or
					// loss this stage has nowhere to put. Refused rather than
					// swallowed; P6 is where the gap gets a home.
					return apierr.Invalid("PAY_ALLOC_CURRENCY_MISMATCH",
						"第 "+lineNo+" 笔币种不符：发票 "+invCurrency+"，付款 "+currency)
				}
				touched[l.InvoiceID] = true
			} else {
				var poSupplier int64
				var poCurrency, poStatus string
				err := tx.QueryRow(ctx, `
					SELECT supplier_id, currency, status FROM purchase_orders
					 WHERE tenant_id=$1 AND id=$2`,
					tenantID, l.POID).Scan(&poSupplier, &poCurrency, &poStatus)
				if err != nil {
					return apierr.NotFound("PAY_ALLOC_PO_NOT_FOUND", "第 "+lineNo+" 笔的采购单不存在")
				}
				if poStatus == "CANCELLED" {
					return apierr.Invalid("PAY_ALLOC_PO_CANCELLED", "第 "+lineNo+" 笔的采购单已取消")
				}
				if poSupplier != supplierID {
					return apierr.Invalid("PAY_ALLOC_SUPPLIER_MISMATCH", "第 "+lineNo+" 笔的采购单不属于该供应商")
				}
				if poCurrency != currency {
					return apierr.Invalid("PAY_ALLOC_CURRENCY_MISMATCH",
						"第 "+lineNo+" 笔币种不符：采购单 "+poCurrency+"，付款 "+currency)
				}
			}
			adding = adding.Add(amt).Add(fee)
		}
		if adding.GreaterThan(remaining) {
			return apierr.Invalid("PAY_ALLOC_EXCEEDS_PAYMENT",
				"本次分配 "+adding.String()+" 超出付款单未分配余额 "+remaining.String())
		}

		for _, l := range lines {
			amt, _ := decimal.NewFromString(strings.TrimSpace(l.Amount))
			fee, _ := decimal.NewFromString(orZero(strings.TrimSpace(l.FeeAmount)))
			if _, err := tx.Exec(ctx, `
				INSERT INTO payment_allocations
				  (tenant_id, payment_id, invoice_id, po_id, amount, fee_amount,
				   currency, allocated_by, allocated_by_name)
				VALUES ($1,$2,nullif($3,0),nullif($4,0),$5::numeric,$6::numeric,$7,$8,$9)`,
				tenantID, paymentID, l.InvoiceID, l.POID,
				amt.String(), fee.String(), currency, op.ID, op.Name); err != nil {
				return err
			}
		}
		for invoiceID := range touched {
			if err := recomputeInvoiceSettlement(ctx, tx, tenantID, invoiceID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return SupplierPayment{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetSupplierPayment(ctx, tenantID, paymentID)
}

// ReverseSupplierPaymentAllocation undoes one judgement by writing its
// opposite. Not a DELETE and not an UPDATE — the receivables discipline.
func (s *Service) ReverseSupplierPaymentAllocation(ctx context.Context, tenantID, allocID int64, reason string, op Operator) (SupplierPayment, error) {
	if strings.TrimSpace(reason) == "" {
		return SupplierPayment{}, apierr.Invalid("PAY_REVERSE_REASON_REQUIRED", "请填写冲销原因")
	}
	var paymentID int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var invoiceID, poID, reversalOf int64
		var amountText, feeText, currency string
		err := tx.QueryRow(ctx, `
			SELECT payment_id, coalesce(invoice_id,0), coalesce(po_id,0),
			       amount::text, fee_amount::text, currency, coalesce(reversal_of,0)
			  FROM payment_allocations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
			tenantID, allocID).Scan(&paymentID, &invoiceID, &poID,
			&amountText, &feeText, &currency, &reversalOf)
		if err == pgx.ErrNoRows {
			return apierr.NotFound("PAY_ALLOC_NOT_FOUND", "核销记录不存在")
		}
		if err != nil {
			return err
		}
		if reversalOf != 0 {
			return apierr.Invalid("PAY_ALLOC_IS_REVERSAL", "冲销记录本身不能再被冲销")
		}
		if err := s.AuthorizeSupplierPayment(ctx, tenantID, paymentID, op); err != nil {
			return err
		}
		amt := decimal.RequireFromString(amountText).Neg()
		fee := decimal.RequireFromString(feeText).Neg()
		if _, err := tx.Exec(ctx, `
			INSERT INTO payment_allocations
			  (tenant_id, payment_id, invoice_id, po_id, amount, fee_amount,
			   currency, reversal_of, reverse_reason, allocated_by, allocated_by_name)
			VALUES ($1,$2,nullif($3,0),nullif($4,0),$5::numeric,$6::numeric,$7,$8,$9,$10,$11)`,
			tenantID, paymentID, invoiceID, poID, amt.String(), fee.String(),
			currency, allocID, strings.TrimSpace(reason), op.ID, op.Name); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apierr.Conflict("PAY_ALLOC_ALREADY_REVERSED", "这笔核销已经冲销过")
			}
			return err
		}
		if invoiceID != 0 {
			return recomputeInvoiceSettlement(ctx, tx, tenantID, invoiceID)
		}
		return nil
	})
	if err != nil {
		return SupplierPayment{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetSupplierPayment(ctx, tenantID, paymentID)
}

// recomputeInvoiceSettlement moves an invoice between OPEN and SETTLED from
// the sum of its live allocations.
//
// Derived, never stored independently: the allocation rows are the single
// source of truth, and a status that could drift from their sum would be a
// second set of books.
func recomputeInvoiceSettlement(ctx context.Context, tx pgx.Tx, tenantID, invoiceID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE supplier_invoices i
		   SET status = CASE
		         WHEN coalesce((SELECT sum(a.amount) FROM payment_allocations a
		                WHERE a.tenant_id = i.tenant_id AND a.invoice_id = i.id), 0)
		              >= i.total_amount
		         THEN 'SETTLED' ELSE 'OPEN' END
		 WHERE tenant_id = $1 AND id = $2 AND status <> 'VOID'`,
		tenantID, invoiceID)
	return err
}
