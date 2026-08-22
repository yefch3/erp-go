package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// Supplier invoices ride on the order handler: an invoice is meaningless
// except against orders, and splitting it into its own service would only
// add a registration for the same permission boundary.

func (h *OrderHandler) CreateSupplierInvoice(ctx context.Context, req *prv1.CreateSupplierInvoiceRequest) (*prv1.CreateSupplierInvoiceResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.SupplierInvoiceLineInput, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		lines = append(lines, app.SupplierInvoiceLineInput{
			POID: l.GetPoId(), POItemID: l.GetPoItemId(),
			Description: l.GetDescription(), Qty: l.GetQty(),
			UnitPrice: l.GetUnitPrice(), Amount: l.GetAmount(),
		})
	}
	v, err := h.svc.CreateSupplierInvoice(ctx, grpcx.TenantID(ctx), app.SupplierInvoiceInput{
		SupplierID: req.GetSupplierId(), SupplierCode: req.GetSupplierCode(),
		SupplierName: req.GetSupplierName(), InvoiceNo: req.GetInvoiceNo(),
		InvoiceType: req.GetInvoiceType(), Currency: req.GetCurrency(),
		TotalAmount: req.GetTotalAmount(), TaxAmount: req.GetTaxAmount(),
		InvoiceDate: req.GetInvoiceDate(), DueDate: req.GetDueDate(),
		Lines: lines,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSupplierInvoiceResponse{Invoice: supplierInvoiceProto(v)}, nil
}

func (h *OrderHandler) ListSupplierInvoices(ctx context.Context, req *prv1.ListSupplierInvoicesRequest) (*prv1.ListSupplierInvoicesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	items, total, err := h.svc.ListSupplierInvoices(ctx, grpcx.TenantID(ctx),
		app.SupplierInvoiceFilter{
			SupplierID: req.GetSupplierId(), Status: req.GetStatus(),
			MatchStatus: req.GetMatchStatus(), Keyword: req.GetKeyword(),
		}, req.GetPage().GetPage(), req.GetPage().GetPageSize(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SupplierInvoice, 0, len(items))
	for _, v := range items {
		out = append(out, supplierInvoiceProto(v))
	}
	return &prv1.ListSupplierInvoicesResponse{Items: out, Total: total}, nil
}

func (h *OrderHandler) GetSupplierInvoice(ctx context.Context, req *prv1.GetSupplierInvoiceRequest) (*prv1.GetSupplierInvoiceResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.AuthorizeSupplierInvoice(ctx, grpcx.TenantID(ctx), req.GetId(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	v, err := h.svc.GetSupplierInvoice(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetSupplierInvoiceResponse{Invoice: supplierInvoiceProto(v)}, nil
}

func (h *OrderHandler) VoidSupplierInvoice(ctx context.Context, req *prv1.VoidSupplierInvoiceRequest) (*prv1.VoidSupplierInvoiceResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.VoidSupplierInvoice(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetReason(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.VoidSupplierInvoiceResponse{Invoice: supplierInvoiceProto(v)}, nil
}

func supplierInvoiceProto(v app.SupplierInvoice) *prv1.SupplierInvoice {
	lines := make([]*prv1.SupplierInvoiceLine, 0, len(v.Lines))
	for _, l := range v.Lines {
		lines = append(lines, &prv1.SupplierInvoiceLine{
			Id: l.ID, PoId: l.POID, PoItemId: l.POItemID, PoNo: l.PONo,
			Description: l.Description, Qty: l.Qty,
			UnitPrice: l.UnitPrice, Amount: l.Amount,
		})
	}
	return &prv1.SupplierInvoice{
		Id: v.ID, SupplierId: v.SupplierID, SupplierCode: v.SupplierCode,
		SupplierName: v.SupplierName, InvoiceNo: v.InvoiceNo,
		InvoiceType: v.InvoiceType, Currency: v.Currency,
		TotalAmount: v.TotalAmount, TaxAmount: v.TaxAmount,
		InvoiceDate: v.InvoiceDate, DueDate: v.DueDate,
		MatchStatus: v.MatchStatus, MatchNote: v.MatchNote,
		Status: v.Status, VoidReason: v.VoidReason,
		CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt,
		Lines:         lines,
		AttachmentKey: v.AttachmentKey, AttachmentUrl: v.AttachmentURL,
		AttachmentName: v.AttachmentName,
	}
}

func (h *OrderHandler) PresignSupplierInvoiceFile(ctx context.Context, req *prv1.PresignSupplierInvoiceFileRequest) (*prv1.PresignSupplierInvoiceFileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	p, err := h.svc.PresignSupplierInvoiceFile(ctx, grpcx.TenantID(ctx),
		req.GetInvoiceId(), req.GetFileName(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.PresignSupplierInvoiceFileResponse{
		Key: p.Key, UploadUrl: p.UploadURL, ExpiresSeconds: p.Expires,
	}, nil
}

func (h *OrderHandler) AttachSupplierInvoiceFile(ctx context.Context, req *prv1.AttachSupplierInvoiceFileRequest) (*prv1.AttachSupplierInvoiceFileResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.AttachSupplierInvoiceFile(ctx, grpcx.TenantID(ctx),
		req.GetInvoiceId(), req.GetKey(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.AttachSupplierInvoiceFileResponse{Invoice: supplierInvoiceProto(v)}, nil
}

func (h *OrderHandler) MatchSupplierInvoice(ctx context.Context, req *prv1.MatchSupplierInvoiceRequest) (*prv1.MatchSupplierInvoiceResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.MatchSupplierInvoice(ctx, grpcx.TenantID(ctx), req.GetId(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.MatchSupplierInvoiceResponse{Invoice: supplierInvoiceProto(v)}, nil
}
