package app

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// TransactionInput is one line off a bank statement, as typed in or imported.
type TransactionInput struct {
	AccountID           int64
	BankRef             string
	Direction           string
	Amount              string
	Currency            string
	ValueDate           string
	Counterparty        string
	CounterpartyAccount string
	RemittanceInfo      string
	Source              string
	// Only set by a channel we control (payment link, PSP webhook). A
	// customer-typed contract number goes in RemittanceInfo instead, because
	// the two deserve very different amounts of trust.
	TrustedRef string
	Note       string
}

// AllocationLine is one contract's share of a bank line.
type AllocationLine struct {
	ContractID int64
	Amount     string
	// What the intermediary banks took. Absorbed by us: it does not come out
	// of the bank line, it only closes the gap on the contract.
	FeeAmount string
}

// TransactionView is a bank line with what has been decided about it.
type TransactionView struct {
	Transaction store.GetBankTransactionRow
	Allocations []store.ListAllocationsOfTransactionRow
	// Contracts whose numbers appear in the remittance line. A suggestion
	// only — see Allocate for why nothing here settles itself.
	Suggestions []store.FindContractsByNoRow
}

// RecordTransaction stores what the bank said.
//
// Nothing here judges whether the money is a customer payment. Interest,
// tax refunds, transfers between our own accounts and supplier refunds all
// land in the same table, because reconciliation is an argument about
// completeness: filtering at the door makes it impossible to ever explain a
// difference between the bank balance and the system.
func (s *Service) RecordTransaction(ctx context.Context, tenantID int64, in TransactionInput, op Operator) (TransactionView, error) {
	amount, err := decimal.NewFromString(in.Amount)
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return TransactionView{}, apierr.Invalid("EX_TX_AMOUNT_INVALID", "金额必须大于 0")
	}
	if in.BankRef == "" {
		return TransactionView{}, apierr.Invalid("EX_TX_REF_REQUIRED",
			"请填写银行流水号——同一笔重复录入就是靠它挡住的")
	}
	if in.Currency == "" {
		return TransactionView{}, apierr.Invalid("EX_TX_CURRENCY_REQUIRED", "请选择币种")
	}
	if in.ValueDate == "" {
		return TransactionView{}, apierr.Invalid("EX_TX_DATE_REQUIRED", "请填写到账日期")
	}
	if in.AccountID == 0 {
		return TransactionView{}, apierr.Invalid("EX_TX_ACCOUNT_REQUIRED", "请选择收款账户")
	}
	direction := strings.ToUpper(in.Direction)
	if direction != "CREDIT" && direction != "DEBIT" {
		direction = "CREDIT"
	}
	source := strings.ToUpper(in.Source)
	if source == "" {
		source = "MANUAL"
	}

	id, err := s.q.RecordBankTransaction(ctx, store.RecordBankTransactionParams{
		TenantID: tenantID, AccountID: in.AccountID, BankRef: in.BankRef,
		Direction: direction, Amount: amount.StringFixed(2), Currency: in.Currency,
		ValueDate: in.ValueDate, Counterparty: in.Counterparty,
		CounterpartyAccount: in.CounterpartyAccount, RemittanceInfo: in.RemittanceInfo,
		Source: source, TrustedRef: in.TrustedRef, Note: in.Note,
		RecordedBy: op.ID, RecordedByName: op.Name,
	})
	if err != nil {
		return TransactionView{}, translateUnique(err, "EX_TX_REF_TAKEN",
			"这笔银行流水号已经录过了")
	}
	return s.GetTransaction(ctx, tenantID, id)
}

func (s *Service) GetTransaction(ctx context.Context, tenantID, id int64) (TransactionView, error) {
	head, err := s.q.GetBankTransaction(ctx, store.GetBankTransactionParams{
		TenantID: tenantID, ID: id,
	})
	if err == pgx.ErrNoRows {
		return TransactionView{}, apierr.NotFound("EX_TX_NOT_FOUND", "银行流水不存在")
	}
	if err != nil {
		return TransactionView{}, err
	}
	allocs, err := s.q.ListAllocationsOfTransaction(ctx, store.ListAllocationsOfTransactionParams{
		TenantID: tenantID, TransactionID: id,
	})
	if err != nil {
		return TransactionView{}, err
	}
	suggestions, err := s.suggestContracts(ctx, tenantID, head.TrustedRef, head.RemittanceInfo)
	if err != nil {
		return TransactionView{}, err
	}
	return TransactionView{Transaction: head, Allocations: allocs, Suggestions: suggestions}, nil
}

