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

// TransactionInput is one line off a bank statement, as typed in.
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
	Transaction BankRow
	Allocations []store.ListAllocationsOfTransactionRow
	// Contracts whose numbers appear in the remittance line. A suggestion
	// only — see Allocate for why nothing here settles itself.
	Suggestions []store.FindContractsByNoRow
	// 这一行还剩多少没核。算出来的，不存——存一份就会和核销记录对不上。
	AllocatedAmount   string
	UnallocatedAmount string
}

// 收款对账页面上的那三档状态。**它们是算出来的，不是存的。**
//
// 原来这三档存在 disposition 列里。F2 之后「与应收无关」变成了「归属不是
// 客户」，另外两档是核销记录的和与到账金额比出来的——一个存起来的状态和一个
// 算得出来的状态并存，早晚会对不上，而对不上的那一刻没有任何地方看得出来。
const (
	DispositionUnprocessed = "UNPROCESSED"
	DispositionAllocated   = "ALLOCATED"
	DispositionIrrelevant  = "IRRELEVANT"
)

// Disposition 把「归属 + 核了多少」翻译成页面上那三档。
func (v TransactionView) Disposition() string {
	if v.Transaction.Ownership != OwnershipCustomer && v.Transaction.Ownership != OwnershipPending {
		return DispositionIrrelevant
	}
	if mustDec(v.AllocatedAmount).GreaterThanOrEqual(mustDec(v.Transaction.Amount)) {
		return DispositionAllocated
	}
	return DispositionUnprocessed
}

// RecordTransaction stores what the bank said.
//
// 行落在采购那本唯一的账上，归属直接写「客户收款」——在收款对账页登记的人，
// 登记这个动作本身就是在说这是客户打来的。
//
// Nothing here judges whether the money really is a customer payment beyond
// that: interest, tax refunds and transfers between our own accounts can all
// be re-filed later by changing the ownership. Reconciliation is an argument
// about completeness; filtering at the door makes it impossible to ever
// explain a difference between the bank balance and the system.
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
	// 出账暂时拒收。不是永远：收付队列改造（docs/开发计划.md）会让客户退款
	// （出账、归属客户往来）在这一页有自己的子页面。但**今天**这里登记一笔
	// 出账，它会从每一个页面上消失：本页列表写死只出进账，归属又被无条件
	// 写成客户，于是供应商那边的匹配也看不见它——钱录进去了，谁都找不到，
	// 也没有任何提示。一句明确的拒绝比一次无声的吞没好。
	if direction == "DEBIT" {
		return TransactionView{}, apierr.Invalid("EX_TX_DEBIT_NOT_YET",
			"这里暂时只能登记进账（客户打来的钱）。出账（客户退款）的登记入口即将上线；"+
				"付给供应商的钱请走「银行流水」导入。")
	}
	row, err := s.bank.Record(ctx, BankRowInput{
		AccountID: in.AccountID, BankRef: in.BankRef, Direction: direction,
		Amount: amount.StringFixed(2), Currency: in.Currency, ValueDate: in.ValueDate,
		Counterparty: in.Counterparty, CounterpartyAccount: in.CounterpartyAccount,
		RemittanceInfo: in.RemittanceInfo, TrustedRef: in.TrustedRef, Note: in.Note,
	})
	if err != nil {
		return TransactionView{}, err
	}
	return s.viewOf(ctx, tenantID, row)
}

func (s *Service) GetTransaction(ctx context.Context, tenantID, id int64) (TransactionView, error) {
	row, err := s.bank.Get(ctx, id)
	if err != nil {
		return TransactionView{}, err
	}
	return s.viewOf(ctx, tenantID, row)
}

// viewOf 把账本上的一行和出口这边的核销记录拼起来，**给详情页用**。
//
// 它每次都要再问两次库（核销明细、合同号建议），所以列表不走这里——列表
// 只需要一个已核金额，那条路见 ListTransactions。
//
// **已核金额每次都重算**，不读账本上那个 claimed_amount。那一列是为了让队列
// 能分页筛选而存的，万一写回失败就会偏旧；页面上显示的数必须来自核销记录本身。
// 宁可筛错档，不可显示错数。
func (s *Service) viewOf(ctx context.Context, tenantID int64, row BankRow) (TransactionView, error) {
	allocs, err := s.q.ListAllocationsOfTransaction(ctx, store.ListAllocationsOfTransactionParams{
		TenantID: tenantID, TransactionID: row.ID,
	})
	if err != nil {
		return TransactionView{}, err
	}
	suggestions, err := s.suggestContracts(ctx, tenantID, row.TrustedRef, row.RemittanceInfo)
	if err != nil {
		return TransactionView{}, err
	}
	allocated := decimal.Zero
	for _, a := range allocs {
		allocated = allocated.Add(mustDec(a.Amount))
	}
	return TransactionView{
		Transaction: row, Allocations: allocs, Suggestions: suggestions,
		AllocatedAmount:   allocated.StringFixed(2),
		UnallocatedAmount: mustDec(row.Amount).Sub(allocated).StringFixed(2),
	}, nil
}

