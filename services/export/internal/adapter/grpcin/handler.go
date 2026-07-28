// Package grpcin exposes quotations over gRPC.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/export/internal/app"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

type Handler struct {
	exv1.UnimplementedQuotationServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListQuotations(ctx context.Context, req *exv1.ListQuotationsRequest) (*exv1.ListQuotationsResponse, error) {
	rows, total, err := h.svc.ListQuotations(ctx, grpcx.TenantID(ctx), req.GetKeyword(),
		req.GetCustomerId(), req.GetStatus(), req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.Quotation, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.Quotation{
			Id: r.ID, QuoteNo: r.QuoteNo, CustomerId: r.CustomerID, CustomerName: r.CustomerName,
			Currency: r.Currency, TotalAmount: r.TotalAmount, BaseAmount: r.BaseAmount,
			Status: r.Status, SalesEmployee: r.SalesEmployee, ValidUntil: r.ValidUntil,
			CreatedAt: ts(r.CreatedAt),
		})
	}
	return &exv1.ListQuotationsResponse{Quotations: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *Handler) GetQuotation(ctx context.Context, req *exv1.GetQuotationRequest) (*exv1.GetQuotationResponse, error) {
	q, items, err := h.svc.GetQuotation(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &exv1.GetQuotationResponse{Quotation: quotationToProto(q), Items: itemsToProto(items)}, nil
}

func (h *Handler) CreateQuotation(ctx context.Context, req *exv1.CreateQuotationRequest) (*exv1.CreateQuotationResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	q, items, err := h.svc.CreateQuotation(ctx, grpcx.TenantID(ctx), app.QuotationInput{
		CustomerID: req.GetCustomerId(), ContactID: req.GetContactId(),
		Currency: req.GetCurrency(), Incoterm: req.GetIncoterm(),
		PortOfLoading: req.GetPortOfLoading(), PortOfDischarge: req.GetPortOfDischarge(),
		PaymentMethod: req.GetPaymentMethod(), ValidUntil: req.GetValidUntil(),
		Remark: req.GetRemark(), Items: itemsFromProto(req.GetItems()),
		OperatorID: op.EmployeeID, OperatorName: op.Name,
	})
	if err != nil {
		return nil, err
	}
	return &exv1.CreateQuotationResponse{Quotation: quotationToProto(q), Items: itemsToProto(items)}, nil
}

func (h *Handler) UpdateQuotation(ctx context.Context, req *exv1.UpdateQuotationRequest) (*exv1.UpdateQuotationResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	q, items, err := h.svc.UpdateQuotation(ctx, grpcx.TenantID(ctx), req.GetId(), app.QuotationInput{
		CustomerID: req.GetCustomerId(), ContactID: req.GetContactId(),
		Currency: req.GetCurrency(), Incoterm: req.GetIncoterm(),
		PortOfLoading: req.GetPortOfLoading(), PortOfDischarge: req.GetPortOfDischarge(),
		PaymentMethod: req.GetPaymentMethod(), ValidUntil: req.GetValidUntil(),
		Remark: req.GetRemark(), Items: itemsFromProto(req.GetItems()),
		OperatorID: op.EmployeeID, OperatorName: op.Name,
	})
	if err != nil {
		return nil, err
	}
	return &exv1.UpdateQuotationResponse{Quotation: quotationToProto(q), Items: itemsToProto(items)}, nil
}

func (h *Handler) SendQuotation(ctx context.Context, req *exv1.SendQuotationRequest) (*exv1.SendQuotationResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	status, err := h.svc.Send(ctx, grpcx.TenantID(ctx), req.GetId(), op.EmployeeID)
	if err != nil {
		return nil, err
	}
	return &exv1.SendQuotationResponse{Status: status}, nil
}

func (h *Handler) RespondQuotation(ctx context.Context, req *exv1.RespondQuotationRequest) (*exv1.RespondQuotationResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	status, err := h.svc.Respond(ctx, grpcx.TenantID(ctx), req.GetId(), op.EmployeeID, req.GetStatus())
	if err != nil {
		return nil, err
	}
	return &exv1.RespondQuotationResponse{Status: status}, nil
}

func (h *Handler) CancelQuotation(ctx context.Context, req *exv1.CancelQuotationRequest) (*exv1.CancelQuotationResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	status, err := h.svc.Cancel(ctx, grpcx.TenantID(ctx), req.GetId(), op.EmployeeID)
	if err != nil {
		return nil, err
	}
	return &exv1.CancelQuotationResponse{Status: status}, nil
}

// ---------------------------------------------------------------- mapping

func ts(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func itemsFromProto(in []*exv1.ItemInput) []app.ItemInput {
	out := make([]app.ItemInput, 0, len(in))
	for _, i := range in {
		out = append(out, app.ItemInput{
			ProductID: i.GetProductId(), SkuID: i.GetSkuId(), Spec: i.GetSpec(),
			Qty: i.GetQty(), UnitPrice: i.GetUnitPrice(), Remark: i.GetRemark(),
		})
	}
	return out
}

func quotationToProto(q store.GetQuotationRow) *exv1.Quotation {
	return &exv1.Quotation{
		Id: q.ID, QuoteNo: q.QuoteNo, CustomerId: q.CustomerID, CustomerName: q.CustomerName,
		ContactId: q.ContactID, ContactName: q.ContactName, ContactEmail: q.ContactEmail,
		Currency: q.Currency, Incoterm: q.Incoterm, PortOfLoading: q.PortOfLoading,
		PortOfDischarge: q.PortOfDischarge, PaymentMethod: q.PaymentMethod,
		ValidUntil: q.ValidUntil,
		Fx: &exv1.FxSnapshot{
			Rate: q.FxRate, RateAt: ts(q.FxRateAt), Source: q.FxSource, BaseCurrency: q.FxBaseCurrency,
		},
		TotalAmount: q.TotalAmount, BaseAmount: q.BaseAmount, Remark: q.Remark,
		Status: q.Status, SalesEmployeeId: q.SalesEmployeeID, SalesEmployee: q.SalesEmployee,
		SentAt: ts(q.SentAt), RespondedAt: ts(q.RespondedAt), CreatedAt: ts(q.CreatedAt),
	}
}

func itemsToProto(items []store.ListQuotationItemsRow) []*exv1.QuotationItem {
	out := make([]*exv1.QuotationItem, 0, len(items))
	for _, i := range items {
		var sku int64
		if i.SkuID != nil {
			sku = *i.SkuID
		}
		out = append(out, &exv1.QuotationItem{
			Id: i.ID, LineNo: i.LineNo, ProductId: i.ProductID, SkuId: sku,
			ProductCode: i.ProductCode, ProductName: i.ProductName, Spec: i.Spec,
			Qty: i.Qty, UomId: i.UomID, UomCode: i.UomCode,
			UnitPrice: i.UnitPrice, Amount: i.Amount, Remark: i.Remark,
		})
	}
	return out
}
