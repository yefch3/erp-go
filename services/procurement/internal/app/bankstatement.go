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
// 谁能看这本账，见下面那段「谁能看」。

// ---------------------------------------------------------------- 谁能看
//
// 银行流水**不走数据范围这条闸**，走网关的权限。
//
// 数据范围回答的是「这些行归谁，你能看见谁的」。银行流水没有归属人——
// 一笔汇款不是某个业务员的，它就是公司账上的一笔钱。拿一个「归谁」的问题
// 去问一堆没有主人的行，答案只能是错的，而这里错的方向特别难看：
//
// 原来这里每个方法都要求「采购订单全量范围」。生产上有那个范围的只有超级
// 管理员——FINANCE 和 PROCUREMENT_MANAGER 的 procurement_order 是 SELF。
// 于是**银行流水这个页面对财务是打不开的**，而它本来就是给财务做的。
// 页面直接报「对账视图需要采购订单的全量数据范围」，一句财务看不懂、也
// 无从下手的话。
//
// 真正的闸在网关：/api/bank-transactions* 要 procurement:payment:read|write，
// /api/receipt-transactions* 要 export:receipt:read|write。权限本来就归网关
// 管，数据范围才归这一层管——这一层不该替网关再问一遍它已经问过的问题，
// 更不该拿一个不适用的问题去问。
//
// 供应商对账（supplierstatement.go）那边的 requireFullScope 留着：它算的是
// 按供应商汇总的采购订单金额，那些行**确实有主人**，按人截断的合计会像完整
// 余额一样被当真。同一个函数，两种数据，只在一边适用。

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
	// F2 第二步补进来的：出口那本账原来独有的信息。AccountID 为 0 表示
	// 没填账户（CSV 导进来的行没有这个信息），**不是第 0 号账户**。
	AccountID           int64
	AccountName         string
	CounterpartyAccount string
	// 付款人自己写进汇款里的话。扫合同号靠它，和 Remark（银行导出文件里
	// 那一列备注）不是一回事。
	RemittanceInfo string
	Source         string
	TrustedRef     string
	Note           string
	// 这一行已经被认领了多少钱。**「处理完了没有」对两条线是同一个定义**：
	// ClaimedAmount < Amount 就是还没完。供应商那条线由匹配写（匹配即全额），
	// 客户那条线由出口服务核销之后写回来。
	ClaimedAmount string
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
	// 认领状态。"" 不筛 / OPEN 还没认领完 / CLAIMED 认领完了。
	// 这就是财务每天要清的那个队列。
	ClaimStatus string
	// 一次筛好几档归属。非空时**压过** Ownership 和 OwnershipPending。
	// 空串在这里是正常元素，表示「还没人认过的那一档」。
	OwnershipIn []string
}

