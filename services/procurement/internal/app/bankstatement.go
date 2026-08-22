package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// The bank statement: the fifth voice in the reconciliation, and the only
// one written by a machine that does not care what anybody meant. Rows are
// imported verbatim and never edited; matching is OUR judgement, recorded
// on the payment (bank_txn_id), reversible without touching the bank's row.
//
// Everything here requires full data scope, like the statement view and for
// the same reason: a bank account is not divisible by clerk.

// BankImportSummary is what one upload did.
type BankImportSummary struct {
	Imported   int32
	Duplicates int32
	Errors     []BankRowError
}

// BankTransactionView is one bank row plus what our book says about it.
type BankTransactionView struct {
	ID           int64
	TxnDate      string
	Direction    string
	Amount       string
	Currency     string
	Counterparty string
	BankRef      string
	Remark       string
	ImportedBy   string
	CreatedAt    string
	// Set when a payment claims this row.
	MatchedPaymentID int64
	MatchedPaymentNo string
	// The closest unclaimed payment with the same currency and amount
	// within a few days — a suggestion, never an action.
	SuggestedPaymentID       int64
	SuggestedPaymentNo       string
	SuggestedPaymentSupplier string
}

// BankTransactionFilter narrows the list.
type BankTransactionFilter struct {
	Status    string // MATCHED | UNMATCHED | ""
	Direction string // DEBIT | CREDIT | ""
	Keyword   string
}

// ImportBankStatement parses and stores the upload. Valid rows import,
// broken rows come back with their Excel row numbers, and rows the bank
// already told us about (same bank_ref) count as duplicates — re-uploading
// last week's file is the expected workflow, not an error.
func (s *Service) ImportBankStatement(ctx context.Context, tenantID int64, fileName string, data []byte, defaultCurrency string, op Operator) (BankImportSummary, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return BankImportSummary{}, err
	}
	if len(data) == 0 {
		return BankImportSummary{}, apierr.Invalid("BANK_CSV_EMPTY", "文件为空")
	}
	rows, rowErrs, err := parseBankStatementCSV(data, defaultCurrency)
	if err != nil {
		return BankImportSummary{}, err
	}
	summary := BankImportSummary{Errors: rowErrs}
	for _, row := range rows {
		tag, err := s.pool.Exec(ctx, `
			INSERT INTO bank_transactions
			  (tenant_id, txn_date, direction, amount, currency, counterparty,
			   bank_ref, remark, source_file, imported_by_id, imported_by_name)
			VALUES ($1,$2::date,$3,$4::numeric,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (tenant_id, bank_ref) DO NOTHING`,
			tenantID, row.TxnDate, row.Direction, row.Amount.String(), row.Currency,
			row.Counterparty, row.BankRef, row.Remark,
			strings.TrimSpace(fileName), op.ID, op.Name)
		if err != nil {
			return BankImportSummary{}, err
		}
		if tag.RowsAffected() == 0 {
			summary.Duplicates++
		} else {
			summary.Imported++
		}
	}
	s.nudge(ctx, tenantID)
	return summary, nil
}