// TransactionQuery is the queue filter.
type TransactionQuery struct {
	Disposition string
	Direction   string
	Keyword     string
	Page, Size  int32
}

func (s *Service) ListTransactions(ctx context.Context, tenantID int64, qy TransactionQuery) ([]store.ListBankTransactionsRow, int64, error) {
	page, size := normalizePage(qy.Page, qy.Size)
	rows, err := s.q.ListBankTransactions(ctx, store.ListBankTransactionsParams{
		TenantID: tenantID, Disposition: qy.Disposition, Direction: qy.Direction,
		Keyword: qy.Keyword, RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

// Allocate decides which contracts a bank line paid for.
//
// The bank line itself is never touched: `amount` stays whatever the bank
// said, and this only adds rows saying where it went. That separation is the
// whole design — the statement is fact, the allocation is judgement, and
// judgement gets revised.
func (s *Service) Allocate(ctx context.Context, tenantID, txID int64, lines []AllocationLine, op Operator) (TransactionView, error) {
	if len(lines) == 0 {
		return TransactionView{}, apierr.Invalid("EX_ALLOC_EMPTY", "请至少分配一笔到合同")
	}
	var owners []int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockBankTransaction(ctx, store.LockBankTransactionParams{
			TenantID: tenantID, ID: txID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_TX_NOT_FOUND", "银行流水不存在")
		}
		if err != nil {
			return err
		}
		if locked.Direction != "CREDIT" {
			return apierr.Invalid("EX_TX_NOT_CREDIT",
				"这是一笔付出去的款，不能核销到应收合同")
		}
		if locked.Disposition == "IRRELEVANT" {
			return apierr.Invalid("EX_TX_IRRELEVANT",
				"这笔流水已标记为与应收无关，请先撤销标记")
		}

		// Read the remaining balance inside the lock. Two people allocating
		// the same line at once would otherwise both see it as unallocated
		// and between them hand out more money than arrived.
		existing, err := q.ListAllocationsOfTransaction(ctx, store.ListAllocationsOfTransactionParams{
			TenantID: tenantID, TransactionID: txID,
		})
		if err != nil {
			return err
		}
		allocated := decimal.Zero
		for _, a := range existing {
			allocated = allocated.Add(mustDec(a.Amount))
		}
		total := mustDec(locked.Amount)
		remaining := total.Sub(allocated)

		// Check every line before writing any of them. A rollback would undo
		// partial work anyway, but refusing up front means the error names
		// the line that is actually wrong rather than whichever one happened
		// to be reached first.
		type checked struct {
			line     AllocationLine
			amount   decimal.Decimal
			fee      decimal.Decimal
			progress store.ContractReceiptProgressRow
		}
		ready := make([]checked, 0, len(lines))
		adding := decimal.Zero
		for _, l := range lines {
			amount, err := decimal.NewFromString(l.Amount)
			if err != nil || amount.LessThanOrEqual(decimal.Zero) {
				return apierr.Invalid("EX_ALLOC_AMOUNT_INVALID", "核销金额必须大于 0")
			}
			fee := decimal.Zero
			if l.FeeAmount != "" {
				fee, err = decimal.NewFromString(l.FeeAmount)
				if err != nil || fee.IsNegative() {
					return apierr.Invalid("EX_ALLOC_FEE_INVALID", "手续费不能为负数")
				}
			}
			adding = adding.Add(amount)

			progress, err := q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{
				TenantID: tenantID, ContractID: l.ContractID,
			})
			if err == pgx.ErrNoRows {
				return apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
			}
			if err != nil {
				return err
			}
			if progress.Currency == "" {
				return apierr.Invalid("EX_CONTRACT_NO_VERSION",
					"合同还没有生效版本，无法核销").WithMeta("contract_no", progress.ContractNo)
			}
			// Cross-currency settlement produces an exchange gain or loss,
			// and there is nowhere to put one until a general ledger exists.
			// Refusing is honest; guessing a rate would quietly invent money.
			if progress.Currency != locked.Currency {
				return apierr.Invalid("EX_ALLOC_CURRENCY_MISMATCH",
					"币种不一致，不能核销").WithMeta(
					"contract_no", progress.ContractNo,
					"contract_currency", progress.Currency,
					"payment_currency", locked.Currency)
			}
			ready = append(ready, checked{line: l, amount: amount, fee: fee, progress: progress})
		}

		if adding.GreaterThan(remaining) {
			return apierr.Invalid("EX_ALLOC_EXCEEDS_PAYMENT",
				"分配金额超过这笔流水的未分配余额").WithMeta(
				"bank_ref", locked.BankRef,
				"remaining", remaining.StringFixed(2),
				"requested", adding.StringFixed(2))
		}

		for _, c := range ready {
			if _, err := q.AddReceiptAllocation(ctx, store.AddReceiptAllocationParams{
				TenantID: tenantID, TransactionID: txID, ContractID: c.line.ContractID,
				ContractNo: c.progress.ContractNo, CustomerName: c.progress.CustomerName,
				Amount: c.amount.StringFixed(2), FeeAmount: c.fee.StringFixed(2),
				Currency: c.progress.Currency, ReversalOf: 0, ReverseReason: "",
				AllocatedBy: op.ID, AllocatedByName: op.Name,
			}); err != nil {
				return err
			}
		}

		// Settled only when nothing is left over. A partly allocated line
		// stays in the queue on purpose: the rest of it is still somebody's
		// money and the page exists to stop that being forgotten.
		disposition := "UNPROCESSED"
		if remaining.Sub(adding).IsZero() {
			disposition = "ALLOCATED"
		}
		if _, err := q.SetTransactionDisposition(ctx, store.SetTransactionDispositionParams{
			TenantID: tenantID, ID: txID, Disposition: disposition, IrrelevantType: "", Note: "",
		}); err != nil {
			return err
		}
		owners = s.ownersOf(ctx, q, tenantID, lines)
		return nil
	})
	if err != nil {
		return TransactionView{}, err
	}
	s.tellOwners(ctx, tenantID, owners)
	return s.GetTransaction(ctx, tenantID, txID)
}

// ReverseAllocation undoes one allocation by writing its opposite.
//
// Not a DELETE and not an UPDATE: the document already requires that a
// confirmed receipt can only be reversed, never removed. Summing the table
// still gives today's answer; reading it in order gives the history of how
// somebody arrived at it, including the mistake.
func (s *Service) ReverseAllocation(ctx context.Context, tenantID, allocID int64, reason string, op Operator) (TransactionView, error) {
	if strings.TrimSpace(reason) == "" {
		return TransactionView{}, apierr.Invalid("EX_REVERSE_REASON_REQUIRED",
			"请填写冲销原因——没有理由的冲销事后没人说得清")
	}
	var txID int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		orig, err := q.GetAllocation(ctx, store.GetAllocationParams{
			TenantID: tenantID, ID: allocID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_ALLOC_NOT_FOUND", "核销记录不存在")
		}
		if err != nil {
			return err
		}
		if orig.ReversalOf != 0 {
			return apierr.Invalid("EX_ALLOC_IS_REVERSAL", "这本身就是一条冲销记录")
		}
		reversed, err := q.AllocationReversed(ctx, store.AllocationReversedParams{
			TenantID: tenantID, AllocationID: allocID,
		})
		if err != nil {
			return err
		}
		if reversed {
			return apierr.Invalid("EX_ALLOC_ALREADY_REVERSED", "这条核销已经冲销过了")
		}
		txID = orig.TransactionID

		if _, err := q.AddReceiptAllocation(ctx, store.AddReceiptAllocationParams{
			TenantID: tenantID, TransactionID: orig.TransactionID,
			ContractID: orig.ContractID, ContractNo: orig.ContractNo,
			CustomerName: orig.CustomerName,
			// Both legs are negated: the money goes back into the bank line's
			// unallocated balance, and the fee stops counting towards the
			// contract as paid.
			Amount:    mustDec(orig.Amount).Neg().StringFixed(2),
			FeeAmount: mustDec(orig.FeeAmount).Neg().StringFixed(2),
			Currency:  orig.Currency, ReversalOf: allocID, ReverseReason: reason,
			AllocatedBy: op.ID, AllocatedByName: op.Name,
		}); err != nil {
			return err
		}
		// Money is free again, so the line goes back into the queue.
		if _, err := q.SetTransactionDisposition(ctx, store.SetTransactionDispositionParams{
			TenantID: tenantID, ID: orig.TransactionID,
			Disposition: "UNPROCESSED", IrrelevantType: "", Note: "",
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return TransactionView{}, err
	}
	return s.GetTransaction(ctx, tenantID, txID)
}

// MarkIrrelevant files a line that has nothing to do with receivables.
//
// This exit has to exist. Without it the tax refunds, the interest and the
// transfers between our own accounts pile up in the queue for ever, the queue
// stops being a to-do list, and the whole page gets ignored.
func (s *Service) MarkIrrelevant(ctx context.Context, tenantID, txID int64, kind, note string, op Operator) (TransactionView, error) {
	if !validIrrelevantType(kind) {
		return TransactionView{}, apierr.Invalid("EX_IRRELEVANT_TYPE_REQUIRED",
			"请选择这笔流水的类别")
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.LockBankTransaction(ctx, store.LockBankTransactionParams{
			TenantID: tenantID, ID: txID,
		})
		if err == pgx.ErrNoRows {
			return apierr.NotFound("EX_TX_NOT_FOUND", "银行流水不存在")
		}
		if err != nil {
			return err
		}
		allocs, err := q.ListAllocationsOfTransaction(ctx, store.ListAllocationsOfTransactionParams{
			TenantID: tenantID, TransactionID: txID,
		})
		if err != nil {
			return err
		}
		live := decimal.Zero
		for _, a := range allocs {
			live = live.Add(mustDec(a.Amount))
		}
		if !live.IsZero() {
			return apierr.Invalid("EX_TX_HAS_ALLOCATIONS",
				"这笔流水已经核销到合同，请先冲销再标记").
				WithMeta("bank_ref", locked.BankRef, "allocated", live.StringFixed(2))
		}
		if _, err := q.SetTransactionDisposition(ctx, store.SetTransactionDispositionParams{
			TenantID: tenantID, ID: txID,
			Disposition: "IRRELEVANT", IrrelevantType: kind, Note: note,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return TransactionView{}, err
	}
	return s.GetTransaction(ctx, tenantID, txID)
}

// ReopenTransaction takes a line back out of "not ours to match".
func (s *Service) ReopenTransaction(ctx context.Context, tenantID, txID int64, op Operator) (TransactionView, error) {
	n, err := s.q.SetTransactionDisposition(ctx, store.SetTransactionDispositionParams{
		TenantID: tenantID, ID: txID, Disposition: "UNPROCESSED", IrrelevantType: "", Note: "",
	})
	if err != nil {
		return TransactionView{}, err
	}
	if n == 0 {
		return TransactionView{}, apierr.NotFound("EX_TX_NOT_FOUND", "银行流水不存在")
	}
	return s.GetTransaction(ctx, tenantID, txID)
}

// ContractReceipts is the other half of the many-to-many: one contract
// collected in instalments.
func (s *Service) ContractReceipts(ctx context.Context, tenantID, contractID int64) (
	store.ContractReceiptProgressRow, []store.ListAllocationsOfContractRow, error,
) {
	progress, err := s.q.ContractReceiptProgress(ctx, store.ContractReceiptProgressParams{
		TenantID: tenantID, ContractID: contractID,
	})
	if err == pgx.ErrNoRows {
		return store.ContractReceiptProgressRow{}, nil,
			apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在")
	}
	if err != nil {
		return store.ContractReceiptProgressRow{}, nil, err
	}
	rows, err := s.q.ListAllocationsOfContract(ctx, store.ListAllocationsOfContractParams{
		TenantID: tenantID, ContractID: contractID,
	})
	if err != nil {
		return progress, nil, err
	}
	return progress, rows, nil
}

// OpenReceivables feeds the allocation picker.
func (s *Service) OpenReceivables(ctx context.Context, tenantID int64, currency string, customerID int64, keyword string) ([]store.OpenReceivablesRow, error) {
	return s.q.OpenReceivables(ctx, store.OpenReceivablesParams{
		TenantID: tenantID, Currency: currency, CustomerID: customerID,
		Keyword: keyword, RowLimit: 100,
	})
}

func (s *Service) ListBankAccounts(ctx context.Context, tenantID int64) ([]store.ListBankAccountsRow, error) {
	return s.q.ListBankAccounts(ctx, tenantID)
}

// BankAccountInput is one of our own accounts.
type BankAccountInput struct {
	AccountNo, AccountName, BankName, Currency string
}

func (s *Service) CreateBankAccount(ctx context.Context, tenantID int64, in BankAccountInput) (int64, error) {
	if in.AccountNo == "" || in.AccountName == "" {
		return 0, apierr.Invalid("EX_ACCOUNT_REQUIRED", "请填写账号和户名")
	}
	id, err := s.q.CreateBankAccount(ctx, store.CreateBankAccountParams{
		TenantID: tenantID, AccountNo: in.AccountNo, AccountName: in.AccountName,
		BankName: in.BankName, Currency: orDefault(in.Currency, "USD"),
	})
	if err != nil {
		return 0, translateUnique(err, "EX_ACCOUNT_TAKEN", "这个账号已经存在")
	}
	return id, nil
}

// contractNoPattern matches the shape of a document number rather than a
// fixed prefix, because the numbering rule is configuration. Anything that
// looks like one is looked up; the ones that do not exist simply do not come
// back, so a false positive here costs nothing.
var contractNoPattern = regexp.MustCompile(`[A-Z]{2,6}-[0-9]{4,8}-[0-9]{2,6}`)

// suggestContracts scrapes contract numbers out of a remittance line.
//
// A suggestion, never an action. What a customer typed into a wire is not
// evidence: it is regularly the previous order's number, or the right number
// against the wrong amount. Only trusted_ref — a reference we issued
// ourselves alongside the amount — could ever settle anything by itself, and
// even that is left to a human until the payment-link path is built.
func (s *Service) suggestContracts(ctx context.Context, tenantID int64, refs ...string) ([]store.FindContractsByNoRow, error) {
	seen := make(map[string]bool)
	var nos []string
	for _, ref := range refs {
		for _, m := range contractNoPattern.FindAllString(strings.ToUpper(ref), -1) {
			if !seen[m] {
				seen[m] = true
				nos = append(nos, m)
			}
		}
	}
	if len(nos) == 0 {
		return nil, nil
	}
	return s.q.FindContractsByNo(ctx, store.FindContractsByNoParams{
		TenantID: tenantID, ContractNos: nos,
	})
}

// ownersOf collects the salespeople whose contracts just got paid, so their
// pages update without a refresh.
func (s *Service) ownersOf(ctx context.Context, q *store.Queries, tenantID int64, lines []AllocationLine) []int64 {
	out := make([]int64, 0, len(lines))
	for _, l := range lines {
		row, err := q.GetContract(ctx, store.GetContractParams{TenantID: tenantID, ID: l.ContractID})
		if err != nil {
			continue
		}
		out = append(out, row.SalesEmployeeID)
	}
	return out
}

func validIrrelevantType(kind string) bool {
	switch kind {
	case "TAX_REFUND", "INTEREST", "INTERNAL", "SUPPLIER_REFUND", "DEPOSIT_RETURN", "OTHER":
		return true
	}
	return false
}
