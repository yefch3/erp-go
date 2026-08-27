package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/apierr"
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
}

// RecordBankTransaction 手工记下一行流水。
//
// 和 CSV 导入的一处**故意不同**：流水号撞车时导入算「重复」（默默跳过，
// 因为重传上周的文件是正常操作），手工登记则**报错**。手工敲进来的重号只有
// 两种可能——要么这笔钱已经记过了，要么号敲错了——两种都得让人知道，
// 默默吞掉会让人以为钱记上了，而账上根本没有。
func (s *Service) RecordBankTransaction(ctx context.Context, tenantID int64, in BankTransactionInput, op Operator) (BankTransactionView, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return BankTransactionView{}, err
	}
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
		   trusted_ref, note, ownership, ownership_detail)
		VALUES ($1,$2::date,$3,$4::numeric,$5,$6,$7,'','',$8,$9,
		        $10,$11,$12,'MANUAL',$13,$14,$15,$16)
		RETURNING id`,
		tenantID, in.TxnDate, in.Direction, in.Amount, in.Currency, in.Counterparty,
		in.BankRef, op.ID, op.Name,
		in.AccountID, in.CounterpartyAccount, in.RemittanceInfo,
		in.TrustedRef, in.Note, in.Ownership, in.OwnershipDetail).Scan(&id)
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
	if err := s.requireFullScope(ctx, op); err != nil {
		return BankTransactionView{}, err
	}
	var v BankTransactionView
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.txn_date::text, t.direction, t.amount::text, t.currency,
		       t.counterparty, t.bank_ref, t.remark, t.imported_by_name, t.created_at::text,
		       t.ownership, t.ownership_detail,
		       t.account_id, coalesce(a.account_name, ''), t.counterparty_account,
		       t.remittance_info, t.source, t.trusted_ref, t.note,
		       coalesce(p.id, 0), coalesce(p.payment_no, '')
		  FROM bank_transactions t
		  LEFT JOIN bank_accounts a ON a.id = t.account_id AND a.tenant_id = t.tenant_id
		  LEFT JOIN supplier_payments p ON p.bank_txn_id = t.id
		 WHERE t.tenant_id=$1 AND t.id=$2`, tenantID, txnID,
	).Scan(&v.ID, &v.TxnDate, &v.Direction, &v.Amount, &v.Currency,
		&v.Counterparty, &v.BankRef, &v.Remark, &v.ImportedBy, &v.CreatedAt,
		&v.Ownership, &v.OwnershipDetail,
		&v.AccountID, &v.AccountName, &v.CounterpartyAccount,
		&v.RemittanceInfo, &v.Source, &v.TrustedRef, &v.Note,
		&v.MatchedPaymentID, &v.MatchedPaymentNo)
	if errors.Is(err, pgx.ErrNoRows) {
		return BankTransactionView{}, apierr.NotFound("BANK_TXN_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return BankTransactionView{}, err
	}
	return v, nil
}

// ListBankAccounts 给出我们自己的账户。默认只给启用的——登记流水的下拉框里
// 不该出现已经销户的账户。
func (s *Service) ListBankAccounts(ctx context.Context, tenantID int64, includeInactive bool, op Operator) ([]BankAccountView, error) {
	if err := s.requireFullScope(ctx, op); err != nil {
		return nil, err
	}
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
	if err := s.requireFullScope(ctx, op); err != nil {
		return 0, err
	}
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
