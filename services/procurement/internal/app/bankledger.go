package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// F2 第二步补上的两块能力：**手工登记**和**我们自己的账户清单**。
//
// 为什么导入之外还要手工登记：银行对账单一天出一次，而客户常常先把水单发过
// 来。财务要能先把这笔钱记下来，才好回复「收到了」。出口的收款对账原来自己
// 有一张表干这件事——F2 之后由这里统一收着，因为**一笔钱只该在一个账本里
// 出现一次**。
//
// 这一版只是把能力放上去，还没有人调用它（收款对账切过来是下一版的事）。

// BankAccountView 是我们自己的一个账户。
type BankAccountView struct {
	ID          int64
	AccountNo   string
	AccountName string
	BankName    string
	Currency    string
	Status      string
}

// BankTransactionInput 是手工登记一行流水要填的东西。
//
// 里面**只有「银行说了什么」**：日期、方向、金额、对方、附言。至于这笔钱要
// 拿去核销哪张合同，那是后面的判断，不在登记这一步做。唯一的例外是归属——
// 登记的人当场就知道这是客户打来的还是供应商退的，让他顺手写上，比事后再点
// 一次强；不知道就留空，落到「待处理」。
type BankTransactionInput struct {
	// 0 表示不指定账户。填了就必须是一个存在且启用的账户。
	AccountID           int64
	BankRef             string
	Direction           string
	Amount              string
	Currency            string
	TxnDate             string
	Counterparty        string
	CounterpartyAccount string
	RemittanceInfo      string
	TrustedRef          string
	Note                string
	Ownership           string
	OwnershipDetail     string
	DocumentNo          string
}

