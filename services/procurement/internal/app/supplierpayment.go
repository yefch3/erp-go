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
	// MANUAL 手工登记 / BANK 付款对账直核时系统自动建的影子单。
	// 只是让财务认出「这张单是系统替你写的」，号段和守门与手工单相同。
	Source      string
	Allocations []PaymentAllocationView
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
		       -- 退款单的核销行是负数，覆盖量要先翻号再从金额里减
		       -- （SQL 侧的 coveredOfPayment）。
		       (p.amount - coalesce((SELECT sum(a.amount + a.fee_amount)
		          FROM payment_allocations a
		         WHERE a.tenant_id = p.tenant_id AND a.payment_id = p.id), 0)
		          * (CASE WHEN p.payment_type = 'REFUND' THEN -1 ELSE 1 END))::text,
		       p.paid_at::text, p.method, p.bank_ref, p.remark,
		       p.created_by_name, p.created_at::text,
		       p.base_currency, p.base_amount::text, p.fx_rate::text, p.source,
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
			&v.BaseCurrency, &v.BaseAmount, &v.FxRate, &v.Source, &total); err != nil {
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
		       -- 同 List：退款单的覆盖量先翻号再减（SQL 侧的 coveredOfPayment）。
		       (p.amount - coalesce((SELECT sum(a.amount + a.fee_amount)
		          FROM payment_allocations a
		         WHERE a.tenant_id = p.tenant_id AND a.payment_id = p.id), 0)
		          * (CASE WHEN p.payment_type = 'REFUND' THEN -1 ELSE 1 END))::text,
		       p.paid_at::text, p.method, p.bank_ref, p.remark,
		       p.created_by_name, p.created_at::text,
		       p.base_currency, p.base_amount::text, p.fx_rate::text, p.source
		  FROM supplier_payments p WHERE p.tenant_id=$1 AND p.id=$2`, tenantID, id,
	).Scan(&v.ID, &v.SupplierID, &v.SupplierName, &v.PaymentNo, &v.PaymentType,
		&v.Currency, &v.Amount, &v.Unallocated, &v.PaidAt, &v.Method,
		&v.BankRef, &v.Remark, &v.CreatedBy, &v.CreatedAt,
		&v.BaseCurrency, &v.BaseAmount, &v.FxRate, &v.Source)
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
		return allocateLinesTx(ctx, tx, tenantID, paymentID, lines, op)
	})
	if err != nil {
		return SupplierPayment{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetSupplierPayment(ctx, tenantID, paymentID)
}

// allocateLinesTx 是核销的事务体，抽出来给两个入口共用：付款单页的
// AllocateSupplierPayment，和付款对账里银行流水直核（建完影子单后同一个
// 事务里核销，见 banksettle.go）。守门一道不少、一处不多——两个入口
// 必须过同一批闸。
func allocateLinesTx(ctx context.Context, tx pgx.Tx, tenantID, paymentID int64, lines []PaymentAllocationInput, op Operator) error {
	var supplierID int64
	var currency, paymentType string
	var amount decimal.Decimal
	var amountText string
	err := tx.QueryRow(ctx, `
			SELECT supplier_id, currency, payment_type, amount::text FROM supplier_payments
			 WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		tenantID, paymentID).Scan(&supplierID, &currency, &paymentType, &amountText)
	if err != nil {
		return err
	}
	// 退款单的核销行落库为**负数**（界面照旧填正数，翻号只发生在写入
	// 那一刻）。于是每一处「已付 = 把核销金额加起来」的算式——对账五组
	// 数字、发票结清判定、付款单余额——都自动把退款算成冲减，一个求和
	// 不用改。这里所有守门在正数域里做：coveredOfPayment 是唯一的翻号
	// 读取点。出口侧 receipt.go 的 coveredOf 用同一套办法，先证明过。
	isRefund := paymentType == "REFUND"
	amount = decimal.RequireFromString(amountText)

	var allocatedText string
	if err := tx.QueryRow(ctx, `
			SELECT coalesce(sum(amount + fee_amount),0)::text FROM payment_allocations
			 WHERE tenant_id=$1 AND payment_id=$2`,
		tenantID, paymentID).Scan(&allocatedText); err != nil {
		return err
	}
	remaining := amount.Sub(coveredOfPayment(paymentType,
		decimal.RequireFromString(allocatedText)))

	adding := decimal.Zero
	touched := map[int64]bool{}
	// The payment-side balance is not the only ceiling: an invoice must
	// not be settled past its own total either. Per-invoice sums are read
	// once, under the invoice row lock every allocator takes, and amounts
	// from this very request accumulate against them — two lines naming
	// the same invoice are one claim, not two independent ones.
	settledByInvoice := map[int64]decimal.Decimal{}
	addingByInvoice := map[int64]decimal.Decimal{}
	// 退款的镜像天花板：一张采购单/发票上只能退**实际核销过的净额**。
	// 两个 map 都存正数量，方向由 isRefund 定。
	advanceByPO := map[int64]decimal.Decimal{}
	addingByPO := map[int64]decimal.Decimal{}
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
		// 手续费是「补足结算差额」用的，语义只在付出去的方向成立。
		// 退款行带手续费没有定义——银行扣了手续费导致退回来的钱变少，
		// 走认差/少退的路，不走这里。客户侧同款闸门：EX_REFUND_NO_FEE。
		if isRefund && !fee.IsZero() {
			return apierr.Invalid("PAY_ALLOC_REFUND_NO_FEE",
				"第 "+lineNo+" 笔是退款核销，不能带手续费")
		}

		if l.InvoiceID != 0 {
			var invSupplier int64
			var invCurrency, invStatus, invTotalText string
			err := tx.QueryRow(ctx, `
					SELECT supplier_id, currency, status, total_amount::text FROM supplier_invoices
					 WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
				tenantID, l.InvoiceID).Scan(&invSupplier, &invCurrency, &invStatus, &invTotalText)
			if err != nil {
				return apierr.NotFound("PAY_ALLOC_INVOICE_NOT_FOUND", "第 "+lineNo+" 笔的发票不存在")
			}
			if invStatus == "VOID" {
				return apierr.Invalid("PAY_ALLOC_INVOICE_VOID", "第 "+lineNo+" 笔的发票已作废")
			}
			// 已结清的发票拒绝再收钱，却必须能收退款——「付清之后厂里
			// 退了一部分」正是退款最常见的样子。负行落下去之后
			// recomputeInvoiceSettlement 会把它翻回 OPEN。
			if invStatus == "SETTLED" && !isRefund {
				return apierr.Invalid("PAY_ALLOC_INVOICE_SETTLED",
					"第 "+lineNo+" 笔的发票已结清——如需改动请先冲销一笔核销")
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
			settled, seen := settledByInvoice[l.InvoiceID]
			if !seen {
				var settledText string
				if err := tx.QueryRow(ctx, `
						SELECT coalesce(sum(amount),0)::text FROM payment_allocations
						 WHERE tenant_id=$1 AND invoice_id=$2`,
					tenantID, l.InvoiceID).Scan(&settledText); err != nil {
					return err
				}
				settled = decimal.RequireFromString(settledText)
				settledByInvoice[l.InvoiceID] = settled
			}
			if isRefund {
				// settled 是净额（先前的退款负行已经在里面），所以这条
				// 天花板天然框得住连续退两笔。
				refunding := addingByInvoice[l.InvoiceID].Add(amt)
				if refunding.GreaterThan(settled) {
					return apierr.Invalid("PAY_ALLOC_REFUND_EXCEEDS_SETTLED",
						"第 "+lineNo+" 笔退掉后该发票累计退 "+refunding.String()+
							" 超出已核销净额 "+settled.String()+"——只能退实际核销过的钱")
				}
			} else {
				invTotal := decimal.RequireFromString(invTotalText)
				newSum := settled.Add(addingByInvoice[l.InvoiceID]).Add(amt)
				if newSum.GreaterThan(invTotal) {
					return apierr.Invalid("PAY_ALLOC_EXCEEDS_INVOICE",
						"第 "+lineNo+" 笔核销后该发票累计核销 "+newSum.String()+" 超出发票金额 "+invTotal.String())
				}
			}
			addingByInvoice[l.InvoiceID] = addingByInvoice[l.InvoiceID].Add(amt)
			touched[l.InvoiceID] = true
		} else {
			// FOR UPDATE 和发票分支同理：退款的天花板（净预付款）在行锁
			// 下读，两笔并发退款必须串行，否则各自都以为额度够。
			var poSupplier int64
			var poCurrency, poStatus string
			err := tx.QueryRow(ctx, `
					SELECT supplier_id, currency, status FROM purchase_orders
					 WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
				tenantID, l.POID).Scan(&poSupplier, &poCurrency, &poStatus)
			if err != nil {
				return apierr.NotFound("PAY_ALLOC_PO_NOT_FOUND", "第 "+lineNo+" 笔的采购单不存在")
			}
			// 取消的采购单拒收新预付款，却必须能收退款——「订金付了、
			// 单取消了、厂里退钱」正是预付款退款的主场。
			if poStatus == "CANCELLED" && !isRefund {
				return apierr.Invalid("PAY_ALLOC_PO_CANCELLED", "第 "+lineNo+" 笔的采购单已取消")
			}
			if poSupplier != supplierID {
				return apierr.Invalid("PAY_ALLOC_SUPPLIER_MISMATCH", "第 "+lineNo+" 笔的采购单不属于该供应商")
			}
			if poCurrency != currency {
				return apierr.Invalid("PAY_ALLOC_CURRENCY_MISMATCH",
					"第 "+lineNo+" 笔币种不符：采购单 "+poCurrency+"，付款 "+currency)
			}
			if isRefund {
				advance, seen := advanceByPO[l.POID]
				if !seen {
					var advText string
					if err := tx.QueryRow(ctx, `
							SELECT coalesce(sum(amount),0)::text FROM payment_allocations
							 WHERE tenant_id=$1 AND po_id=$2`,
						tenantID, l.POID).Scan(&advText); err != nil {
						return err
					}
					advance = decimal.RequireFromString(advText)
					advanceByPO[l.POID] = advance
				}
				refunding := addingByPO[l.POID].Add(amt)
				if refunding.GreaterThan(advance) {
					return apierr.Invalid("PAY_ALLOC_REFUND_EXCEEDS_ADVANCE",
						"第 "+lineNo+" 笔退掉后该采购单累计退 "+refunding.String()+
							" 超出预付净额 "+advance.String()+"——只能退实际付过的钱")
				}
				addingByPO[l.POID] = refunding
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
		if isRefund {
			// 唯一的翻号写入点：入参到这里始终是正数。
			amt = amt.Neg()
		}
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
		// 「已经冲销过」必须先于下面的净额闸判——重复冲销撞净额闸会报出
		// 「净额会变负」这种让人摸不着头脑的话。这里在核销行的行锁下查，
		// 判得可靠；末尾 INSERT 上的唯一索引仍是并发时的最后一道兜底。
		var alreadyReversed bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM payment_allocations r
			  WHERE r.tenant_id=$1 AND r.reversal_of=$2)`,
			tenantID, allocID).Scan(&alreadyReversed); err != nil {
			return err
		}
		if alreadyReversed {
			return apierr.Conflict("PAY_ALLOC_ALREADY_REVERSED", "这笔核销已经冲销过")
		}
		// 负行进账本之后，「一笔冲销」不再天然安全，落笔前先看目标上的
		// 净核销冲完还站不站得住：
		//  · 不能为负——发票/采购单上挂着退款时，得先冲退款再冲付款核销，
		//    否则净额变成「退的比付的多」
		//  · 发票不能超票面——冲掉一笔退款，净额弹回去；若这期间又有别的
		//    付款核销进来，弹回去就越过了发票金额
		// 目标行锁 + 锁内求和，与核销那头同一套串行化。
		if invoiceID != 0 {
			var invTotalText string
			if err := tx.QueryRow(ctx, `
				SELECT total_amount::text FROM supplier_invoices
				 WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
				tenantID, invoiceID).Scan(&invTotalText); err != nil {
				return err
			}
			var netText string
			if err := tx.QueryRow(ctx, `
				SELECT coalesce(sum(amount),0)::text FROM payment_allocations
				 WHERE tenant_id=$1 AND invoice_id=$2`,
				tenantID, invoiceID).Scan(&netText); err != nil {
				return err
			}
			after := decimal.RequireFromString(netText).Add(amt)
			if after.IsNegative() {
				return apierr.Conflict("PAY_REVERSE_REFUND_FIRST",
					"冲销后该发票净核销为 "+after.String()+"（负数）——"+
						"先冲销挂在发票上的退款核销，再冲这笔")
			}
			if after.GreaterThan(decimal.RequireFromString(invTotalText)) {
				return apierr.Conflict("PAY_REVERSE_EXCEEDS_INVOICE",
					"冲销这笔退款后发票净核销 "+after.String()+" 超出票面 "+invTotalText+
						"——退款之后发票又被别的付款核销过，先冲销那一笔")
			}
		}
		if poID != 0 {
			var one int
			if err := tx.QueryRow(ctx, `
				SELECT 1 FROM purchase_orders WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
				tenantID, poID).Scan(&one); err != nil {
				return err
			}
			var netText string
			if err := tx.QueryRow(ctx, `
				SELECT coalesce(sum(amount),0)::text FROM payment_allocations
				 WHERE tenant_id=$1 AND po_id=$2`,
				tenantID, poID).Scan(&netText); err != nil {
				return err
			}
			if after := decimal.RequireFromString(netText).Add(amt); after.IsNegative() {
				return apierr.Conflict("PAY_REVERSE_REFUND_FIRST",
					"冲销后该采购单净预付为 "+after.String()+"（负数）——"+
						"先冲销挂在采购单上的退款核销，再冲这笔")
			}
		}
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

// coveredOfPayment 把「这张付款单被说清了多少」换算回正数域。
//
// 退款单的核销行存负数，直接求和得到的是负值；报给守门和界面的量必须
// 是正的「已说清多少」。全部翻号集中在这一个函数（SQL 里对应的 CASE 是
// 它的镜像），别处一律在正数域里比较——这正是客户侧 coveredOf 防住
// 「负数比大小全部失灵」的同一招。
func coveredOfPayment(paymentType string, allocated decimal.Decimal) decimal.Decimal {
	if paymentType == "REFUND" {
		return allocated.Neg()
	}
	return allocated
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
