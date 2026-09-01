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
			Deleted: req.GetDeleted(),
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
		DeletedAt:                v.DeletedAt,
		DeletedBy:                v.DeletedBy,
		DeleteReason:             v.DeleteReason,
		AccountId:                v.AccountID,
		AccountName:              v.AccountName,
		CounterpartyAccount:      v.CounterpartyAccount,
		RemittanceInfo:           v.RemittanceInfo,
		Source:                   v.Source,
		TrustedRef:               v.TrustedRef,
		Note:                     v.Note,
		AttachmentKey:            v.AttachmentKey,
		AttachmentUrl:            v.AttachmentURL,
		AttachmentName:           v.AttachmentName,
	}
}

// 对账单那张纸的两步：先发上传许可，传完再登记 key。文件不经过我们的服务。
func (h *OrderHandler) PresignBankTransactionFile(ctx context.Context, req *prv1.PresignBankTransactionFileRequest) (*prv1.PresignBankTransactionFileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	p, err := h.svc.PresignBankTransactionFile(ctx, grpcx.TenantID(ctx),
		req.GetTxnId(), req.GetFileName(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.PresignBankTransactionFileResponse{
		Key: p.Key, UploadUrl: p.UploadURL, ExpiresSeconds: p.Expires,
	}, nil
}

func (h *OrderHandler) AttachBankTransactionFile(ctx context.Context, req *prv1.AttachBankTransactionFileRequest) (*prv1.AttachBankTransactionFileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.AttachBankTransactionFile(ctx, grpcx.TenantID(ctx),
		req.GetTxnId(), req.GetKey(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.AttachBankTransactionFileResponse{Transaction: bankTransactionPB(v)}, nil
}

func (h *OrderHandler) RecordBankTransaction(ctx context.Context, req *prv1.RecordBankTransactionRequest) (*prv1.RecordBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.RecordBankTransaction(ctx, grpcx.TenantID(ctx),
		bankTransactionInput(req.GetTransaction()),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
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

// DeleteBankTransaction 归档一条流水。要填理由；已被认领或已匹配付款单的
// 会被服务层拒掉。
func (h *OrderHandler) DeleteBankTransaction(ctx context.Context, req *prv1.DeleteBankTransactionRequest) (*prv1.DeleteBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.DeleteBankTransaction(ctx, grpcx.TenantID(ctx), req.GetTxnId(),
		req.GetReason(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.DeleteBankTransactionResponse{}, nil
}

// RestoreBankTransaction 把归档的那一行放回列表。
func (h *OrderHandler) RestoreBankTransaction(ctx context.Context, req *prv1.RestoreBankTransactionRequest) (*prv1.RestoreBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.RestoreBankTransaction(ctx, grpcx.TenantID(ctx), req.GetTxnId(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.RestoreBankTransactionResponse{}, nil
}

// bankTransactionInput 是登记和编辑共用的那一层翻译。
//
// 一处写两处用：编辑就是「把当初填的重填一遍」，两边各抄一份字段清单的话，
// 以后加一个字段只改了一边，另一边会**静默地**把它当成空值——而在编辑那边，
// 空值意味着「你把它清空了」，还会留一条痕说你清空了它。
func bankTransactionInput(in *prv1.BankTransactionInput) app.BankTransactionInput {
	return app.BankTransactionInput{
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
	}
}

// UpdateBankTransaction 改一行流水。要填理由，每处改动留痕。
func (h *OrderHandler) UpdateBankTransaction(ctx context.Context, req *prv1.UpdateBankTransactionRequest) (*prv1.UpdateBankTransactionResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.UpdateBankTransaction(ctx, grpcx.TenantID(ctx), req.GetTxnId(),
		bankTransactionInput(req.GetFields()), req.GetReason(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.UpdateBankTransactionResponse{}, nil
}

// ListBankTransactionChanges 这一行被改过什么。
func (h *OrderHandler) ListBankTransactionChanges(ctx context.Context, req *prv1.ListBankTransactionChangesRequest) (*prv1.ListBankTransactionChangesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	items, err := h.svc.ListBankTransactionChanges(ctx, grpcx.TenantID(ctx), req.GetTxnId(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.BankTransactionChange, 0, len(items))
	for _, c := range items {
		out = append(out, &prv1.BankTransactionChange{
			Field: c.Field, OldValue: c.OldValue, NewValue: c.NewValue,
			Reason: c.Reason, ChangedBy: c.ChangedBy, CreatedAt: c.CreatedAt,
		})
	}
	return &prv1.ListBankTransactionChangesResponse{Items: out}, nil
}