// 认领状态的两档。
const (
	ClaimOpen    = "OPEN"
	ClaimClaimed = "CLAIMED"
)

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
	page, size = normalizePage(page, size)
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.txn_date::text, t.direction, t.amount::text, t.currency,
		       t.counterparty, t.bank_ref, t.remark, t.imported_by_name, t.created_at::text,
		       t.ownership, t.ownership_detail,
		       t.account_id, coalesce(a.account_name, ''), t.counterparty_account,
		       t.remittance_info, t.source, t.trusted_ref, t.note,
		       t.claimed_amount::text,
		       coalesce(p.id, 0), coalesce(p.payment_no, ''),
		       coalesce(sg.id, 0), coalesce(sg.payment_no, ''), coalesce(sg.supplier_name, ''),
		       count(*) OVER () AS total
		  FROM bank_transactions t
		  LEFT JOIN bank_accounts a ON a.id = t.account_id AND a.tenant_id = t.tenant_id
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
		   AND ($4 = '' OR t.counterparty ILIKE '%'||$4||'%' OR t.bank_ref ILIKE '%'||$4||'%'
		        OR t.remark ILIKE '%'||$4||'%' OR t.remittance_info ILIKE '%'||$4||'%')
		   -- 归属：$10 给一组时按组筛（压过下面两个）；否则 $5 指定某一档，
		   -- $6 为真时单出「待处理」（ownership='')。
		   --
		   -- coalesce 不能省：Go 的 nil 切片到了这里是 NULL，而
		   -- cardinality(NULL) 是 NULL 不是 0——三个条件会一起变成 NULL，
		   -- 于是整张表被筛空，而且不报错。
		   AND (coalesce(cardinality($10::text[]), 0) = 0 OR t.ownership = ANY($10::text[]))
		   AND (coalesce(cardinality($10::text[]), 0) > 0 OR $6 OR $5 = '' OR t.ownership = $5)
		   AND (coalesce(cardinality($10::text[]), 0) > 0 OR NOT $6 OR t.ownership = '')
		   -- 认领状态：财务每天要清的队列。空串不筛。
		   AND ($7 = '' OR ($7 = 'OPEN') = (t.claimed_amount < t.amount))
		 ORDER BY t.txn_date DESC, t.id DESC
		 LIMIT $8 OFFSET $9`,
		tenantID, f.Status, f.Direction, strings.TrimSpace(f.Keyword),
		f.Ownership, f.OwnershipPending, f.ClaimStatus,
		size, (page-1)*size, f.OwnershipIn)
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
			&v.AccountID, &v.AccountName, &v.CounterpartyAccount,
			&v.RemittanceInfo, &v.Source, &v.TrustedRef, &v.Note,
			&v.ClaimedAmount,
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
	//
	// 同时记下「全额认领」。供应商这条线是全有或全无：一张付款单认领整行。
	// 金额可以对不上（中间行会扣手续费），但**认领这件事没有一半**——所以
	// 写的是 amount 而不是付款单的金额。这样「还没处理完」对客户和供应商
	// 两条线就是同一个定义：claimed_amount < amount。
	if _, err := s.pool.Exec(ctx, `
		UPDATE bank_transactions
		   SET ownership = CASE WHEN ownership='' THEN $3 ELSE ownership END,
		       claimed_amount = amount
		 WHERE tenant_id=$1 AND id=$2`,
		tenantID, txnID, OwnershipSupplier); err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	return nil
}

// SetBankTransactionOwnership 记下这笔钱是谁那条线上的。
//
// 银行那一行本身一个字不改——归属是**我们的判断**，和匹配一样可以改、可以
// 改回空（重新变成待处理）。同「付款是事实、核销是判断」。
func (s *Service) SetBankTransactionOwnership(ctx context.Context, tenantID, txnID int64, ownership, detail string, op Operator) error {
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
	// 已经被认领的流水不许把归属改走：改走了，认领它的那张单据就指着一笔
	// 写着「我不是你那条线上的钱」的流水。要改先解开认领。
	//
	// **两条线都要挡，一边一句。** 这里原来只写了供应商那一句，客户那句漏了，
	// 于是两步就能把一笔已核销的钱变成孤儿：在收款对账核销 10000（认领满），
	// 再到银行流水页把归属改成「不用核销」——后端放行，而出口库里那几条核销
	// 记录还指着它，合同上那 10000 照样算收到了。
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
	var claimed, rowAmount string
	err := s.pool.QueryRow(ctx, `
		SELECT claimed_amount::text, amount::text FROM bank_transactions
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID).Scan(&claimed, &rowAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return err
	}
	// 认领数是客户那条线核销之后写回来的（供应商那条线走上面的 matched，
	// 它匹配时也会把这个数写满，所以这里只在没有付款单认领时才管——否则
	// 取消匹配那条路会被自己挡住）。
	//
	// **这一道是尽力而为，不是最后一道。** 它看的是账本上那个写回来的数，
	// 万一出口那次写回失败了（会记 WARN，见 export 的 logClaimGap），这里
	// 就看不见那笔核销。真正说了算的是出口自己那道——收款对账的「标记与
	// 应收无关」在锁里读的是核销记录本身。这里挡的是从银行流水页绕过去的
	// 那条路，能挡住绝大多数，挡不住的那部分在出口那边还有一道。
	if amt, ok := normalizeBankAmount(claimed); ok && amt.IsPositive() &&
		matched == 0 && ownership != OwnershipCustomer {
		return apierr.Conflict("BANK_TXN_OWNERSHIP_ALLOCATED",
			"这条流水已经核销到出口合同（已核 "+amt.String()+"），要改归属请先在收款对账里冲销")
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
	tag, err := s.pool.Exec(ctx, `
		UPDATE supplier_payments SET bank_txn_id=NULL, bank_ref=''
		 WHERE tenant_id=$1 AND bank_txn_id=$2`, tenantID, txnID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apierr.Conflict("BANK_TXN_NOT_MATCHED", "这条流水没有匹配任何付款单")
	}
	// 认领跟着一起撤销，否则这一行会永远停在「已处理」里，再也回不到队列。
	// 归属**不动**：取消匹配是「这张付款单不对」，不是「这笔钱不是供应商的」。
	if _, err := s.pool.Exec(ctx, `
		UPDATE bank_transactions SET claimed_amount=0
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID); err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	return nil
}
