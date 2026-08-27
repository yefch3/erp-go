package grpcout

import (
	"context"

	"google.golang.org/grpc"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

// BankLedger 把出口对那本唯一银行流水账的提问翻译成对采购的 gRPC 调用。
//
// 这里一个字段都不缓存：账本上的行随时可能被别人改归属、被供应商那条线认领。
// 缓存一份就等于又造出了 F2 要消灭的那第二份账。
type BankLedger struct {
	client prv1.PurchaseOrderServiceClient
}

func NewBankLedger(conn *grpc.ClientConn) *BankLedger {
	return &BankLedger{client: prv1.NewPurchaseOrderServiceClient(conn)}
}

func (b *BankLedger) List(ctx context.Context, q app.BankLedgerQuery) ([]app.BankRow, int64, error) {
	resp, err := b.client.ListBankTransactions(ctx, &prv1.ListBankTransactionsRequest{
		Page:        &commonv1.PageRequest{Page: q.Page, PageSize: q.Size},
		OwnershipIn: q.OwnershipIn,
		ClaimStatus: q.ClaimStatus,
		Direction:   q.Direction,
		Keyword:     q.Keyword,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]app.BankRow, 0, len(resp.GetItems()))
	for _, r := range resp.GetItems() {
		out = append(out, bankRowFrom(r))
	}
	return out, resp.GetTotal(), nil
}

func (b *BankLedger) Get(ctx context.Context, id int64) (app.BankRow, error) {
	resp, err := b.client.GetBankTransaction(ctx, &prv1.GetBankTransactionRequest{TxnId: id})
	if err != nil {
		return app.BankRow{}, err
	}
	return bankRowFrom(resp.GetTransaction()), nil
}

func (b *BankLedger) Record(ctx context.Context, in app.BankRowInput) (app.BankRow, error) {
	resp, err := b.client.RecordBankTransaction(ctx, &prv1.RecordBankTransactionRequest{
		Transaction: &prv1.BankTransactionInput{
			AccountId: in.AccountID, BankRef: in.BankRef, Direction: in.Direction,
			Amount: in.Amount, Currency: in.Currency, TxnDate: in.ValueDate,
			Counterparty: in.Counterparty, CounterpartyAccount: in.CounterpartyAccount,
			RemittanceInfo: in.RemittanceInfo, TrustedRef: in.TrustedRef, Note: in.Note,
			// 从收款对账登记的钱，登记的人就是在说「这是客户打来的」。
			// 留空会让它同时出现在供应商那个队列里，等着第二个人再判断一次。
			Ownership: app.OwnershipCustomer,
		},
	})
	if err != nil {
		return app.BankRow{}, err
	}
	return bankRowFrom(resp.GetTransaction()), nil
}

func (b *BankLedger) SetClaim(ctx context.Context, id int64, claimed string) error {
	_, err := b.client.SetBankTransactionClaim(ctx, &prv1.SetBankTransactionClaimRequest{
		TxnId: id, ClaimedAmount: claimed,
	})
	return err
}

func (b *BankLedger) SetOwnership(ctx context.Context, id int64, ownership, detail string) error {
	_, err := b.client.SetBankTransactionOwnership(ctx, &prv1.SetBankTransactionOwnershipRequest{
		TxnId: id, Ownership: ownership, OwnershipDetail: detail,
	})
	return err
}

func (b *BankLedger) ListAccounts(ctx context.Context) ([]app.BankAccount, error) {
	resp, err := b.client.ListBankAccounts(ctx, &prv1.ListBankAccountsRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]app.BankAccount, 0, len(resp.GetAccounts()))
	for _, a := range resp.GetAccounts() {
		out = append(out, app.BankAccount{
			ID: a.GetId(), AccountNo: a.GetAccountNo(), AccountName: a.GetAccountName(),
			BankName: a.GetBankName(), Currency: a.GetCurrency(), Status: a.GetStatus(),
		})
	}
	return out, nil
}

func (b *BankLedger) CreateAccount(ctx context.Context, in app.BankAccount) (int64, error) {
	resp, err := b.client.CreateBankAccount(ctx, &prv1.CreateBankAccountRequest{
		AccountNo: in.AccountNo, AccountName: in.AccountName,
		BankName: in.BankName, Currency: in.Currency,
	})
	if err != nil {
		return 0, err
	}
	return resp.GetId(), nil
}

func bankRowFrom(r *prv1.BankTransaction) app.BankRow {
	return app.BankRow{
		ID: r.GetId(), AccountID: r.GetAccountId(), AccountName: r.GetAccountName(),
		BankRef: r.GetBankRef(), Direction: r.GetDirection(),
		Amount: r.GetAmount(), Currency: r.GetCurrency(),
		// 账本那边叫 txn_date，出口这边一直叫 value_date，是同一个东西：
		// 银行的日子，不是谁录入的日子。
		ValueDate:           r.GetTxnDate(),
		Counterparty:        r.GetCounterparty(),
		CounterpartyAccount: r.GetCounterpartyAccount(),
		RemittanceInfo:      r.GetRemittanceInfo(),
		Source:              r.GetSource(),
		TrustedRef:          r.GetTrustedRef(),
		Note:                r.GetNote(),
		Ownership:           r.GetOwnership(),
		OwnershipDetail:     r.GetOwnershipDetail(),
		ClaimedAmount:       r.GetClaimedAmount(),
		RecordedByName:      r.GetImportedBy(),
		CreatedAt:           r.GetCreatedAt(),
	}
}
