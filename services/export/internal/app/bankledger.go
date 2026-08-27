package app

import "context"

// 那本**唯一**的银行流水账，采购服务守着它（F2）。
//
// 以前出口自己有一张 bank_transactions，采购也有一张，同一笔钱可能在两边各记
// 一次，而且导进来的进账对不了合同、手工登记的出账对不了付款单。现在只有一
// 本，靠「归属」分流：归属是客户的，收款对账管；归属是供应商的，供应商对账管。
//
// **为什么是出口直接问采购，而不是让网关拼。** 核销之前要拿这一行的金额和币种
// 来判断能核多少——让网关把金额带进来，等于把守门的钥匙交给门外的人。出口已经
// 在用同样的方式依赖 masterdata / product / fx / approval / iam 五个服务，
// 再加采购是同一个形状；采购不依赖出口，不成环。
//
// 和这个包里其它出站依赖有一处不同，说在前面：那几个是**写的时候问一次**，
// 这个是**每次翻页都要读**。所以它的失败必须当成页面失败报出去，不能默默给
// 一个空列表——空列表看起来就像「这个月没有收款」。
type BankLedger interface {
	List(ctx context.Context, q BankLedgerQuery) ([]BankRow, int64, error)
	Get(ctx context.Context, id int64) (BankRow, error)
	Record(ctx context.Context, in BankRowInput) (BankRow, error)
	// 报告这一行被核销掉了多少。收的是**总额不是增量**：重试一次不会把数
	// 加两遍。
	SetClaim(ctx context.Context, id int64, claimed string) error
	// 改归属。收款对账的「标记与应收无关」和「撤销标记」都落到这里——
	// 那两个动作的真正含义就是「这笔钱不是客户那条线上的」。
	SetOwnership(ctx context.Context, id int64, ownership, detail string) error
	ListAccounts(ctx context.Context) ([]BankAccount, error)
	CreateAccount(ctx context.Context, in BankAccount) (int64, error)
}

// 归属的取值。收款对账只管前两档：明确是客户的，和还没人认过的。
const (
	OwnershipPending   = ""
	OwnershipCustomer  = "CUSTOMER"
	OwnershipSupplier  = "SUPPLIER"
	OwnershipTaxRefund = "TAX_REFUND"
	OwnershipOther     = "OTHER"
)

// BankLedgerQuery 是收款对账那个队列的筛选条件。
type BankLedgerQuery struct {
	// 要哪几档归属。空 = 不按归属筛。空串是正常元素，表示「还没人认过的」。
	OwnershipIn []string
	// "" 不筛 / OPEN 还没核完 / CLAIMED 核完了
	ClaimStatus string
	// DEBIT / CREDIT / ""。收款对账只看进账。
	Direction  string
	Keyword    string
	Page, Size int32
}

// BankRow 是账本上的一行，出口这边看到的样子。
//
// 这里**没有 disposition**：原来那三档里，「与应收无关」已经变成了归属不是
// 客户，剩下的「待处理 / 已核销」是算出来的（核完没核完看 ClaimedAmount）。
// 一个存起来的状态和一个算得出来的状态并存，早晚会对不上。
type BankRow struct {
	ID                  int64
	AccountID           int64
	AccountName         string
	BankRef             string
	Direction           string
	Amount              string
	Currency            string
	ValueDate           string
	Counterparty        string
	CounterpartyAccount string
	// 付款人自己写进汇款里的话。**扫合同号靠它。**
	RemittanceInfo string
	Source         string
	// 我们自己发出去的号。和客户手打的附言不同，这个可信。
	TrustedRef      string
	Note            string
	Ownership       string
	OwnershipDetail string
	ClaimedAmount   string
	RecordedByName  string
	CreatedAt       string
}

// BankRowInput 是手工登记一行流水要填的东西。
type BankRowInput struct {
	AccountID           int64
	BankRef             string
	Direction           string
	Amount              string
	Currency            string
	ValueDate           string
	Counterparty        string
	CounterpartyAccount string
	RemittanceInfo      string
	TrustedRef          string
	Note                string
}

// BankAccount 是我们自己的一个账户。
type BankAccount struct {
	ID          int64
	AccountNo   string
	AccountName string
	BankName    string
	Currency    string
	Status      string
}
