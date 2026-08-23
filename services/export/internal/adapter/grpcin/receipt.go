package grpcin

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/export/internal/app"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// ReceiptHandler exposes collection. It shares the app service with contracts
// because every allocation is measured against a contract's balance.
type ReceiptHandler struct {
	exv1.UnimplementedReceiptServiceServer
	svc *app.Service
}

func NewReceipts(svc *app.Service) *ReceiptHandler { return &ReceiptHandler{svc: svc} }

func (h *ReceiptHandler) ListBankAccounts(ctx context.Context, _ *exv1.ListBankAccountsRequest) (*exv1.ListBankAccountsResponse, error) {
	rows, err := h.svc.ListBankAccounts(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.BankAccount, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.BankAccount{
			Id: r.ID, AccountNo: r.AccountNo, AccountName: r.AccountName,
			BankName: r.BankName, Currency: r.Currency, Status: r.Status,
		})
	}
	return &exv1.ListBankAccountsResponse{Accounts: out}, nil
}

func (h *ReceiptHandler) CreateBankAccount(ctx context.Context, req *exv1.CreateBankAccountRequest) (*exv1.CreateBankAccountResponse, error) {
	id, err := h.svc.CreateBankAccount(ctx, grpcx.TenantID(ctx), app.BankAccountInput{
		AccountNo: req.GetAccountNo(), AccountName: req.GetAccountName(),
		BankName: req.GetBankName(), Currency: req.GetCurrency(),
	})
	if err != nil {
		return nil, err
	}
	return &exv1.CreateBankAccountResponse{Id: id}, nil
}