// TransactionQuery is the queue filter.
type TransactionQuery struct {
	Disposition string
	Direction   string
	Keyword     string
	Page, Size  int32
}

// ListTransactions 是收款对账那个队列。
//
// **这个页面的范围是「所有进账」**，四个档在这个范围里切。一句话说得清，
// 也就意味着「全部」等于三个档加起来——一个对不上自己三个子集的「全部」，
// 会让人不再信这个页面上的任何数。
//
// 出账（付给供应商的钱）不在这里：它核不到应收合同上，摆在这里只会让队列
// 变长而没有一个动作能对它做。客户退款（出账但归客户）等有那个功能了再说。
//
// 待处理这一档要把**还没人认过归属的行也算进去**——「这笔是不是客户打来的」
// 这个判断，就是在这个页面做的。
func (s *Service) ListTransactions(ctx context.Context, tenantID int64, qy TransactionQuery) ([]TransactionView, int64, error) {
	page, size := normalizePage(qy.Page, qy.Size)
	q := BankLedgerQuery{
		Keyword: qy.Keyword, Page: page, Size: size, Direction: "CREDIT",
	}
	mine := []string{OwnershipCustomer, OwnershipPending}
	switch qy.Disposition {
	case DispositionUnprocessed:
		q.OwnershipIn, q.ClaimStatus = mine, "OPEN"
	case DispositionAllocated:
		q.OwnershipIn, q.ClaimStatus = mine, "CLAIMED"
	case DispositionIrrelevant:
		// 「与应收无关」现在的意思就是归属被改到了别的档。那些行还在这个
		// 页面上看得见（也才能撤销标记），只是不在待办队列里。
		q.OwnershipIn = []string{OwnershipSupplier, OwnershipTaxRefund, OwnershipOther}
	}
	rows, total, err := s.bank.List(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, total, nil
	}

	// 一页的已核金额一次问完，不是一行问一次。
	//
	// 原来这里对每一行调 viewOf，而 viewOf 会**再**查一次核销明细、**再**
	// 扫一次汇款附言找合同号——20 行一页就是最多 40 次往返，换来的东西列表
	// 上一样也用不着：明细和合同号建议只有详情页显示。
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	sums, err := s.q.AllocationSumsByTransactions(ctx, store.AllocationSumsByTransactionsParams{
		TenantID: tenantID, TransactionIds: ids,
	})
	if err != nil {
		return nil, 0, err
	}
	allocated := make(map[int64]decimal.Decimal, len(sums))
	for _, a := range sums {
		allocated[a.TransactionID] = mustDec(a.Allocated)
	}

	out := make([]TransactionView, 0, len(rows))
	for _, r := range rows {
		got := allocated[r.ID]
		out = append(out, TransactionView{
			Transaction:       r,
			AllocatedAmount:   got.StringFixed(2),
			UnallocatedAmount: mustDec(r.Amount).Sub(got).StringFixed(2),
		})
	}
	return out, total, nil
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
	var claimed decimal.Decimal
	var row BankRow
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// 原来这里是 SELECT ... FOR UPDATE 锁住那行银行流水。行搬到采购库
		// 之后跨库锁不住了，换成本库的事务级建议锁，按流水 id 取键。
		//
		// 锁的东西从「那一行」变成「那个 id」，效果一样：两个人同时核销同
		// 一笔汇款会排队，不会各自看见「还没核」然后加起来发出去比到账更多
		// 的钱。锁和写入仍在同一个事务、同一个库，事务一结束自动释放。
		//
		// **金额不用锁**：银行那一行是事实，从落库那一刻起就不再改了
		// （账本那边只有归属和认领数会变，金额一个字不动）。所以在锁外面
		// 读到的 row.Amount 和锁里面读到的是同一个数。
		if err := lockReceiptTransaction(ctx, tx, tenantID, txID); err != nil {
			return err
		}

		// **这一行也要在锁里读。**
		//
		// 金额是事实、落库之后不变，在锁外读没问题；**归属会变**。在锁外读
		// 归属，就是这么错的：
		//
		//     甲：读到归属 = 客户
		//                        乙：标记为「与应收无关」，归属改成退税
		//     甲：拿到锁，照着那份旧的归属往下核 ✓
		//
		// 于是几条核销记录指着一笔写着「我不是客户的钱」的流水。锁保护的
		// 必须是「读判断的依据」到「写」这一整段，不是只保护写。
		var err error
		row, err = s.bank.Get(ctx, txID)
		if err != nil {
			return err
		}
		if row.Direction != "CREDIT" {
			return apierr.Invalid("EX_TX_NOT_CREDIT",
				"这是一笔付出去的款，不能核销到应收合同")
		}
		if row.Ownership != OwnershipCustomer && row.Ownership != OwnershipPending {
			return apierr.Invalid("EX_TX_IRRELEVANT",
				"这笔流水的归属不是「客户往来」，不能核销到应收合同。要核先在银行流水改归属。")
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
		total := mustDec(row.Amount)
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
			if progress.Currency != row.Currency {
				return apierr.Invalid("EX_ALLOC_CURRENCY_MISMATCH",
					"币种不一致，不能核销").WithMeta(
					"contract_no", progress.ContractNo,
					"contract_currency", progress.Currency,
					"payment_currency", row.Currency)
			}
			ready = append(ready, checked{line: l, amount: amount, fee: fee, progress: progress})
		}

		if adding.GreaterThan(remaining) {
			return apierr.Invalid("EX_ALLOC_EXCEEDS_PAYMENT",
				"分配金额超过这笔流水的未分配余额").WithMeta(
				"bank_ref", row.BankRef,
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
		claimed = allocated.Add(adding)
		owners = s.ownersOf(ctx, q, tenantID, lines)
		return nil
	})
	if err != nil {
		return TransactionView{}, err
	}
	// 事务外面报给账本。核销记录已经落了，这一步只是让队列筛得准——所以它
	// 失败不该把已经成功的核销回滚掉，见 reportClaim。
	s.reportClaim(ctx, txID, claimed, row.Ownership)
	s.tellOwners(ctx, tenantID, owners)
	return s.GetTransaction(ctx, tenantID, txID)
}