// ListBankTransactions pages the bank's story next to ours: each row with
// the payment that claims it, or the closest unclaimed candidate.
func (s *Service) ListBankTransactions(ctx context.Context, tenantID int64, f BankTransactionFilter, page, size int32, op Operator) ([]BankTransactionView, int64, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, 0, err
	}
	page, size = normalizePage(page, size)
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.txn_date::text, t.direction, t.amount::text, t.currency,
		       t.counterparty, t.bank_ref, t.remark, t.imported_by_name, t.created_at::text,
		       coalesce(p.id, 0), coalesce(p.payment_no, ''),
		       coalesce(sg.id, 0), coalesce(sg.payment_no, ''), coalesce(sg.supplier_name, ''),
		       count(*) OVER () AS total
		  FROM bank_transactions t
		  LEFT JOIN supplier_payments p ON p.bank_txn_id = t.id
		  LEFT JOIN LATERAL (
		      SELECT sp.id, sp.payment_no, sp.supplier_name
		        FROM supplier_payments sp
		       WHERE p.id IS NULL AND t.direction = 'DEBIT'
		         AND sp.tenant_id = t.tenant_id AND sp.bank_txn_id IS NULL
		         AND sp.currency = t.currency AND sp.amount = t.amount
		         AND abs(sp.paid_at - t.txn_date) <= 5
		       ORDER BY abs(sp.paid_at - t.txn_date), sp.id
		       LIMIT 1
		  ) sg ON true
		 WHERE t.tenant_id = $1
		   AND ($2 = '' OR ($2 = 'MATCHED') = (p.id IS NOT NULL))
		   AND ($3 = '' OR t.direction = $3)
		   AND ($4 = '' OR t.counterparty ILIKE '%'||$4||'%' OR t.bank_ref ILIKE '%'||$4||'%' OR t.remark ILIKE '%'||$4||'%')
		 ORDER BY t.txn_date DESC, t.id DESC
		 LIMIT $5 OFFSET $6`,
		tenantID, f.Status, f.Direction, strings.TrimSpace(f.Keyword),
		size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []BankTransactionView
	var total int64
	for rows.Next() {
		var v BankTransactionView
		if err := rows.Scan(&v.ID, &v.TxnDate, &v.Direction, &v.Amount, &v.Currency,
			&v.Counterparty, &v.BankRef, &v.Remark, &v.ImportedBy, &v.CreatedAt,
			&v.MatchedPaymentID, &v.MatchedPaymentNo,
			&v.SuggestedPaymentID, &v.SuggestedPaymentNo, &v.SuggestedPaymentSupplier,
			&total); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// MatchBankTransaction records that this payment is the one the bank row
// confirms. Amounts MAY differ (intermediary charges shave wires); currency
// may not — a match across currencies is a category error, not a judgement.
func (s *Service) MatchBankTransaction(ctx context.Context, tenantID, txnID, paymentID int64, op Operator) error {
	if err := s.requireFullScope(ctx, op); err != nil {
		return err
	}
	var direction, txnCurrency, bankRef string
	err := s.pool.QueryRow(ctx, `
		SELECT direction, currency, bank_ref FROM bank_transactions
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID,
	).Scan(&direction, &txnCurrency, &bankRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return err
	}
	if direction != "DEBIT" {
		return apierr.Invalid("BANK_TXN_DIRECTION", "只有出账流水才能匹配供应商付款")
	}
	var payCurrency string
	err = s.pool.QueryRow(ctx, `
		SELECT currency FROM supplier_payments
		 WHERE tenant_id=$1 AND id=$2 AND bank_txn_id IS NULL`, tenantID, paymentID,
	).Scan(&payCurrency)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.Conflict("PAY_NOT_MATCHABLE", "付款单不存在，或已经匹配了别的流水")
	}
	if err != nil {
		return err
	}
	if payCurrency != txnCurrency {
		return apierr.Invalid("BANK_MATCH_CURRENCY", "流水币种 "+txnCurrency+" 与付款币种 "+payCurrency+" 不一致")
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE supplier_payments SET bank_txn_id=$3, bank_ref=$4
		 WHERE tenant_id=$1 AND id=$2 AND bank_txn_id IS NULL`,
		tenantID, paymentID, txnID, bankRef)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// The partial unique index: somebody matched this bank row to
			// another payment between our read and our write.
			return apierr.Conflict("BANK_TXN_ALREADY_MATCHED", "这条流水已经被别的付款单认领")
		}
		return err
	}
	s.nudge(ctx, tenantID)
	return nil
}

// UnmatchBankTransaction withdraws the judgement. Only our side changes:
// the payment lets go of the row, the bank's record never moves.
func (s *Service) UnmatchBankTransaction(ctx context.Context, tenantID, txnID int64, op Operator) error {
	if err := s.requireFullScope(ctx, op); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE supplier_payments SET bank_txn_id=NULL, bank_ref=''
		 WHERE tenant_id=$1 AND bank_txn_id=$2`, tenantID, txnID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apierr.Conflict("BANK_TXN_NOT_MATCHED", "这条流水没有匹配任何付款单")
	}
	s.nudge(ctx, tenantID)
	return nil
}
