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
	// 这一笔钱是谁那条线上的。空串 = 待处理，还没人认领过。
	// 取值见 00027 迁移；**归属不是 Direction 的同义词**，供应商退款是进账。
	Ownership       string
	OwnershipDetail string
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
	// "" 表示不按归属筛。要单独筛出「还没人认领的」用 OwnershipPending。
	Ownership        string // CUSTOMER | SUPPLIER | TAX_REFUND | OTHER | ""
	OwnershipPending bool   // true 时只出 ownership='' 的那些
	Keyword          string
}

// 归属的五档。空串是第五档：待处理。
const (
	OwnershipCustomer  = "CUSTOMER"
	OwnershipSupplier  = "SUPPLIER"
	OwnershipTaxRefund = "TAX_REFUND"
	OwnershipOther     = "OTHER"
)

func validOwnership(v string) bool {
	switch v {
	case "", OwnershipCustomer, OwnershipSupplier, OwnershipTaxRefund, OwnershipOther:
		return true
	}
	return false
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
		       t.ownership, t.ownership_detail,
		       coalesce(p.id, 0), coalesce(p.payment_no, ''),
		       coalesce(sg.id, 0), coalesce(sg.payment_no, ''), coalesce(sg.supplier_name, ''),
		       count(*) OVER () AS total
		  FROM bank_transactions t
		  LEFT JOIN supplier_payments p ON p.bank_txn_id = t.id
		  LEFT JOIN LATERAL (
		      SELECT sp.id, sp.payment_no, sp.supplier_name
		        FROM supplier_payments sp
		       WHERE p.id IS NULL AND t.ownership IN ('', 'SUPPLIER')
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
		   -- 归属：$5 指定某一档；$6 为真时单出「待处理」（ownership='')。
		   -- 两者互斥，由调用方保证，这里按「先看 pending」处理。
		   AND ($6 OR $5 = '' OR t.ownership = $5)
		   AND (NOT $6 OR t.ownership = '')
		 ORDER BY t.txn_date DESC, t.id DESC
		 LIMIT $7 OFFSET $8`,
		tenantID, f.Status, f.Direction, strings.TrimSpace(f.Keyword),
		f.Ownership, f.OwnershipPending,
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
			&v.Ownership, &v.OwnershipDetail,
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
	var ownership, txnCurrency, bankRef string
	err := s.pool.QueryRow(ctx, `
		SELECT ownership, currency, bank_ref FROM bank_transactions
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID,
	).Scan(&ownership, &txnCurrency, &bankRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return err
	}
	// 原来这里写的是 direction != "DEBIT" 就拒绝。那条闸踩在一个坑上：
	// **供应商退款是钱进来的**（supplier_payments.payment_type='REFUND'），
	// 却必须对到采购的付款单上。写死只认出账，等于这类钱永远对不上——
	// 于是凡是退过款的供应商，供应商对账那一行的欠款余额都是多算的。
	//
	// 改成看归属：归属已经写着「供应商」的可以匹配；还空着的（待处理）也
	// 可以，匹配这个动作本身就说明了它是谁那条线上的，下面顺手把归属补上。
	// 归属明确写着客户/退税/不用核销的，不该出现在供应商匹配里。
	if ownership != "" && ownership != OwnershipSupplier {
		return apierr.Invalid("BANK_TXN_OWNERSHIP",
			"这条流水的归属不是「供应商」，不能匹配供应商付款。要改先在归属那一列改。")
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
	// 匹配这个动作本身就是在说「这笔钱是供应商那条线上的」，所以顺手把还
	// 空着的归属补上——省掉一次多余的点击，也让列表上的「待处理」是准的。
	if ownership == "" {
		if _, err := s.pool.Exec(ctx, `
			UPDATE bank_transactions SET ownership=$3
			 WHERE tenant_id=$1 AND id=$2 AND ownership=''`,
			tenantID, txnID, OwnershipSupplier); err != nil {
			return err
		}
	}
	s.nudge(ctx, tenantID)
	return nil
}

// SetBankTransactionOwnership 记下这笔钱是谁那条线上的。
//
// 银行那一行本身一个字不改——归属是**我们的判断**，和匹配一样可以改、可以
// 改回空（重新变成待处理）。同「付款是事实、核销是判断」。
func (s *Service) SetBankTransactionOwnership(ctx context.Context, tenantID, txnID int64, ownership, detail string, op Operator) error {
	if err := s.requireFullScope(ctx, op); err != nil {
		return err
	}
	ownership = strings.TrimSpace(ownership)
	detail = strings.TrimSpace(detail)
	if !validOwnership(ownership) {
		return apierr.Invalid("BANK_TXN_OWNERSHIP_INVALID", "归属取值不认识："+ownership)
	}
	// 二级分类只有「不用核销」那一档才有意义。别的档带着它，说明调用方把
	// 状态搞混了——与其悄悄丢掉，不如说出来。
	if detail != "" && ownership != OwnershipOther {
		return apierr.Invalid("BANK_TXN_OWNERSHIP_DETAIL",
			"只有归属为「不用核销」时才能填二级分类")
	}
	// 已经被付款单认领的流水不许改归属：改走了，那张付款单就指着一笔写着
	// 「我不是供应商的钱」的流水。要改先取消匹配。
	var matched int64
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM supplier_payments
		 WHERE tenant_id=$1 AND bank_txn_id=$2`, tenantID, txnID).Scan(&matched); err != nil {
		return err
	}
	if matched > 0 && ownership != OwnershipSupplier {
		return apierr.Conflict("BANK_TXN_OWNERSHIP_MATCHED",
			"这条流水已经匹配了供应商付款单，要改归属请先取消匹配")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE bank_transactions SET ownership=$3, ownership_detail=$4
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID, ownership, detail)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
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
