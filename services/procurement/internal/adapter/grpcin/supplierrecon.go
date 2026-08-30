package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// 供应商对账（手工核销）骑在订单 handler 上，和发票、付款、往来汇总一样：
// 读的是同一本账，走的是同一道边界。

func (h *OrderHandler) ListSupplierRecon(ctx context.Context, req *prv1.ListSupplierReconRequest) (*prv1.ListSupplierReconResponse, error) {
	items, total, err := h.svc.ListSupplierRecon(ctx, grpcx.TenantID(ctx),
		app.SupplierReconFilter{
			Keyword:     req.GetKeyword(),
			ClosedOnly:  req.GetClosedOnly(),
			OverdueOnly: req.GetOverdueOnly(),
			UnsetOnly:   req.GetUnsetOnly(),
			Page:        req.GetPage(),
			PageSize:    req.GetPageSize(),
		}, reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SupplierReconRow, 0, len(items))
	for _, v := range items {
		out = append(out, reconRowProto(v))
	}
	return &prv1.ListSupplierReconResponse{Items: out, Total: total}, nil
}

func (h *OrderHandler) ListPurchaseOrderPayments(ctx context.Context, req *prv1.ListPurchaseOrderPaymentsRequest) (*prv1.ListPurchaseOrderPaymentsResponse, error) {
	entries, err := h.svc.ListPOPayments(ctx, grpcx.TenantID(ctx), req.GetPoId(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.PurchaseOrderPaymentEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &prv1.PurchaseOrderPaymentEntry{
			AllocationId: e.AllocationID, PaidAt: e.PaidAt,
			Amount: e.Amount, FeeAmount: e.FeeAmount, Currency: e.Currency,
			Note: e.Note, AllocatedAt: e.AllocatedAt, AllocatedBy: e.AllocatedBy,
			ReversalOf: e.ReversalOf, ReverseReason: e.ReverseReason,
			PaymentNo: e.PaymentNo, InvoiceNo: e.InvoiceNo,
		})
	}
	return &prv1.ListPurchaseOrderPaymentsResponse{Items: out}, nil
}

