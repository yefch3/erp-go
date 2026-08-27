package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// Bank statement rows ride on the order handler like the rest of the spend
// chain: same book, same boundary.

func (h *OrderHandler) ImportBankStatement(ctx context.Context, req *prv1.ImportBankStatementRequest) (*prv1.ImportBankStatementResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	summary, err := h.svc.ImportBankStatement(ctx, grpcx.TenantID(ctx),
		req.GetFileName(), req.GetData(), req.GetDefaultCurrency(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	errs := make([]*prv1.BankImportRowError, 0, len(summary.Errors))
	for _, e := range summary.Errors {
		errs = append(errs, &prv1.BankImportRowError{RowNo: int32(e.RowNo), Reason: e.Reason})
	}
	return &prv1.ImportBankStatementResponse{
		Imported: summary.Imported, Duplicates: summary.Duplicates, Errors: errs,
	}, nil
}

func (h *OrderHandler) ListBankTransactions(ctx context.Context, req *prv1.ListBankTransactionsRequest) (*prv1.ListBankTransactionsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	items, total, err := h.svc.ListBankTransactions(ctx, grpcx.TenantID(ctx),
		app.BankTransactionFilter{
			Status: req.GetStatus(), Direction: req.GetDirection(), Keyword: req.GetKeyword(),
			Ownership: req.GetOwnership(), OwnershipPending: req.GetOwnershipPending(),
			ClaimStatus: req.GetClaimStatus(), OwnershipIn: req.GetOwnershipIn(),
		}, req.GetPage().GetPage(), req.GetPage().GetPageSize(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.BankTransaction, 0, len(items))
	for _, v := range items {
		out = append(out, bankTransactionPB(v))
	}
	return &prv1.ListBankTransactionsResponse{Items: out, Total: total}, nil
}

// bankTransactionPB 是列表和单行共用的那一层翻译。一处写，两处用——两边
// 各写一份的话，下次加字段必然只加一边。
func bankTransactionPB(v app.BankTransactionView) *prv1.BankTransaction {
	return &prv1.BankTransaction{
		Id: v.ID, TxnDate: v.TxnDate, Direction: v.Direction,
		Amount: v.Amount, Currency: v.Currency, Counterparty: v.Counterparty,
		BankRef: v.BankRef, Remark: v.Remark,
		ImportedBy: v.ImportedBy, CreatedAt: v.CreatedAt,
		MatchedPaymentId: v.MatchedPaymentID, MatchedPaymentNo: v.MatchedPaymentNo,
		SuggestedPaymentId: v.SuggestedPaymentID, SuggestedPaymentNo: v.SuggestedPaymentNo,
		SuggestedPaymentSupplier: v.SuggestedPaymentSupplier,
		Ownership:                v.Ownership,
		OwnershipDetail:          v.OwnershipDetail,
		ClaimedAmount:            v.ClaimedAmount,
		AccountId:                v.AccountID,
		AccountName:              v.AccountName,
		CounterpartyAccount:      v.CounterpartyAccount,
		RemittanceInfo:           v.RemittanceInfo,
		Source:                   v.Source,
		TrustedRef:               v.TrustedRef,
		Note:                     v.Note,
	}
}

func (h *OrderHandler) RecordBankTransaction(ctx context.Context, req *prv1.RecordBankTransactionRequest) (*prv1.RecordBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	in := req.GetTransaction()
	v, err := h.svc.RecordBankTransaction(ctx, grpcx.TenantID(ctx), app.BankTransactionInput{
		AccountID:           in.GetAccountId(),
		BankRef:             in.GetBankRef(),
		Direction:           in.GetDirection(),
		Amount:              in.GetAmount(),
		Currency:            in.GetCurrency(),
		TxnDate:             in.GetTxnDate(),
		Counterparty:        in.GetCounterparty(),
		CounterpartyAccount: in.GetCounterpartyAccount(),
		RemittanceInfo:      in.GetRemittanceInfo(),
		TrustedRef:          in.GetTrustedRef(),
		Note:                in.GetNote(),
		Ownership:           in.GetOwnership(),
		OwnershipDetail:     in.GetOwnershipDetail(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.RecordBankTransactionResponse{Transaction: bankTransactionPB(v)}, nil
}

func (h *OrderHandler) SetBankTransactionClaim(ctx context.Context, req *prv1.SetBankTransactionClaimRequest) (*prv1.SetBankTransactionClaimResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.SetBankTransactionClaim(ctx, grpcx.TenantID(ctx),
		req.GetTxnId(), req.GetClaimedAmount(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.SetBankTransactionClaimResponse{}, nil
}

func (h *OrderHandler) GetBankTransaction(ctx context.Context, req *prv1.GetBankTransactionRequest) (*prv1.GetBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.GetBankTransaction(ctx, grpcx.TenantID(ctx), req.GetTxnId(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.GetBankTransactionResponse{Transaction: bankTransactionPB(v)}, nil
}

func (h *OrderHandler) ListBankAccounts(ctx context.Context, req *prv1.ListBankAccountsRequest) (*prv1.ListBankAccountsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	items, err := h.svc.ListBankAccounts(ctx, grpcx.TenantID(ctx), req.GetIncludeInactive(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.BankAccount, 0, len(items))
	for _, a := range items {
		out = append(out, &prv1.BankAccount{
			Id: a.ID, AccountNo: a.AccountNo, AccountName: a.AccountName,
			BankName: a.BankName, Currency: a.Currency, Status: a.Status,
		})
	}
	return &prv1.ListBankAccountsResponse{Accounts: out}, nil
}

func (h *OrderHandler) CreateBankAccount(ctx context.Context, req *prv1.CreateBankAccountRequest) (*prv1.CreateBankAccountResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	id, err := h.svc.CreateBankAccount(ctx, grpcx.TenantID(ctx), app.BankAccountView{
		AccountNo:   req.GetAccountNo(),
		AccountName: req.GetAccountName(),
		BankName:    req.GetBankName(),
		Currency:    req.GetCurrency(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateBankAccountResponse{Id: id}, nil
}

func (h *OrderHandler) MatchBankTransaction(ctx context.Context, req *prv1.MatchBankTransactionRequest) (*prv1.MatchBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.MatchBankTransaction(ctx, grpcx.TenantID(ctx),
		req.GetTxnId(), req.GetPaymentId(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.MatchBankTransactionResponse{}, nil
}

func (h *OrderHandler) UnmatchBankTransaction(ctx context.Context, req *prv1.UnmatchBankTransactionRequest) (*prv1.UnmatchBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.UnmatchBankTransaction(ctx, grpcx.TenantID(ctx),
		req.GetTxnId(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.UnmatchBankTransactionResponse{}, nil
}

func (h *OrderHandler) SetBankTransactionOwnership(ctx context.Context, req *prv1.SetBankTransactionOwnershipRequest) (*prv1.SetBankTransactionOwnershipResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.SetBankTransactionOwnership(ctx, grpcx.TenantID(ctx),
		req.GetTxnId(), req.GetOwnership(), req.GetOwnershipDetail(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.SetBankTransactionOwnershipResponse{}, nil
}
