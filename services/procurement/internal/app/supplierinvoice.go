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
)

// The third leg of the three-way match: what the factory says we owe.
//
// The purchase order and the receipt are our own records; the invoice is the
// only document here written by the other side. It is stored verbatim as the
// claim it is — matching (P2) judges it against the other two legs, and
// payment allocation (P3) settles it. This file only records and reads.

var supplierInvoiceTypes = map[string]bool{
	"COMMERCIAL": true, "PROFORMA": true,
	"VAT_SPECIAL": true, "VAT_PLAIN": true, "OTHER": true,
}

// SupplierInvoiceLineInput is one claimed line as entered.
type SupplierInvoiceLineInput struct {
	POID        int64
	POItemID    int64
	Description string
	Qty         string
	UnitPrice   string
	Amount      string
}

// SupplierInvoiceInput is the paper as entered.
type SupplierInvoiceInput struct {
	SupplierID   int64
	SupplierCode string
	SupplierName string
	InvoiceNo    string
	InvoiceType  string
	Currency     string
	TotalAmount  string
	TaxAmount    string
	InvoiceDate  string
	DueDate      string
	Lines        []SupplierInvoiceLineInput
}

// SupplierInvoiceLine is one stored line, with the order number resolved for
// display.
type SupplierInvoiceLine struct {
	ID          int64
	POID        int64
	POItemID    int64
	PONo        string
	Description string
	Qty         string
	UnitPrice   string
	Amount      string
}

// SupplierInvoice is the stored paper.
type SupplierInvoice struct {
	ID           int64
	SupplierID   int64
	SupplierCode string
	SupplierName string
	InvoiceNo    string
	InvoiceType  string
	Currency     string
	TotalAmount  string
	TaxAmount    string
	InvoiceDate  string
	DueDate      string
	MatchStatus  string
	MatchNote    string
	Status       string
	VoidReason   string
	CreatedBy    string
	CreatedAt    string
	Lines        []SupplierInvoiceLine
	// The scan of the paper. URL is minted per read and never persisted:
	// it expires, the key does not.
	AttachmentKey  string
	AttachmentURL  string
	AttachmentName string
	// Book-currency snapshot from entry time (P6); zero = not captured.
	BaseCurrency string
	BaseAmount   string
	FxRate       string
}

// SupplierInvoiceFilter narrows the list.
type SupplierInvoiceFilter struct {
	SupplierID  int64
	Status      string
	MatchStatus string
	Keyword     string
}