// UpdateBankTransaction 改一行已经登记的流水。
//
// 有了它，「某个字段写错了」不用再走「删掉重新登记」——而那条路还会撞上
// bank_ref 的唯一键：号被归档的那一行占着，重新登记同号会被拒。
//
// **改的是账，所以每一处改动都留痕**（见 00047）：谁、什么时候、哪个字段、
// 从什么改成什么、为什么。一笔钱的金额改过一次而没人知道，是这种表最难查
// 的问题——报表对不上时，谁也说不清是当初录错了还是后来被人改了。
//
// 留痕只记「会影响账」的那几项。归属、备注、附件这些「我们自己说的」有各自
// 的入口，改它们不动账。
//
// **已被认领或已匹配付款单的不许改**——和删除同一道闸，理由也一样：
// claimed_amount 由客户核销那条线写回来，改掉金额而核销那边不知道，合同上
// 那笔钱就和它的来源对不上了。这是「先取消认领」，不是「永远不许」。
func (s *Service) UpdateBankTransaction(ctx context.Context, tenantID, txnID int64, in BankTransactionInput, reason string, op Operator) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return apierr.Invalid("BANK_TXN_EDIT_REASON", "请填写修改原因")
	}
	if len([]rune(reason)) > 500 {
		return apierr.Invalid("BANK_TXN_EDIT_REASON", "修改原因不要超过 500 字")
	}
	in, err := validateBankTransactionInput(in)
	if err != nil {
		return err
	}
	if in.AccountID != 0 {
		var status string
		err := s.pool.QueryRow(ctx, `
			SELECT status FROM bank_accounts WHERE tenant_id=$1 AND id=$2`,
			tenantID, in.AccountID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.Invalid("BANK_ACCOUNT_NOT_FOUND", "收款账户不存在")
		}
		if err != nil {
			return err
		}
		if status != "ACTIVE" {
			return apierr.Invalid("BANK_ACCOUNT_INACTIVE", "这个收款账户已停用")
		}
	}

	// 先把改之前的样子读出来——留痕要的「从什么改成什么」只能在这一刻拿到。
	var before BankTransactionInput
	var claimed string
	var deletedAt *time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT txn_date::text, direction, amount::text, currency, counterparty,
		       bank_ref, account_id, counterparty_account, remittance_info,
		       trusted_ref, note, document_no, claimed_amount::text, deleted_at
		  FROM bank_transactions WHERE tenant_id=$1 AND id=$2`, tenantID, txnID).
		Scan(&before.TxnDate, &before.Direction, &before.Amount, &before.Currency,
			&before.Counterparty, &before.BankRef, &before.AccountID,
			&before.CounterpartyAccount, &before.RemittanceInfo,
			&before.TrustedRef, &before.Note, &before.DocumentNo, &claimed, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return err
	}
	if deletedAt != nil {
		// 已删除的行不许直接改：它在存档里，改它等于偷偷篡改一份已经封存的
		// 记录。要改先恢复——恢复这个动作本身在列表里看得见。
		return apierr.Conflict("BANK_TXN_EDIT_DELETED", "这条流水在已删除里，要改请先恢复")
	}

	// 哪些字段真的变了。没变的不留痕——一次「只改了备注」的保存，不该在
	// 变更记录里堆出六行「金额 1000 → 1000」。
	//
	// **两边要过同一个规范化再比。** 库里的 amount 是 NUMERIC，读出来是
	// "1000.00"；而输入侧 validateBankTransactionInput 会把它变成 "1000"。
	// 直接比的话，一次「只改了备注」的保存会凭空多出一条「金额 1000.00 →
	// 1000」——而且每保存一次多一条，留痕表越翻越假。日期同理（"2026/08/20"
	// 和 "2026-08-20"）。
	//
	// 忽略这里的错误：before 是从库里读出来的，它写进去的时候就过过一次
	// 校验。真过不了也不该让一次编辑失败——大不了多留一条痕。
	if normalized, err := validateBankTransactionInput(before); err == nil {
		before = normalized
	}
	// 归属也在这个表单里。它的规则和别的字段不一样（已匹配的、已核销到
	// 合同的，各有各的说法），所以走 validateOwnershipChange——**同一份**
	// 规则，单独改归属那条路走的也是它。抄一遍的话两套迟早各长各的。
	var beforeOwnership, beforeDetail string
	if err := s.pool.QueryRow(ctx, `
		SELECT ownership, ownership_detail FROM bank_transactions
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID).
		Scan(&beforeOwnership, &beforeDetail); err != nil {
		return err
	}
	newOwnership := strings.TrimSpace(in.Ownership)
	newDetail := strings.TrimSpace(in.OwnershipDetail)
	ownershipMoved := newOwnership != beforeOwnership || newDetail != beforeDetail
	if ownershipMoved {
		if err := s.validateOwnershipChange(ctx, tenantID, txnID, newOwnership, newDetail); err != nil {
			return err
		}
	}

	changes := bankFieldChanges(before, in)
	if len(changes) == 0 && !ownershipMoved {
		return nil
	}
	// 只要动了会影响账的那几项，就要过认领那道闸。只改备注不受影响。
	if touchesLedger(changes) {
		if amt, ok := normalizeBankAmount(claimed); ok && amt.IsPositive() {
			return apierr.Conflict("BANK_TXN_EDIT_CLAIMED",
				"这条流水已经被认领了 "+amt.String()+"，要改金额或日期请先在收款对账里取消认领")
		}
		var matched int64
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM supplier_payments
			 WHERE tenant_id=$1 AND bank_txn_id=$2`, tenantID, txnID).Scan(&matched); err != nil {
			return err
		}
		if matched > 0 {
			return apierr.Conflict("BANK_TXN_EDIT_MATCHED",
				"这条流水已经匹配了供应商付款单，要改请先取消匹配")
		}
	}

	// 改动和留痕在一个事务里。分开的话，中间失败会留下一次没有记录的改动
	// ——而「没有记录的改动」正是这张留痕表要消灭的东西。
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE bank_transactions
			   SET txn_date=$3::date, direction=$4, amount=$5::numeric, currency=$6,
			       counterparty=$7, bank_ref=$8, account_id=$9,
			       counterparty_account=$10, remittance_info=$11,
			       trusted_ref=$12, note=$13,
			       ownership=$14, ownership_detail=$15, document_no=$16
			 WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,
			tenantID, txnID, in.TxnDate, in.Direction, in.Amount, in.Currency,
			in.Counterparty, in.BankRef, in.AccountID,
			in.CounterpartyAccount, in.RemittanceInfo, in.TrustedRef, in.Note,
			newOwnership, newDetail, in.DocumentNo)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				// 改成一个别人已经占着的流水号。**包括已删除的那些**——
				// 唯一键是整表的，归档的行照样占着号。
				return apierr.Conflict("BANK_REF_EXISTS",
					"流水号 "+in.BankRef+" 已经存在（也可能在「已删除」里）")
			}
			return err
		}
		if tag.RowsAffected() == 0 {
			return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
		}
		for _, c := range changes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO bank_transaction_changes
				  (tenant_id, txn_id, field, old_value, new_value, reason,
				   changed_by_id, changed_by_name)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
				tenantID, txnID, c.Field, c.Old, c.New, reason, op.ID, op.Name); err != nil {
				return err
			}
		}
		return nil
	})
}

// bankFieldChange 是一处改动：哪个字段，从什么变成什么。
type bankFieldChange struct{ Field, Old, New string }

// ledgerFields 是「会影响账」的那几项。改了它们要过认领那道闸。
//
// 判断依据是「这一项变了，账上那笔钱还是不是同一笔」：金额、方向、币种、
// 日期决定这笔钱是多少、往哪走、算哪天；流水号是它和银行对账的钥匙。
// 对方名称不在里面——名字写错不影响钱，改它不该逼人先去取消认领。
var ledgerFields = map[string]bool{
	"txn_date": true, "direction": true, "amount": true,
	"currency": true, "bank_ref": true,
}

func touchesLedger(changes []bankFieldChange) bool {
	for _, c := range changes {
		if ledgerFields[c.Field] {
			return true
		}
	}
	return false
}

// bankFieldChanges 比出改之前和改之后的差别。
//
// 只比人能改的那些列。claimed_amount、deleted_at、imported_by 这些不是人在
// 这个表单上填的，不该出现在「你改了什么」里。
func bankFieldChanges(before, after BankTransactionInput) []bankFieldChange {
	pairs := []struct {
		field    string
		old, new string
	}{
		{"txn_date", before.TxnDate, after.TxnDate},
		{"direction", before.Direction, after.Direction},
		{"amount", before.Amount, after.Amount},
		{"currency", before.Currency, after.Currency},
		{"counterparty", before.Counterparty, after.Counterparty},
		{"bank_ref", before.BankRef, after.BankRef},
		{"account_id", strconv.FormatInt(before.AccountID, 10), strconv.FormatInt(after.AccountID, 10)},
		{"counterparty_account", before.CounterpartyAccount, after.CounterpartyAccount},
		{"remittance_info", before.RemittanceInfo, after.RemittanceInfo},
		{"trusted_ref", before.TrustedRef, after.TrustedRef},
		{"note", before.Note, after.Note},
		{"document_no", before.DocumentNo, after.DocumentNo},
	}
	var out []bankFieldChange
	for _, p := range pairs {
		if p.old != p.new {
			out = append(out, bankFieldChange{Field: p.field, Old: p.old, New: p.new})
		}
	}
	return out
}

// ListBankTransactionChanges 是一行流水被改过什么，最近的排前面。
func (s *Service) ListBankTransactionChanges(ctx context.Context, tenantID, txnID int64, op Operator) ([]BankTransactionChangeView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT field, old_value, new_value, reason, changed_by_name, created_at::text
		  FROM bank_transaction_changes
		 WHERE tenant_id=$1 AND txn_id=$2
		 ORDER BY id DESC`, tenantID, txnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankTransactionChangeView{}
	for rows.Next() {
		var v BankTransactionChangeView
		if err := rows.Scan(&v.Field, &v.OldValue, &v.NewValue, &v.Reason,
			&v.ChangedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// BankTransactionChangeView 是变更记录里的一行。
type BankTransactionChangeView struct {
	// 字段名（txn_date / amount / ...），中文标签由界面按它翻——标签会跟着
	// 界面改，字段名不会。
	Field     string
	OldValue  string
	NewValue  string
	Reason    string
	ChangedBy string
	CreatedAt string
}

// RecordBankTransaction 手工记下一行流水。
//
// 和 CSV 导入的一处**故意不同**：流水号撞车时导入算「重复」（默默跳过，
// 因为重传上周的文件是正常操作），手工登记则**报错**。手工敲进来的重号只有
// 两种可能——要么这笔钱已经记过了，要么号敲错了——两种都得让人知道，
// 默默吞掉会让人以为钱记上了，而账上根本没有。
func (s *Service) RecordBankTransaction(ctx context.Context, tenantID int64, in BankTransactionInput, op Operator) (BankTransactionView, error) {
	in, err := validateBankTransactionInput(in)
	if err != nil {
		return BankTransactionView{}, err
	}
	if in.AccountID != 0 {
		var status string
		err := s.pool.QueryRow(ctx, `
			SELECT status FROM bank_accounts WHERE tenant_id=$1 AND id=$2`,
			tenantID, in.AccountID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return BankTransactionView{}, apierr.Invalid("BANK_ACCOUNT_NOT_FOUND", "收款账户不存在")
		}
		if err != nil {
			return BankTransactionView{}, err
		}
		if status != "ACTIVE" {
			return BankTransactionView{}, apierr.Invalid("BANK_ACCOUNT_INACTIVE", "这个收款账户已停用")
		}
	}

	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO bank_transactions
		  (tenant_id, txn_date, direction, amount, currency, counterparty,
		   bank_ref, remark, source_file, imported_by_id, imported_by_name,
		   account_id, counterparty_account, remittance_info, source,
		   trusted_ref, note, ownership, ownership_detail, document_no)
		VALUES ($1,$2::date,$3,$4::numeric,$5,$6,$7,'','',$8,$9,
		        $10,$11,$12,'MANUAL',$13,$14,$15,$16,$17)
		RETURNING id`,
		tenantID, in.TxnDate, in.Direction, in.Amount, in.Currency, in.Counterparty,
		in.BankRef, op.ID, op.Name,
		in.AccountID, in.CounterpartyAccount, in.RemittanceInfo,
		in.TrustedRef, in.Note, in.Ownership, in.OwnershipDetail, in.DocumentNo).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return BankTransactionView{}, apierr.Conflict("BANK_REF_EXISTS",
				"流水号 "+in.BankRef+" 已经登记过了。要么这笔钱记过一次，要么号敲错了。")
		}
		return BankTransactionView{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetBankTransaction(ctx, tenantID, id, op)
}

// GetBankTransaction 取单独一行，形状和列表里的一行完全一致。
//
// 核销之前必须先拿到它：**金额和币种决定这笔钱能不能核、能核多少**，而这两
// 个数只有账本说了算，不能由调用方带进来。
func (s *Service) GetBankTransaction(ctx context.Context, tenantID, txnID int64, op Operator) (BankTransactionView, error) {
	var v BankTransactionView
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.txn_date::text, t.direction, t.amount::text, t.currency,
		       t.counterparty, t.bank_ref, t.remark, t.imported_by_name, t.created_at::text,
		       t.ownership, t.ownership_detail,
		       t.account_id, coalesce(a.account_name, ''), t.counterparty_account,
		       t.remittance_info, t.source, t.trusted_ref, t.note, t.document_no,
		       t.attachment_key,
		       t.claimed_amount::text,
		       coalesce(p.id, 0), coalesce(p.payment_no, '')
		  FROM bank_transactions t
		  LEFT JOIN bank_accounts a ON a.id = t.account_id AND a.tenant_id = t.tenant_id
		  LEFT JOIN supplier_payments p ON p.bank_txn_id = t.id
		 WHERE t.tenant_id=$1 AND t.id=$2`, tenantID, txnID,
	).Scan(&v.ID, &v.TxnDate, &v.Direction, &v.Amount, &v.Currency,
		&v.Counterparty, &v.BankRef, &v.Remark, &v.ImportedBy, &v.CreatedAt,
		&v.Ownership, &v.OwnershipDetail,
		&v.AccountID, &v.AccountName, &v.CounterpartyAccount,
		&v.RemittanceInfo, &v.Source, &v.TrustedRef, &v.Note, &v.DocumentNo,
		&v.AttachmentKey,
		&v.ClaimedAmount,
		&v.MatchedPaymentID, &v.MatchedPaymentNo)
	if errors.Is(err, pgx.ErrNoRows) {
		return BankTransactionView{}, apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return BankTransactionView{}, err
	}
	s.signAttachment(ctx, &v)
	return v, nil
}

// SetBankTransactionClaim 记下这一行被认领了多少。
//
// 客户那条线的核销记录在出口库，账本自己算不出来，但账本必须知道——否则
// 「还没处理完」那个队列就筛不准。所以出口核完之后把**重算后的总数**报回来。
//
// 三条规矩：
//
//	· 收的是**总额不是增量**。用增量的话，一次网络重试就把数加了两遍。
//	· 认领不能超过这一行本身。超了说明调用方算错了，与其存下一个不可能的数，
//	  不如当场拒绝——账上出现「认领 6 万、到账 5 万」比报错难查得多。
//	· 归属必须是客户那一档。供应商那条线由匹配写，两边同时写同一列会互相
//	  覆盖，而覆盖的结果没有任何地方看得出来。
func (s *Service) SetBankTransactionClaim(ctx context.Context, tenantID, txnID int64, claimed string, op Operator) error {
	amt, ok := normalizeBankAmount(claimed)
	if !ok || amt.IsNegative() {
		return apierr.Invalid("BANK_CLAIM_INVALID", "认领金额看不懂："+claimed)
	}
	var ownership, rowAmount string
	err := s.pool.QueryRow(ctx, `
		SELECT ownership, amount::text FROM bank_transactions
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID).Scan(&ownership, &rowAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return err
	}
	if ownership != OwnershipCustomer {
		return apierr.Invalid("BANK_CLAIM_OWNERSHIP",
			"这条流水的归属不是「客户往来」，认领金额由供应商那条线的匹配来写")
	}
	total, _ := normalizeBankAmount(rowAmount)
	if amt.GreaterThan(total) {
		return apierr.Invalid("BANK_CLAIM_EXCEEDS",
			"认领 "+amt.String()+" 超过这一行的 "+total.String())
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE bank_transactions SET claimed_amount=$3::numeric
		 WHERE tenant_id=$1 AND id=$2`, tenantID, txnID, amt.String()); err != nil {
		return err
	}
	s.nudge(ctx, tenantID)
	return nil
}

// ListBankAccounts 给出我们自己的账户。默认只给启用的——登记流水的下拉框里
// 不该出现已经销户的账户。
func (s *Service) ListBankAccounts(ctx context.Context, tenantID int64, includeInactive bool, op Operator) ([]BankAccountView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, account_no, account_name, bank_name, currency, status
		  FROM bank_accounts
		 WHERE tenant_id=$1 AND ($2 OR status='ACTIVE')
		 ORDER BY id`, tenantID, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BankAccountView
	for rows.Next() {
		var a BankAccountView
		if err := rows.Scan(&a.ID, &a.AccountNo, &a.AccountName, &a.BankName,
			&a.Currency, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateBankAccount 记下一个我们自己的账户。
func (s *Service) CreateBankAccount(ctx context.Context, tenantID int64, a BankAccountView, op Operator) (int64, error) {
	a.AccountNo = strings.TrimSpace(a.AccountNo)
	a.AccountName = strings.TrimSpace(a.AccountName)
	a.BankName = strings.TrimSpace(a.BankName)
	a.Currency = strings.ToUpper(strings.TrimSpace(a.Currency))
	if a.AccountNo == "" {
		return 0, apierr.Invalid("BANK_ACCOUNT_NO_REQUIRED", "账号必填")
	}
	if a.AccountName == "" {
		return 0, apierr.Invalid("BANK_ACCOUNT_NAME_REQUIRED", "户名必填")
	}
	if a.Currency == "" {
		a.Currency = "USD"
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO bank_accounts (tenant_id, account_no, account_name, bank_name, currency)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		tenantID, a.AccountNo, a.AccountName, a.BankName, a.Currency).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, apierr.Conflict("BANK_ACCOUNT_EXISTS", "这个账号已经登记过了")
		}
		return 0, err
	}
	return id, nil
}

// validateBankTransactionInput 在写库之前把话说清楚，顺手把值规整了。
func validateBankTransactionInput(in BankTransactionInput) (BankTransactionInput, error) {
	in.BankRef = strings.TrimSpace(in.BankRef)
	in.Direction = strings.ToUpper(strings.TrimSpace(in.Direction))
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Counterparty = strings.TrimSpace(in.Counterparty)
	in.CounterpartyAccount = strings.TrimSpace(in.CounterpartyAccount)
	in.TrustedRef = strings.TrimSpace(in.TrustedRef)
	in.Ownership = strings.TrimSpace(in.Ownership)
	in.OwnershipDetail = strings.TrimSpace(in.OwnershipDetail)
	in.DocumentNo = strings.TrimSpace(in.DocumentNo)

	// 流水号是去重的依据。没有它，同一笔钱记两遍就没有任何东西拦得住。
	if in.BankRef == "" {
		return in, apierr.Invalid("BANK_REF_REQUIRED", "银行流水号必填——去重靠它")
	}
	if in.Direction != "DEBIT" && in.Direction != "CREDIT" {
		return in, apierr.Invalid("BANK_DIRECTION_INVALID", "方向只能是进账或出账")
	}
	if in.Currency == "" {
		return in, apierr.Invalid("BANK_CURRENCY_REQUIRED", "币种必填")
	}
	amt, ok := normalizeBankAmount(in.Amount)
	if !ok {
		return in, apierr.Invalid("BANK_AMOUNT_INVALID", "金额看不懂："+in.Amount)
	}
	// 方向已经说明了钱往哪走，金额只记大小。负数金额配上方向会变成双重
	// 否定，读账的人得在脑子里算一次符号——那正是记错账的地方。
	if !amt.IsPositive() {
		return in, apierr.Invalid("BANK_AMOUNT_INVALID", "金额必须大于 0，钱往哪走看方向")
	}
	in.Amount = amt.String()
	date, ok := normalizeBankDate(in.TxnDate)
	if !ok {
		return in, apierr.Invalid("BANK_DATE_INVALID", "交易日期看不懂："+in.TxnDate)
	}
	in.TxnDate = date

	if !validOwnership(in.Ownership) {
		return in, apierr.Invalid("BANK_TXN_OWNERSHIP_INVALID", "归属取值不认识："+in.Ownership)
	}
	// 和 SetBankTransactionOwnership 同一条规矩：二级分类只有「不用核销」
	// 那一档才有意义。
	if in.OwnershipDetail != "" && in.Ownership != OwnershipOther {
		return in, apierr.Invalid("BANK_TXN_OWNERSHIP_DETAIL",
			"只有归属为「不用核销」时才能填二级分类")
	}
	return in, nil
}