// reportClaim 把重算之后的已核总额报给账本。
//
// **它失败不算核销失败。** 核销记录已经写进出口库了，那是这件事的真相；账本上
// 那个数只决定这一行出现在「待处理」还是「已核销」的筛选里。为了一个筛选把
// 一笔已经成立的核销回滚掉，是拿真的换假的。
//
// 偏了也能自己好：下一次对同一行核销或冲销时会再报一次全量。
func (s *Service) reportClaim(ctx context.Context, txID int64, claimed decimal.Decimal, ownership string) {
	// 归属还空着的话，核销这个动作本身就说明了它是客户那条线上的——先补上，
	// 否则账本会拒绝认领（认领只认客户那一档）。
	if ownership == OwnershipPending {
		if err := s.bank.SetOwnership(ctx, txID, OwnershipCustomer, ""); err != nil {
			// **一定要说出来。** 归属没写上有两个后果：这一行会一直显示成
			// 「待处理」，而且它还空着的归属让供应商那条线可以把它匹配走——
			// 一笔已经核给客户合同的钱，被当成付给供应商的款认领了。
			s.logClaimGap(ctx, txID, claimed, "归属没能写回账本", err)
			return
		}
	}
	if err := s.bank.SetClaim(ctx, txID, claimed.StringFixed(2)); err != nil {
		s.logClaimGap(ctx, txID, claimed, "已核金额没能写回账本", err)
	}
}