// CreateSupplierInvoice records the factory's claim.
//
// Validation stops at "is this a coherent document" — number, dates, amounts,
// and that any bound order really belongs to this supplier. It deliberately
// does NOT judge whether the amounts are right; that is the matcher's job,
// and an invoice that disagrees with our records must still be recordable,
// because the disagreement is the thing the reconciliation exists to surface.
func (s *Service) CreateSupplierInvoice(ctx context.Context, tenantID int64, in SupplierInvoiceInput, op Operator) (SupplierInvoice, error) {
	if in.SupplierID <= 0 || strings.TrimSpace(in.SupplierName) == "" {
		return SupplierInvoice{}, apierr.Invalid("INV_SUPPLIER_REQUIRED", "请选择供应商")
	}
	invoiceNo := strings.TrimSpace(in.InvoiceNo)
	if invoiceNo == "" {
		return SupplierInvoice{}, apierr.Invalid("INV_NO_REQUIRED", "请填写发票号")
	}
	invType := strings.TrimSpace(in.InvoiceType)
	if invType == "" {
		invType = "COMMERCIAL"
	}
	if !supplierInvoiceTypes[invType] {
		return SupplierInvoice{}, apierr.Invalid("INV_TYPE_INVALID", "发票类型无效")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" || len(currency) > 8 {
		return SupplierInvoice{}, apierr.Invalid("INV_CURRENCY_REQUIRED", "请填写币种")
	}
	total, err := decimal.NewFromString(strings.TrimSpace(in.TotalAmount))
	if err != nil || !total.IsPositive() {
		return SupplierInvoice{}, apierr.Invalid("INV_TOTAL_INVALID", "发票金额必须大于零")
	}
	tax, err := decimal.NewFromString(orZero(strings.TrimSpace(in.TaxAmount)))
	if err != nil || tax.IsNegative() {
		return SupplierInvoice{}, apierr.Invalid("INV_TAX_INVALID", "税额不能为负数")
	}
	if _, err := time.Parse("2006-01-02", in.InvoiceDate); err != nil {
		return SupplierInvoice{}, apierr.Invalid("INV_DATE_INVALID", "开票日期格式应为 YYYY-MM-DD")
	}
	dueDate := strings.TrimSpace(in.DueDate)
	if dueDate != "" {
		if _, err := time.Parse("2006-01-02", dueDate); err != nil {
			return SupplierInvoice{}, apierr.Invalid("INV_DUE_DATE_INVALID", "到期日格式应为 YYYY-MM-DD")
		}
	}

	type lineRow struct {
		poID, poItemID     int64
		desc               string
		qty, price, amount decimal.Decimal
	}
	rows := make([]lineRow, 0, len(in.Lines))
	sum := decimal.Zero
	for i, l := range in.Lines {
		amount, err := decimal.NewFromString(strings.TrimSpace(l.Amount))
		if err != nil {
			return SupplierInvoice{}, apierr.Invalid("INV_LINE_AMOUNT_INVALID", "第 "+itoa(i+1)+" 行金额无效")
		}
		qty, err := decimal.NewFromString(orZero(strings.TrimSpace(l.Qty)))
		if err != nil || qty.IsNegative() {
			return SupplierInvoice{}, apierr.Invalid("INV_LINE_QTY_INVALID", "第 "+itoa(i+1)+" 行数量无效")
		}
		price, err := decimal.NewFromString(orZero(strings.TrimSpace(l.UnitPrice)))
		if err != nil || price.IsNegative() {
			return SupplierInvoice{}, apierr.Invalid("INV_LINE_PRICE_INVALID", "第 "+itoa(i+1)+" 行单价无效")
		}
		poID, poItemID := l.POID, l.POItemID
		// A bound order line implies its order; resolve and cross-check so a
		// line can never point at somebody else's purchase.
		if poItemID != 0 {
			var itemPO, poSupplier int64
			var poCurrency string
			err := s.pool.QueryRow(ctx,
				`SELECT i.po_id, o.supplier_id, o.currency FROM purchase_order_items i
				   JOIN purchase_orders o ON o.id = i.po_id
				  WHERE i.tenant_id=$1 AND i.id=$2`, tenantID, poItemID).Scan(&itemPO, &poSupplier, &poCurrency)
			if err != nil {
				return SupplierInvoice{}, apierr.Invalid("INV_LINE_ITEM_INVALID", "第 "+itoa(i+1)+" 行的采购明细不存在")
			}
			if poID != 0 && poID != itemPO {
				return SupplierInvoice{}, apierr.Invalid("INV_LINE_ITEM_MISMATCH", "第 "+itoa(i+1)+" 行的明细不属于所选采购单")
			}
			if poSupplier != in.SupplierID {
				return SupplierInvoice{}, apierr.Invalid("INV_LINE_SUPPLIER_MISMATCH", "第 "+itoa(i+1)+" 行的采购单不属于该供应商")
			}
			if poCurrency != currency {
				// The matcher subtracts the claim (invoice currency) from the
				// payable (order currency); a mixed pair makes that arithmetic
				// meaningless. Refused at entry, exactly as cross-currency
				// settlement is refused at allocation.
				return SupplierInvoice{}, apierr.Invalid("INV_LINE_CURRENCY_MISMATCH",
					"第 "+itoa(i+1)+" 行币种不符：发票 "+currency+"，采购单 "+poCurrency)
			}
			poID = itemPO
		} else if poID != 0 {
			var poSupplier int64
			var poCurrency string
			err := s.pool.QueryRow(ctx,
				`SELECT supplier_id, currency FROM purchase_orders WHERE tenant_id=$1 AND id=$2`,
				tenantID, poID).Scan(&poSupplier, &poCurrency)
			if err != nil {
				return SupplierInvoice{}, apierr.Invalid("INV_LINE_PO_INVALID", "第 "+itoa(i+1)+" 行的采购单不存在")
			}
			if poSupplier != in.SupplierID {
				return SupplierInvoice{}, apierr.Invalid("INV_LINE_SUPPLIER_MISMATCH", "第 "+itoa(i+1)+" 行的采购单不属于该供应商")
			}
			if poCurrency != currency {
				return SupplierInvoice{}, apierr.Invalid("INV_LINE_CURRENCY_MISMATCH",
					"第 "+itoa(i+1)+" 行币种不符：发票 "+currency+"，采购单 "+poCurrency)
			}
		}
		sum = sum.Add(amount)
		rows = append(rows, lineRow{poID: poID, poItemID: poItemID,
			desc: strings.TrimSpace(l.Description), qty: qty, price: price, amount: amount})
	}
	// When lines are given they must reproduce the header total exactly. Both
	// numbers come off the same sheet of paper, so a gap is a typo, and a typo
	// caught at entry costs seconds where one caught at settlement costs a
	// phone call to the factory.
	if len(rows) > 0 && !sum.Equal(total) {
		return SupplierInvoice{}, apierr.Invalid("INV_LINES_TOTAL_MISMATCH",
			"明细合计 "+sum.String()+" 与发票金额 "+total.String()+" 不一致")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SupplierInvoice{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Same-moment fx snapshot as payments get (P6): gain/loss is the
	// difference between two snapshots, and one alone prices nothing.
	fxRate, baseAmount := s.fxSnapshot(ctx, currency, total)
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO supplier_invoices
		  (tenant_id, supplier_id, supplier_code, supplier_name, invoice_no,
		   invoice_type, currency, total_amount, tax_amount, invoice_date,
		   due_date, created_by_id, created_by_name,
		   base_currency, base_amount, fx_rate)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9::numeric,$10::date,
		        nullif($11,'')::date,$12,$13,$14,$15::numeric,$16::numeric)
		RETURNING id`,
		tenantID, in.SupplierID, strings.TrimSpace(in.SupplierCode),
		strings.TrimSpace(in.SupplierName), invoiceNo, invType, currency,
		total.String(), tax.String(), in.InvoiceDate, dueDate, op.ID, op.Name,
		s.bookCurrency(), baseAmount.String(), fxRate.String(),
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return SupplierInvoice{}, apierr.Conflict("INV_DUPLICATE",
				"该供应商已录入过发票号 "+invoiceNo+"——重复录入是重复付款最常见的起点")
		}
		return SupplierInvoice{}, err
	}
	for _, r := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO supplier_invoice_lines
			  (tenant_id, invoice_id, po_id, po_item_id, description, qty, unit_price, amount)
			VALUES ($1,$2,nullif($3,0),nullif($4,0),$5,$6::numeric,$7::numeric,$8::numeric)`,
			tenantID, id, r.poID, r.poItemID, r.desc,
			r.qty.String(), r.price.String(), r.amount.String()); err != nil {
			return SupplierInvoice{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SupplierInvoice{}, err
	}
	// The verdict lands with the entry so the screen never shows a bare
	// PENDING for a paper that could have been judged at once. Best-effort:
	// the entry is committed, and a matcher hiccup must not look like a
	// failed save — PENDING plus the re-run button is the honest fallback.
	_ = s.matchInvoice(ctx, tenantID, id)
	s.nudge(ctx, tenantID)
	return s.GetSupplierInvoice(ctx, tenantID, id)
}

// ListSupplierInvoices pages through the claims.
func (s *Service) ListSupplierInvoices(ctx context.Context, tenantID int64, f SupplierInvoiceFilter, page, size int32, operators ...Operator) ([]SupplierInvoice, int64, error) {
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
		SELECT id, supplier_id, supplier_code, supplier_name, invoice_no,
		       invoice_type, currency, total_amount::text, tax_amount::text,
		       invoice_date::text, coalesce(due_date::text,''),
		       match_status, match_note, status, void_reason,
		       created_by_name, created_at::text,
		       count(*) OVER () AS total
		  FROM supplier_invoices
		 WHERE tenant_id = $1
		   AND ($2 = 0 OR supplier_id = $2)
		   AND ($3 = '' OR status = $3)
		   AND ($4 = '' OR match_status = $4)
		   AND ($5 = '' OR invoice_no ILIKE '%'||$5||'%' OR supplier_name ILIKE '%'||$5||'%')
		   AND ($6::bool OR created_by_id = ANY($7::bigint[]))
		 ORDER BY created_at DESC, id DESC
		 LIMIT $8 OFFSET $9`,
		tenantID, f.SupplierID, f.Status, f.MatchStatus,
		strings.TrimSpace(f.Keyword), visible.All, visible.EmployeeIDs,
		size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SupplierInvoice
	var total int64
	for rows.Next() {
		var v SupplierInvoice
		if err := rows.Scan(&v.ID, &v.SupplierID, &v.SupplierCode, &v.SupplierName,
			&v.InvoiceNo, &v.InvoiceType, &v.Currency, &v.TotalAmount, &v.TaxAmount,
			&v.InvoiceDate, &v.DueDate, &v.MatchStatus, &v.MatchNote,
			&v.Status, &v.VoidReason, &v.CreatedBy, &v.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// GetSupplierInvoice reads one claim with its lines.
func (s *Service) GetSupplierInvoice(ctx context.Context, tenantID, id int64) (SupplierInvoice, error) {
	var v SupplierInvoice
	err := s.pool.QueryRow(ctx, `
		SELECT id, supplier_id, supplier_code, supplier_name, invoice_no,
		       invoice_type, currency, total_amount::text, tax_amount::text,
		       invoice_date::text, coalesce(due_date::text,''),
		       match_status, match_note, status, void_reason,
		       created_by_name, created_at::text, attachment_key,
		       base_currency, base_amount::text, fx_rate::text
		  FROM supplier_invoices WHERE tenant_id=$1 AND id=$2`, tenantID, id,
	).Scan(&v.ID, &v.SupplierID, &v.SupplierCode, &v.SupplierName,
		&v.InvoiceNo, &v.InvoiceType, &v.Currency, &v.TotalAmount, &v.TaxAmount,
		&v.InvoiceDate, &v.DueDate, &v.MatchStatus, &v.MatchNote,
		&v.Status, &v.VoidReason, &v.CreatedBy, &v.CreatedAt, &v.AttachmentKey,
		&v.BaseCurrency, &v.BaseAmount, &v.FxRate)
	if err == pgx.ErrNoRows {
		return SupplierInvoice{}, apierr.NotFound("INV_NOT_FOUND", "发票不存在")
	}
	if err != nil {
		return SupplierInvoice{}, err
	}
	if v.AttachmentKey != "" {
		v.AttachmentName = attachmentDisplayName(v.AttachmentKey)
		if s.files != nil {
			// A failed presign degrades to "listed but not downloadable",
			// which beats failing the whole read.
			if url, err := s.files.PresignGet(ctx, v.AttachmentKey); err == nil {
				v.AttachmentURL = url
			}
		}
	}
	lines, err := s.pool.Query(ctx, `
		SELECT l.id, coalesce(l.po_id,0), coalesce(l.po_item_id,0),
		       coalesce(o.po_no,''), l.description,
		       l.qty::text, l.unit_price::text, l.amount::text
		  FROM supplier_invoice_lines l
		  LEFT JOIN purchase_orders o ON o.id = l.po_id
		 WHERE l.tenant_id=$1 AND l.invoice_id=$2
		 ORDER BY l.id`, tenantID, id)
	if err != nil {
		return SupplierInvoice{}, err
	}
	defer lines.Close()
	for lines.Next() {
		var l SupplierInvoiceLine
		if err := lines.Scan(&l.ID, &l.POID, &l.POItemID, &l.PONo,
			&l.Description, &l.Qty, &l.UnitPrice, &l.Amount); err != nil {
			return SupplierInvoice{}, err
		}
		v.Lines = append(v.Lines, l)
	}
	return v, lines.Err()
}

// VoidSupplierInvoice takes a claim out of play without erasing it.
//
// VOID, not DELETE: the paper existed, somebody entered it, and "we decided
// to ignore it, and why" is part of the record — the same reasoning that
// makes allocations reverse instead of vanish on the receivables side.
func (s *Service) VoidSupplierInvoice(ctx context.Context, tenantID, id int64, reason string, op Operator) (SupplierInvoice, error) {
	if strings.TrimSpace(reason) == "" {
		return SupplierInvoice{}, apierr.Invalid("INV_VOID_REASON_REQUIRED", "请填写作废原因")
	}
	if err := s.AuthorizeSupplierInvoice(ctx, tenantID, id, op); err != nil {
		return SupplierInvoice{}, err
	}
	// An invoice with live allocations refuses to void: money is recorded as
	// settling this claim, and "the claim doesn't count but the settlement
	// stands" is not a state the book can hold. Reverse the allocations
	// first, one by one.
	//
	// 判据是「每一笔核销都已被冲销」而**不是**「净额为零」。退款核销落负行
	// 之后，付 2000 又退 2000 的发票净额恰好是 0——但两笔真钱都发生过、
	// 都记在这张发票上，把它当「从没结算过」作废，那两笔钱就没了下落。
	// 冲销对（原行 + 指回它的 reversal 行）不算数；没被冲销的行，不论正负，
	// 都算数。
	var live int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM payment_allocations a
		 WHERE a.tenant_id=$1 AND a.invoice_id=$2 AND a.reversal_of IS NULL
		   AND NOT EXISTS (SELECT 1 FROM payment_allocations r
		                    WHERE r.tenant_id=a.tenant_id AND r.reversal_of=a.id)`,
		tenantID, id).Scan(&live); err != nil {
		return SupplierInvoice{}, err
	}
	if live > 0 {
		return SupplierInvoice{}, apierr.Conflict("INV_HAS_ALLOCATIONS",
			"发票上还挂着 "+itoa(live)+" 笔未冲销的核销/退款，先逐笔冲销再作废").
			WithMeta("liveAllocations", itoa(live))
	}
	cmd, err := s.pool.Exec(ctx, `
		UPDATE supplier_invoices SET status='VOID', void_reason=$3
		 WHERE tenant_id=$1 AND id=$2 AND status='OPEN'`,
		tenantID, id, strings.TrimSpace(reason))
	if err != nil {
		return SupplierInvoice{}, err
	}
	if cmd.RowsAffected() == 0 {
		return SupplierInvoice{}, apierr.Conflict("INV_NOT_OPEN", "发票不存在或已不是待处理状态")
	}
	s.nudge(ctx, tenantID)
	return s.GetSupplierInvoice(ctx, tenantID, id)
}

func itoa(v int) string { return strconv.Itoa(v) }
