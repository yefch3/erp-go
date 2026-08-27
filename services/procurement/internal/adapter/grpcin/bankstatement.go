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
		}, req.GetPage().GetPage(), req.GetPage().GetPageSize(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.BankTransaction, 0, len(items))
	for _, v := range items {
		out = append(out, &prv1.BankTransaction{
			Id: v.ID, TxnDate: v.TxnDate, Direction: v.Direction,
			Amount: v.Amount, Currency: v.Currency, Counterparty: v.Counterparty,
			BankRef: v.BankRef, Remark: v.Remark,
			ImportedBy: v.ImportedBy, CreatedAt: v.CreatedAt,
			MatchedPaymentId: v.MatchedPaymentID, MatchedPaymentNo: v.MatchedPaymentNo,
			SuggestedPaymentId: v.SuggestedPaymentID, SuggestedPaymentNo: v.SuggestedPaymentNo,
			SuggestedPaymentSupplier: v.SuggestedPaymentSupplier,
			Ownership:                v.Ownership,
			OwnershipDetail:          v.OwnershipDetail,
		})
	}
	return &prv1.ListBankTransactionsResponse{Items: out, Total: total}, nil
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