func (h *OrderHandler) RecordPurchaseOrderPayment(ctx context.Context, req *prv1.RecordPurchaseOrderPaymentRequest) (*prv1.RecordPurchaseOrderPaymentResponse, error) {
	row, err := h.svc.RecordPOPayment(ctx, grpcx.TenantID(ctx), app.POPaymentInput{
		POID: req.GetPoId(), Amount: req.GetAmount(), IsRefund: req.GetIsRefund(),
		PaidAt: req.GetPaidAt(), Note: req.GetNote(),
	}, reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.RecordPurchaseOrderPaymentResponse{Row: reconRowProto(row)}, nil
}

func (h *OrderHandler) ReversePurchaseOrderPayment(ctx context.Context, req *prv1.ReversePurchaseOrderPaymentRequest) (*prv1.ReversePurchaseOrderPaymentResponse, error) {
	row, err := h.svc.ReversePOPayment(ctx, grpcx.TenantID(ctx),
		req.GetPoId(), req.GetAllocationId(), req.GetReason(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ReversePurchaseOrderPaymentResponse{Row: reconRowProto(row)}, nil
}

func (h *OrderHandler) ClosePurchaseOrderPayment(ctx context.Context, req *prv1.ClosePurchaseOrderPaymentRequest) (*prv1.ClosePurchaseOrderPaymentResponse, error) {
	row, err := h.svc.ClosePOPayment(ctx, grpcx.TenantID(ctx),
		req.GetPoId(), req.GetCategory(), req.GetNote(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ClosePurchaseOrderPaymentResponse{Row: reconRowProto(row)}, nil
}

func (h *OrderHandler) ReopenPurchaseOrderPayment(ctx context.Context, req *prv1.ReopenPurchaseOrderPaymentRequest) (*prv1.ReopenPurchaseOrderPaymentResponse, error) {
	row, err := h.svc.ReopenPOPayment(ctx, grpcx.TenantID(ctx),
		req.GetPoId(), req.GetReason(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ReopenPurchaseOrderPaymentResponse{Row: reconRowProto(row)}, nil
}

// BackfillPayableDue 已下线：它按「供应商配的账期」推算存量单的到期日，
// 而到期日现在是单据自己的字段、由人填，没有可推导的来源了。存量单要补，
// 走对账页上的「改到期日」，一张一张地填，理由留痕。
//
// 空壳保留只因为 buf 的兼容检查不允许删 RPC。网关已经不挂这条路由，所以
// 正常情况下没有人能走到这里。
func (h *OrderHandler) BackfillPayableDue(context.Context, *prv1.BackfillPayableDueRequest) (*prv1.BackfillPayableDueResponse, error) {
	return nil, apierr.Conflict("PR_BACKFILL_RETIRED",
		"批量补算已下线：应付到期日现在由建单的人填，没有可以推算的账期了")
}

func (h *OrderHandler) SetPayableDueDate(ctx context.Context, req *prv1.SetPayableDueDateRequest) (*prv1.SetPayableDueDateResponse, error) {
	row, err := h.svc.SetPOPayableDue(ctx, grpcx.TenantID(ctx),
		req.GetPoId(), req.GetDueDate(), req.GetReason(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.SetPayableDueDateResponse{Row: reconRowProto(row)}, nil
}

func reconOperator(ctx context.Context) app.Operator {
	op, _ := grpcx.OperatorFromContext(ctx)
	return app.Operator{ID: op.EmployeeID, Name: op.Name}
}

func reconRowProto(v app.SupplierReconRow) *prv1.SupplierReconRow {
	return &prv1.SupplierReconRow{
		PoId: v.POID, PoNo: v.PONo,
		SupplierId: v.SupplierID, SupplierName: v.SupplierName,
		Currency: v.Currency, OrderStatus: v.OrderStatus, BuyerName: v.BuyerName,
		OrderedDate: v.OrderedDate, ExpectedDate: v.ExpectedDate,
		DueDate: v.DueDate, OverdueDays: v.OverdueDays, DueUnset: v.DueUnset,
		OrderedAmount: v.OrderedAmount, PaidAmount: v.PaidAmount, OpenAmount: v.OpenAmount,
		InvoicePaidAmount: v.InvoicePaidAmount,
		ClosedCategory:    v.ClosedCategory, ClosedNote: v.ClosedNote,
		ClosedByName: v.ClosedByName, ClosedAt: v.ClosedAt,
	}
}

// ── 对账页上的凭证 ────────────────────────────────────────

func (h *OrderHandler) PresignReconFile(ctx context.Context, req *prv1.PresignReconFileRequest) (*prv1.PresignReconFileResponse, error) {
	p, err := h.svc.PresignReconFile(ctx, grpcx.TenantID(ctx),
		req.GetPoId(), req.GetFileName(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.PresignReconFileResponse{
		Key: p.Key, UploadUrl: p.UploadURL, ExpiresSeconds: p.Expires,
	}, nil
}

func (h *OrderHandler) AttachReconFile(ctx context.Context, req *prv1.AttachReconFileRequest) (*prv1.AttachReconFileResponse, error) {
	files, err := h.svc.AttachReconFile(ctx, grpcx.TenantID(ctx),
		req.GetPoId(), req.GetKey(), req.GetFileName(), req.GetNote(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.AttachReconFileResponse{Items: reconFilesProto(files)}, nil
}

func (h *OrderHandler) ListReconFiles(ctx context.Context, req *prv1.ListReconFilesRequest) (*prv1.ListReconFilesResponse, error) {
	files, err := h.svc.ListReconFiles(ctx, grpcx.TenantID(ctx), req.GetPoId(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ListReconFilesResponse{Items: reconFilesProto(files)}, nil
}

func (h *OrderHandler) RemoveReconFile(ctx context.Context, req *prv1.RemoveReconFileRequest) (*prv1.RemoveReconFileResponse, error) {
	files, err := h.svc.RemoveReconFile(ctx, grpcx.TenantID(ctx), req.GetFileId(), reconOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.RemoveReconFileResponse{Items: reconFilesProto(files)}, nil
}

func reconFilesProto(files []app.ReconFile) []*prv1.ReconFile {
	out := make([]*prv1.ReconFile, 0, len(files))
	for _, f := range files {
		out = append(out, &prv1.ReconFile{
			Id: f.ID, FileName: f.FileName, Note: f.Note, Url: f.URL,
			UploadedByName: f.UploadedByName, UploadedAt: f.UploadedAt,
		})
	}
	return out
}