func (h *ReceiptHandler) ListTransactions(ctx context.Context, req *exv1.ListTransactionsRequest) (*exv1.ListTransactionsResponse, error) {
	rows, total, err := h.svc.ListTransactions(ctx, grpcx.TenantID(ctx), app.TransactionQuery{
		Disposition: req.GetDisposition(), Direction: req.GetDirection(),
		Keyword: req.GetKeyword(),
		Page:    req.GetPage().GetPage(), Size: req.GetPage().GetPageSize(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.BankTransaction, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.BankTransaction{
			Id: r.ID, BankRef: r.BankRef, Direction: r.Direction,
			Amount: r.Amount, Currency: r.Currency, ValueDate: r.ValueDate,
			Counterparty: r.Counterparty, RemittanceInfo: r.RemittanceInfo,
			Source: r.Source, TrustedRef: r.TrustedRef, Disposition: r.Disposition,
			IrrelevantType: r.IrrelevantType, RecordedByName: r.RecordedByName,
			CreatedAt: ts(r.CreatedAt), AccountName: r.AccountName,
			AllocatedAmount: r.AllocatedAmount, UnallocatedAmount: r.UnallocatedAmount,
		})
	}
	return &exv1.ListTransactionsResponse{Transactions: out, Total: total}, nil
}

func (h *ReceiptHandler) GetTransaction(ctx context.Context, req *exv1.GetTransactionRequest) (*exv1.GetTransactionResponse, error) {
	view, err := h.svc.GetTransaction(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &exv1.GetTransactionResponse{
		Transaction: txToProto(view.Transaction),
		Allocations: allocationsToProto(view.Allocations),
		Suggestions: suggestionsToProto(view.Suggestions),
	}, nil
}

func (h *ReceiptHandler) RecordTransaction(ctx context.Context, req *exv1.RecordTransactionRequest) (*exv1.RecordTransactionResponse, error) {
	in := req.GetTransaction()
	view, err := h.svc.RecordTransaction(ctx, grpcx.TenantID(ctx), app.TransactionInput{
		AccountID: in.GetAccountId(), BankRef: in.GetBankRef(), Direction: in.GetDirection(),
		Amount: in.GetAmount(), Currency: in.GetCurrency(), ValueDate: in.GetValueDate(),
		Counterparty: in.GetCounterparty(), CounterpartyAccount: in.GetCounterpartyAccount(),
		RemittanceInfo: in.GetRemittanceInfo(), Source: in.GetSource(),
		TrustedRef: in.GetTrustedRef(), Note: in.GetNote(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.RecordTransactionResponse{Transaction: txToProto(view.Transaction)}, nil
}

func (h *ReceiptHandler) AllocateReceipt(ctx context.Context, req *exv1.AllocateReceiptRequest) (*exv1.AllocateReceiptResponse, error) {
	lines := make([]app.AllocationLine, 0, len(req.GetAllocations()))
	for _, a := range req.GetAllocations() {
		lines = append(lines, app.AllocationLine{
			ContractID: a.GetContractId(), Amount: a.GetAmount(), FeeAmount: a.GetFeeAmount(),
		})
	}
	view, err := h.svc.Allocate(ctx, grpcx.TenantID(ctx), req.GetTransactionId(), lines, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.AllocateReceiptResponse{
		Transaction: txToProto(view.Transaction),
		Allocations: allocationsToProto(view.Allocations),
	}, nil
}

func (h *ReceiptHandler) ReverseAllocation(ctx context.Context, req *exv1.ReverseAllocationRequest) (*exv1.ReverseAllocationResponse, error) {
	view, err := h.svc.ReverseAllocation(ctx, grpcx.TenantID(ctx),
		req.GetAllocationId(), req.GetReason(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.ReverseAllocationResponse{
		Transaction: txToProto(view.Transaction),
		Allocations: allocationsToProto(view.Allocations),
	}, nil
}

func (h *ReceiptHandler) MarkIrrelevant(ctx context.Context, req *exv1.MarkIrrelevantRequest) (*exv1.MarkIrrelevantResponse, error) {
	view, err := h.svc.MarkIrrelevant(ctx, grpcx.TenantID(ctx),
		req.GetTransactionId(), req.GetIrrelevantType(), req.GetNote(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.MarkIrrelevantResponse{Transaction: txToProto(view.Transaction)}, nil
}

func (h *ReceiptHandler) ReopenTransaction(ctx context.Context, req *exv1.ReopenTransactionRequest) (*exv1.ReopenTransactionResponse, error) {
	view, err := h.svc.ReopenTransaction(ctx, grpcx.TenantID(ctx), req.GetTransactionId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.ReopenTransactionResponse{Transaction: txToProto(view.Transaction)}, nil
}

func (h *ReceiptHandler) ListOpenReceivables(ctx context.Context, req *exv1.ListOpenReceivablesRequest) (*exv1.ListOpenReceivablesResponse, error) {
	rows, err := h.svc.OpenReceivables(ctx, grpcx.TenantID(ctx),
		req.GetCurrency(), req.GetCustomerId(), req.GetKeyword())
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.OpenReceivable, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.OpenReceivable{
			ContractId: r.ID, ContractNo: r.ContractNo,
			CustomerId: r.CustomerID, CustomerName: r.CustomerName,
			Currency: r.Currency, TotalAmount: r.TotalAmount,
			ReceivedAmount: r.ReceivedAmount, OpenAmount: r.OpenAmount,
		})
	}
	return &exv1.ListOpenReceivablesResponse{Receivables: out}, nil
}

// ListReceivableDue 是财务的催收清单（E1）。按人围栏——应收是钱的事，
// 谁能看见哪张合同的欠款和谁能看见哪张合同是同一个问题。
func (h *ReceiptHandler) ListReceivableDue(ctx context.Context, req *exv1.ListReceivableDueRequest) (*exv1.ListReceivableDueResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, total, err := h.svc.ListReceivableDue(ctx, grpcx.TenantID(ctx), app.ReceivableFilter{
		OverdueOnly: req.GetOverdueOnly(), UnsetOnly: req.GetUnsetOnly(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.ReceivableDue, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.ReceivableDue{
			ContractId: r.ContractID, ContractNo: r.ContractNo,
			CustomerId: r.CustomerID, CustomerName: r.CustomerName,
			SalesEmployeeId: r.SalesEmployeeID, SalesEmployee: r.SalesEmployee,
			DueDate: r.DueDate, EffectiveDate: r.EffectiveDate,
			Currency: r.Currency, TotalAmount: r.TotalAmount,
			ReceivedAmount: r.ReceivedAmount, OpenAmount: r.OpenAmount,
			OverdueDays: r.OverdueDays, DueUnset: r.DueUnset,
		})
	}
	return &exv1.ListReceivableDueResponse{Items: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

// ListReceivableReminders 是本人的应收提醒收件箱——按登录人隔离，
// 不需要额外围栏：提醒本来就是发给具体某个人的。
func (h *ReceiptHandler) ListReceivableReminders(ctx context.Context, req *exv1.ListReceivableRemindersRequest) (*exv1.ListReceivableRemindersResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, unread, err := h.svc.ReceivableInbox(ctx, grpcx.TenantID(ctx), op.EmployeeID, req.GetUnreadOnly(), req.GetLimit())
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.ReceivableReminder, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.ReceivableReminder{
			Id: r.ID, ContractId: r.ContractID, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, ReminderType: r.Type, PeriodNo: r.PeriodNo,
			DueDate: r.DueDate, OpenAmount: r.OpenAmount, Currency: r.Currency,
			Title: r.Title, Content: r.Content, DetailUrl: r.DetailURL,
			CreatedAt: r.CreatedAt, Unread: r.Unread,
		})
	}
	return &exv1.ListReceivableRemindersResponse{Items: out, UnreadTotal: unread}, nil
}

func (h *ReceiptHandler) MarkReceivableRemindersRead(ctx context.Context, req *exv1.MarkReceivableRemindersReadRequest) (*exv1.MarkReceivableRemindersReadResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	n, err := h.svc.MarkReceivableRemindersRead(ctx, grpcx.TenantID(ctx), op.EmployeeID, req.GetIds())
	if err != nil {
		return nil, err
	}
	return &exv1.MarkReceivableRemindersReadResponse{Marked: n}, nil
}

func (h *ReceiptHandler) GetContractReceipts(ctx context.Context, req *exv1.GetContractReceiptsRequest) (*exv1.GetContractReceiptsResponse, error) {
	progress, rows, err := h.svc.ContractReceipts(ctx, grpcx.TenantID(ctx), req.GetContractId())
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.ContractReceipt, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.ContractReceipt{
			AllocationId: r.ID, TransactionId: r.TransactionID,
			BankRef: r.BankRef, ValueDate: r.ValueDate, Counterparty: r.Counterparty,
			Source: r.Source, Amount: r.Amount, FeeAmount: r.FeeAmount,
			Currency: r.Currency, ReversalOf: r.ReversalOf,
			AllocatedByName: r.AllocatedByName,
		})
	}
	return &exv1.GetContractReceiptsResponse{
		Progress: &exv1.ReceiptProgress{
			ContractId: progress.ID, ContractNo: progress.ContractNo,
			CustomerName: progress.CustomerName, Currency: progress.Currency,
			TotalAmount: progress.TotalAmount, ReceivedAmount: progress.ReceivedAmount,
			OpenAmount: progress.OpenAmount,
		},
		Receipts: out,
	}, nil
}

func txToProto(r store.GetBankTransactionRow) *exv1.BankTransaction {
	return &exv1.BankTransaction{
		Id: r.ID, AccountId: r.AccountID, AccountName: r.AccountName,
		BankRef: r.BankRef, Direction: r.Direction, Amount: r.Amount,
		Currency: r.Currency, ValueDate: r.ValueDate,
		Counterparty: r.Counterparty, CounterpartyAccount: r.CounterpartyAccount,
		RemittanceInfo: r.RemittanceInfo, Source: r.Source, TrustedRef: r.TrustedRef,
		Disposition: r.Disposition, IrrelevantType: r.IrrelevantType, Note: r.Note,
		RecordedByName: r.RecordedByName, CreatedAt: ts(r.CreatedAt),
		AllocatedAmount: r.AllocatedAmount, UnallocatedAmount: r.UnallocatedAmount,
		FeeAmount: r.FeeAmount,
	}
}

func allocationsToProto(rows []store.ListAllocationsOfTransactionRow) []*exv1.ReceiptAllocation {
	out := make([]*exv1.ReceiptAllocation, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.ReceiptAllocation{
			Id: r.ID, ContractId: r.ContractID, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, Amount: r.Amount, FeeAmount: r.FeeAmount,
			Currency: r.Currency, ReversalOf: r.ReversalOf, ReverseReason: r.ReverseReason,
			AllocatedByName: r.AllocatedByName, AllocatedAt: ts(r.AllocatedAt),
		})
	}
	return out
}

func suggestionsToProto(rows []store.FindContractsByNoRow) []*exv1.ContractSuggestion {
	out := make([]*exv1.ContractSuggestion, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.ContractSuggestion{
			ContractId: r.ID, ContractNo: r.ContractNo, CustomerName: r.CustomerName,
			Currency: r.Currency, OpenAmount: r.OpenAmount,
		})
	}
	return out
}
