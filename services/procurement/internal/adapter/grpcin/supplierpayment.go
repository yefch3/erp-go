package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// Payments ride on the order handler like invoices do: one spend chain, one
// permission boundary per document type, no extra service registration.

func (h *OrderHandler) CreateSupplierPayment(ctx context.Context, req *prv1.CreateSupplierPaymentRequest) (*prv1.CreateSupplierPaymentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.CreateSupplierPayment(ctx, grpcx.TenantID(ctx), app.SupplierPaymentInput{
		SupplierID: req.GetSupplierId(), SupplierName: req.GetSupplierName(),
		PaymentType: req.GetPaymentType(), Currency: req.GetCurrency(),
		Amount: req.GetAmount(), PaidAt: req.GetPaidAt(),
		Method: req.GetMethod(), BankRef: req.GetBankRef(), Remark: req.GetRemark(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSupplierPaymentResponse{Payment: supplierPaymentProto(v)}, nil
}

func (h *OrderHandler) ListSupplierPayments(ctx context.Context, req *prv1.ListSupplierPaymentsRequest) (*prv1.ListSupplierPaymentsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	items, total, err := h.svc.ListSupplierPayments(ctx, grpcx.TenantID(ctx),
		app.SupplierPaymentFilter{
			SupplierID: req.GetSupplierId(), PaymentType: req.GetPaymentType(),
			Keyword: req.GetKeyword(),
		}, req.GetPage().GetPage(), req.GetPage().GetPageSize(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SupplierPayment, 0, len(items))
	for _, v := range items {
		out = append(out, supplierPaymentProto(v))
	}
	return &prv1.ListSupplierPaymentsResponse{Items: out, Total: total}, nil
}

func (h *OrderHandler) GetSupplierPayment(ctx context.Context, req *prv1.GetSupplierPaymentRequest) (*prv1.GetSupplierPaymentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	tenantID := grpcx.TenantID(ctx)
	if err := h.svc.AuthorizeSupplierPayment(ctx, tenantID, req.GetId(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	v, err := h.svc.GetSupplierPayment(ctx, tenantID, req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetSupplierPaymentResponse{Payment: supplierPaymentProto(v)}, nil
}

func (h *OrderHandler) AllocateSupplierPayment(ctx context.Context, req *prv1.AllocateSupplierPaymentRequest) (*prv1.AllocateSupplierPaymentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.PaymentAllocationInput, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		lines = append(lines, app.PaymentAllocationInput{
			InvoiceID: l.GetInvoiceId(), POID: l.GetPoId(),
			Amount: l.GetAmount(), FeeAmount: l.GetFeeAmount(),
		})
	}
	v, err := h.svc.AllocateSupplierPayment(ctx, grpcx.TenantID(ctx), req.GetId(), lines,
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.AllocateSupplierPaymentResponse{Payment: supplierPaymentProto(v)}, nil
}

func (h *OrderHandler) ReverseSupplierPaymentAllocation(ctx context.Context, req *prv1.ReverseSupplierPaymentAllocationRequest) (*prv1.ReverseSupplierPaymentAllocationResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.ReverseSupplierPaymentAllocation(ctx, grpcx.TenantID(ctx),
		req.GetAllocationId(), req.GetReason(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ReverseSupplierPaymentAllocationResponse{Payment: supplierPaymentProto(v)}, nil
}

func supplierPaymentProto(v app.SupplierPayment) *prv1.SupplierPayment {
	allocs := make([]*prv1.PaymentAllocation, 0, len(v.Allocations))
	for _, a := range v.Allocations {
		allocs = append(allocs, &prv1.PaymentAllocation{
			Id: a.ID, InvoiceId: a.InvoiceID, InvoiceNo: a.InvoiceNo,
			PoId: a.POID, PoNo: a.PONo, Amount: a.Amount, FeeAmount: a.FeeAmount,
			Currency: a.Currency, ReversalOf: a.ReversalOf,
			ReverseReason: a.ReverseReason, AllocatedBy: a.AllocatedBy,
			AllocatedAt: a.AllocatedAt,
		})
	}
	return &prv1.SupplierPayment{
		Id: v.ID, SupplierId: v.SupplierID, SupplierName: v.SupplierName,
		PaymentNo: v.PaymentNo, PaymentType: v.PaymentType,
		Currency: v.Currency, Amount: v.Amount, Unallocated: v.Unallocated,
		PaidAt: v.PaidAt, Method: v.Method, BankRef: v.BankRef, Remark: v.Remark,
		CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, Allocations: allocs,
		BaseCurrency: v.BaseCurrency, BaseAmount: v.BaseAmount, FxRate: v.FxRate,
	}
}
