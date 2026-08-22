package app

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// The reconciliation view (A4 P5). Everything here is derived at read time
// from the five kinds of paper — orders, receipts, exceptions, invoices,
// payments — and never stored: a stored balance is one UPDATE bug away from
// lying, a computed one can only be as wrong as its inputs, which are the
// auditable rows themselves.
//
// One row per (supplier, currency). Currencies are never mixed: before P6
// wires fx there is no honest way to add USD to CNY, and a wrong total on a
// reconciliation screen is worse than two right ones.

// SupplierStatement is one supplier in one currency.
type SupplierStatement struct {
	SupplierID        int64
	SupplierName      string
	Currency          string
	OrderedAmount     string
	ReceivedAmount    string
	ExceptionAmount   string
	InvoicedAmount    string
	PaidAmount        string
	AdvanceAmount     string
	UnallocatedAmount string
	Balance           string
	OverdueCount      int32
	OverdueAmount     string
	// Book-currency view (P6), built only from rows that carry snapshots;
	// zero-snapshot rows sit out rather than pretend. Gain/loss is realized:
	// allocation × (invoice rate − payment rate) — what the claim was booked
	// at versus what the cash was worth when it left.
	BaseCurrency string
	InvoicedBase string
	PaidBase     string
	FxGainLoss   string
}

// StatementLine is one event in the money history, in time order.
type StatementLine struct {
	At      string
	Type    string
	Ref     string
	Against string
	Amount  string
	Balance string
	Note    string
}

// Orders the statement counts as committed spend. DRAFT and the approval
// states are intentions, not promises; CANCELLED promised and then didn't.
const committedOrders = "('ORDERED','PARTIALLY_RECEIVED','RECEIVED')"

// Exception types that reduce what a delivery is worth. WRONG_PRODUCT and
// UNIT_MISMATCH are identity disputes, not quantity ones — the matcher
// (supplierinvoice_match.go) draws the same line.
const valueExceptions = "('SHORT_SHIPMENT','DAMAGE','QUALITY_DISPUTE','RETURN')"

// requireFullScope is the statement's own fence. Every list in this module
// filters rows by their owner; an aggregate cannot — a SELF-scoped caller
// would get sums over the fraction they may see, and a partial balance
// presented as a balance is exactly the kind of number somebody pays from.
// So the view is all-or-nothing, and says so instead of quietly shrinking.
func (s *Service) requireFullScope(ctx context.Context, op Operator) error {
	visible, err := s.visibleOrdersTo(ctx, op)
	if err != nil {
		return err
	}
	if !visible.All {
		return apierr.Permission("PR_RECON_SCOPE_LIMITED",
			"对账视图需要采购订单的全量数据范围——按人截断的合计会像完整余额一样被当真")
	}
	return nil
}