// logClaimGap 记下「核销成了、但账本那边没跟上」。
//
// 这里**不回滚也不报错**：核销记录已经落在出口库里了，那是这件事的真相；
// 账本上那个数只决定这一行出现在哪个筛选里。为了一个筛选把一笔已经成立的
// 核销撤掉，是拿真的换假的。
//
// 但**绝不能一声不吭**——原来这里是 `_ =`，采购服务抖一下、或者用户核销完
// 立刻关掉页面（ctx 被取消），那一行就永远停在「待处理」，而没有任何地方
// 能告诉人为什么。日志里带上 bank_txn_id 和金额，是为了能直接拿去对。
//
// 会自己好：下一次对同一行核销或冲销时会再报一次全量。
func (s *Service) logClaimGap(ctx context.Context, txID int64, claimed decimal.Decimal, what string, err error) {
	s.log.WarnContext(ctx, "核销已入账，但银行流水账本没跟上",
		"what", what,
		"bank_txn_id", txID,
		"allocated", claimed.StringFixed(2),
		"err", err.Error(),
		"impact", "这一行会停在「待处理」筛选里，直到下次对它核销或冲销")
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
	var claimed decimal.Decimal
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
		txID = orig.TransactionID
		// 和核销走同一把锁：冲销也在改「这一行还剩多少」。
		if err := lockReceiptTransaction(ctx, tx, tenantID, txID); err != nil {
			return err
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
		after, err := q.ListAllocationsOfTransaction(ctx, store.ListAllocationsOfTransactionParams{
			TenantID: tenantID, TransactionID: txID,
		})
		if err != nil {
			return err
		}
		claimed = decimal.Zero
		for _, a := range after {
			claimed = claimed.Add(mustDec(a.Amount))
		}
		return nil
	})
	if err != nil {
		return TransactionView{}, err
	}
	// 钱又空出来了，账本上的认领数跟着降回去，这一行回到队列里。
	s.reportClaim(ctx, txID, claimed, OwnershipCustomer)
	return s.GetTransaction(ctx, tenantID, txID)
}

// MarkIrrelevant files a line that has nothing to do with receivables.
//
// This exit has to exist. Without it the tax refunds, the interest and the
// transfers between our own accounts pile up in the queue for ever, the queue
// stops being a to-do list, and the whole page gets ignored.
//
// F2 之后它的实现变成了「改归属」——那本来就是这个动作的意思：这笔钱不是
// 客户那条线上的。改完之后这一行从收款对账消失，出现在它该去的地方（供应商
// 退款去供应商对账，其余留在银行流水页）。要改回来，在银行流水页改。
func (s *Service) MarkIrrelevant(ctx context.Context, tenantID, txID int64, kind, note string, op Operator) (TransactionView, error) {
	ownership, detail, ok := ownershipForIrrelevant(kind)
	if !ok {
		return TransactionView{}, apierr.Invalid("EX_IRRELEVANT_TYPE_REQUIRED",
			"请选择这笔流水的类别")
	}
	row, err := s.bank.Get(ctx, txID)
	if err != nil {
		return TransactionView{}, err
	}
	// **要拿锁**，和核销拿的是同一把。
	//
	// 下面那句「已经核销掉的钱不能改归属」是一个读了再判断的检查，不锁的话
	// 它挡不住任何东西：
	//
	//     甲：读核销记录 → 空
	//                        乙：核销 10000（成功）
	//     甲：改归属 = 退税 ✓
	//
	// 结果是几条核销记录指着一笔写着「我不是客户的钱」的流水，而合同上那笔
	// 钱照样算收到了。F2 之前这里是 SELECT ... FOR UPDATE 锁住流水那一行；
	// 行搬到采购库之后我改写时把锁丢了，判断留着——判断留着更糟，因为它看
	// 起来像还挡着。
	//
	// 改归属这一步（gRPC）放在事务里，锁会一直握到它返回。这是有意的：先放
	// 锁再改，就等于没锁。调用很短，握着的是一把建议锁，不挡别的表。
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockReceiptTransaction(ctx, tx, tenantID, txID); err != nil {
			return err
		}
		allocs, err := s.q.WithTx(tx).ListAllocationsOfTransaction(ctx,
			store.ListAllocationsOfTransactionParams{TenantID: tenantID, TransactionID: txID})
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
				WithMeta("bank_ref", row.BankRef, "allocated", live.StringFixed(2))
		}
		return s.bank.SetOwnership(ctx, txID, ownership, detail)
	})
	if err != nil {
		return TransactionView{}, err
	}
	return s.GetTransaction(ctx, tenantID, txID)
}

// ownershipForIrrelevant 把原来那六种「与应收无关」翻译成归属。
//
// 只有「供应商退款」换了地方：它归到供应商那条线，于是**能对到付款单上**——
// 原来它是一条死路，钱进了系统就再也对不上任何东西。
func ownershipForIrrelevant(kind string) (ownership, detail string, ok bool) {
	switch kind {
	case "SUPPLIER_REFUND":
		return OwnershipSupplier, "", true
	case "TAX_REFUND":
		return OwnershipTaxRefund, "", true
	case "INTEREST", "INTERNAL", "DEPOSIT_RETURN", "OTHER":
		return OwnershipOther, kind, true
	}
	return "", "", false
}