// ListSupplierStatements sums the whole book, one row per supplier+currency.
func (s *Service) ListSupplierStatements(ctx context.Context, tenantID int64, keyword string, op Operator) ([]SupplierStatement, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, err
	}
	// Scalar subqueries rather than one grand join: each number keeps its own
	// WHERE clause readable, and the planner runs them per key row — fine at
	// a trading company's supplier count, and legible at review time.
	rows, err := s.pool.Query(ctx, `
		WITH keys AS (
			SELECT supplier_id, currency FROM purchase_orders
			 WHERE tenant_id=$1 AND status IN `+committedOrders+`
			UNION
			SELECT supplier_id, currency FROM supplier_invoices
			 WHERE tenant_id=$1 AND status <> 'VOID'
			UNION
			SELECT supplier_id, currency FROM supplier_payments WHERE tenant_id=$1
		)
		SELECT * FROM (
			SELECT k.supplier_id, k.currency,
			  coalesce(
			    (SELECT si.supplier_name FROM supplier_invoices si
			      WHERE si.tenant_id=$1 AND si.supplier_id=k.supplier_id ORDER BY si.id DESC LIMIT 1),
			    (SELECT po.supplier_name FROM purchase_orders po
			      WHERE po.tenant_id=$1 AND po.supplier_id=k.supplier_id ORDER BY po.id DESC LIMIT 1),
			    (SELECT sp.supplier_name FROM supplier_payments sp
			      WHERE sp.tenant_id=$1 AND sp.supplier_id=k.supplier_id ORDER BY sp.id DESC LIMIT 1),
			    '') AS supplier_name,
			  coalesce((SELECT sum(po.total_amount) FROM purchase_orders po
			     WHERE po.tenant_id=$1 AND po.supplier_id=k.supplier_id AND po.currency=k.currency
			       AND po.status IN `+committedOrders+`),0)::text AS ordered_amount,
			  coalesce((SELECT sum(i.received_qty * i.unit_price)
			      FROM purchase_order_items i JOIN purchase_orders po ON po.id=i.po_id
			     WHERE po.tenant_id=$1 AND po.supplier_id=k.supplier_id AND po.currency=k.currency
			       AND po.status IN `+committedOrders+`),0)::text AS received_amount,
			  coalesce((SELECT sum(e.qty * i.unit_price)
			      FROM purchase_receipt_exceptions e
			      JOIN purchase_order_items i ON i.id=e.po_item_id
			      JOIN purchase_orders po ON po.id=e.po_id
			     WHERE e.tenant_id=$1 AND po.supplier_id=k.supplier_id AND po.currency=k.currency
			       AND e.status='OPEN' AND e.exception_type IN `+valueExceptions+`),0)::text AS exception_amount,
			  coalesce((SELECT sum(si.total_amount) FROM supplier_invoices si
			     WHERE si.tenant_id=$1 AND si.supplier_id=k.supplier_id AND si.currency=k.currency
			       AND si.status <> 'VOID'),0)::text AS invoiced_amount,
			  coalesce((SELECT sum(a.amount)
			      FROM payment_allocations a JOIN supplier_invoices si ON si.id=a.invoice_id
			     WHERE a.tenant_id=$1 AND si.supplier_id=k.supplier_id AND a.currency=k.currency),0)::text AS paid_amount,
			  coalesce((SELECT sum(a.amount)
			      FROM payment_allocations a JOIN purchase_orders po ON po.id=a.po_id
			     WHERE a.tenant_id=$1 AND po.supplier_id=k.supplier_id AND a.currency=k.currency),0)::text AS advance_amount,
			  (coalesce((SELECT sum(sp.amount) FROM supplier_payments sp
			     WHERE sp.tenant_id=$1 AND sp.supplier_id=k.supplier_id AND sp.currency=k.currency),0)
			   - coalesce((SELECT sum(a.amount + a.fee_amount)
			      FROM payment_allocations a JOIN supplier_payments sp ON sp.id=a.payment_id
			     WHERE a.tenant_id=$1 AND sp.supplier_id=k.supplier_id AND sp.currency=k.currency),0))::text AS unallocated_amount,
			  coalesce((SELECT count(*) FROM supplier_invoices si
			     WHERE si.tenant_id=$1 AND si.supplier_id=k.supplier_id AND si.currency=k.currency
			       AND si.status='OPEN' AND si.due_date IS NOT NULL AND si.due_date < current_date),0) AS overdue_count,
			  coalesce((SELECT sum(si.total_amount - coalesce(
			        (SELECT sum(a.amount) FROM payment_allocations a WHERE a.tenant_id=$1 AND a.invoice_id=si.id),0))
			      FROM supplier_invoices si
			     WHERE si.tenant_id=$1 AND si.supplier_id=k.supplier_id AND si.currency=k.currency
			       AND si.status='OPEN' AND si.due_date IS NOT NULL AND si.due_date < current_date),0)::text AS overdue_amount,
			  round(coalesce((SELECT sum(si.base_amount) FROM supplier_invoices si
			     WHERE si.tenant_id=$1 AND si.supplier_id=k.supplier_id AND si.currency=k.currency
			       AND si.status <> 'VOID'),0), 2)::text AS invoiced_base,
			  round(coalesce((SELECT sum(a.amount * sp.fx_rate)
			      FROM payment_allocations a
			      JOIN supplier_payments sp ON sp.id=a.payment_id
			      JOIN supplier_invoices si ON si.id=a.invoice_id
			     WHERE a.tenant_id=$1 AND si.supplier_id=k.supplier_id AND a.currency=k.currency),0), 2)::text AS paid_base,
			  round(coalesce((SELECT sum(a.amount * (si.fx_rate - sp.fx_rate))
			      FROM payment_allocations a
			      JOIN supplier_payments sp ON sp.id=a.payment_id
			      JOIN supplier_invoices si ON si.id=a.invoice_id
			     WHERE a.tenant_id=$1 AND si.supplier_id=k.supplier_id AND a.currency=k.currency
			       AND si.fx_rate <> 0 AND sp.fx_rate <> 0),0), 2)::text AS fx_gain_loss
			FROM keys k
		) t
		WHERE $2 = '' OR t.supplier_name ILIKE '%'||$2||'%'
		ORDER BY t.supplier_name, t.currency`,
		tenantID, strings.TrimSpace(keyword))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SupplierStatement
	for rows.Next() {
		var v SupplierStatement
		if err := rows.Scan(&v.SupplierID, &v.Currency, &v.SupplierName,
			&v.OrderedAmount, &v.ReceivedAmount, &v.ExceptionAmount,
			&v.InvoicedAmount, &v.PaidAmount, &v.AdvanceAmount,
			&v.UnallocatedAmount, &v.OverdueCount, &v.OverdueAmount,
			&v.InvoicedBase, &v.PaidBase, &v.FxGainLoss); err != nil {
			return nil, err
		}
		v.Balance = subtractMoney(v.InvoicedAmount, v.PaidAmount)
		v.BaseCurrency = s.bookCurrency()
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetSupplierStatement is the drill-in: the same summary for one key, plus
// the money history that explains it, line by line with a running balance.
func (s *Service) GetSupplierStatement(ctx context.Context, tenantID, supplierID int64, currency string, op Operator) (SupplierStatement, []StatementLine, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if supplierID <= 0 || currency == "" {
		return SupplierStatement{}, nil, apierr.Invalid("PR_RECON_KEY_REQUIRED", "请指定供应商与币种")
	}
	// Reuses the list query so the two screens can never disagree about a
	// number; a statement whose header contradicts its own lines is the
	// fastest way to lose the accountant's trust.
	all, err := s.ListSupplierStatements(ctx, tenantID, "", op)
	if err != nil {
		return SupplierStatement{}, nil, err
	}
	var summary SupplierStatement
	found := false
	for _, v := range all {
		if v.SupplierID == supplierID && v.Currency == currency {
			summary, found = v, true
			break
		}
	}
	if !found {
		return SupplierStatement{}, nil, apierr.NotFound("PR_RECON_NOT_FOUND", "该供应商在此币种下没有任何往来")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT at::text, typ, ref, against, amount, note FROM (
			SELECT si.created_at AS at, 'INVOICE' AS typ, si.invoice_no AS ref,
			       '' AS against, si.total_amount::text AS amount, si.status AS note
			  FROM supplier_invoices si
			 WHERE si.tenant_id=$1 AND si.supplier_id=$2 AND si.currency=$3 AND si.status <> 'VOID'
			UNION ALL
			SELECT a.allocated_at,
			       CASE WHEN a.reversal_of IS NOT NULL THEN 'PAYMENT_REVERSAL' ELSE 'PAYMENT' END,
			       sp.payment_no, si.invoice_no, a.amount::text, a.reverse_reason
			  FROM payment_allocations a
			  JOIN supplier_payments sp ON sp.id=a.payment_id
			  JOIN supplier_invoices si ON si.id=a.invoice_id
			 WHERE a.tenant_id=$1 AND si.supplier_id=$2 AND a.currency=$3
			UNION ALL
			SELECT a.allocated_at,
			       CASE WHEN a.reversal_of IS NOT NULL THEN 'ADVANCE_REVERSAL' ELSE 'ADVANCE' END,
			       sp.payment_no, po.po_no, a.amount::text, a.reverse_reason
			  FROM payment_allocations a
			  JOIN supplier_payments sp ON sp.id=a.payment_id
			  JOIN purchase_orders po ON po.id=a.po_id
			 WHERE a.tenant_id=$1 AND po.supplier_id=$2 AND a.currency=$3
		) t ORDER BY at, typ, ref`,
		tenantID, supplierID, currency)
	if err != nil {
		return SupplierStatement{}, nil, err
	}
	defer rows.Close()
	var lines []StatementLine
	running := decimal.Zero
	for rows.Next() {
		var l StatementLine
		if err := rows.Scan(&l.At, &l.Type, &l.Ref, &l.Against, &l.Amount, &l.Note); err != nil {
			return SupplierStatement{}, nil, err
		}
		amt, _ := decimal.NewFromString(l.Amount)
		switch l.Type {
		case "INVOICE":
			running = running.Add(amt)
		case "PAYMENT", "PAYMENT_REVERSAL":
			// Reversal rows carry negative amounts, so one rule covers both.
			running = running.Sub(amt)
		}
		// ADVANCE rows park money on an order; the invoice balance is not
		// theirs to move until somebody re-points them.
		l.Balance = running.StringFixed(2)
		lines = append(lines, l)
	}
	return summary, lines, rows.Err()
}

func subtractMoney(a, b string) string {
	da, _ := decimal.NewFromString(a)
	db, _ := decimal.NewFromString(b)
	return da.Sub(db).StringFixed(2)
}