// ReopenTransaction takes a line back out of "not ours to match".
func (s *Service) ReopenTransaction(ctx context.Context, tenantID, txID int64, op Operator) (TransactionView, error) {
	if err := s.bank.SetOwnership(ctx, txID, OwnershipCustomer, ""); err != nil {
		return TransactionView{}, err
	}
	return s.GetTransaction(ctx, tenantID, txID)
}

// ContractReceipt 是这张合同收到的一笔钱：出口这边的核销记录，配上账本那边
// 的流水号和到账日期。
//
// 这两半以前是一句 JOIN。F2 之后行在采购库里，跨库 JOIN 不了，只能拿
// transaction_id 去账本取——**取不到就报错，不留空**：一条没有流水号的收款
// 记录在对账时说明不了任何事，空着比报错更难查。
type ContractReceipt struct {
	store.ListAllocationsOfContractRow
	BankRef      string
	ValueDate    string
	Counterparty string
	Source       string
}

// ContractReceipts is the other half of the many-to-many: one contract
// collected in instalments.
//
// 按流水去重之后逐条问账本。条数是「这张合同分几次收的」，通常个位数——
// 冲销行和它冲的那条指着同一笔流水，所以去重是实打实省一次。
func (s *Service) ContractReceipts(ctx context.Context, tenantID, contractID int64) (
	store.ContractReceiptProgressRow, []ContractReceipt, error,
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
	ledger := make(map[int64]BankRow, len(rows))
	out := make([]ContractReceipt, 0, len(rows))
	for _, r := range rows {
		row, ok := ledger[r.TransactionID]
		if !ok {
			row, err = s.bank.Get(ctx, r.TransactionID)
			if err != nil {
				return progress, nil, err
			}
			ledger[r.TransactionID] = row
		}
		out = append(out, ContractReceipt{
			ListAllocationsOfContractRow: r,
			BankRef:                      row.BankRef,
			ValueDate:                    row.ValueDate,
			Counterparty:                 row.Counterparty,
			Source:                       row.Source,
		})
	}
	return progress, out, nil
}

// OpenReceivables feeds the allocation picker.
func (s *Service) OpenReceivables(ctx context.Context, tenantID int64, currency string, customerID int64, keyword string) ([]store.OpenReceivablesRow, error) {
	return s.q.OpenReceivables(ctx, store.OpenReceivablesParams{
		TenantID: tenantID, Currency: currency, CustomerID: customerID,
		Keyword: keyword, RowLimit: 100,
	})
}

// ListBankAccounts 也走账本。账户清单跟着账本走，否则流水上的 account_id
// 指向的是另一个库里的另一套编号——同一个数字，两个意思。
func (s *Service) ListBankAccounts(ctx context.Context, tenantID int64) ([]BankAccount, error) {
	return s.bank.ListAccounts(ctx)
}

// BankAccountInput is one of our own accounts.
type BankAccountInput struct {
	AccountNo, AccountName, BankName, Currency string
}

func (s *Service) CreateBankAccount(ctx context.Context, tenantID int64, in BankAccountInput) (int64, error) {
	if in.AccountNo == "" || in.AccountName == "" {
		return 0, apierr.Invalid("EX_ACCOUNT_REQUIRED", "请填写账号和户名")
	}
	return s.bank.CreateAccount(ctx, BankAccount{
		AccountNo: in.AccountNo, AccountName: in.AccountName,
		BankName: in.BankName, Currency: orDefault(in.Currency, "USD"),
	})
}

// lockReceiptTransaction 让同一笔流水上的核销排队。
//
// 原来靠的是 SELECT ... FOR UPDATE 锁住银行流水那一行。行搬到采购库之后跨库
// 锁不住了，改用本库的事务级建议锁：锁的东西从「那一行」变成「那个 id」，
// 但锁和写入仍在同一个事务、同一个库，事务一结束自动释放。
//
// 两个参数而不是把 tenant 和 id 拼成一个数：不同公司的第 7 号流水互不相干，
// 拼成一个数会让它们抢同一把锁。
func lockReceiptTransaction(ctx context.Context, tx pgx.Tx, tenantID, txID int64) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`,
		receiptLockClass(tenantID), txID)
	return err
}

// pg_advisory_xact_lock 的两参数版收的是 int4。tenant_id 是 int64（用的是
// 纳秒时间戳一类的大数），直接转会溢出，所以取一个稳定的 32 位摘要。
// 撞车的代价只是两笔无关的核销偶尔排一次队，不影响正确性。
func receiptLockClass(tenantID int64) int32 {
	return int32(uint32(tenantID) ^ uint32(tenantID>>32))
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
