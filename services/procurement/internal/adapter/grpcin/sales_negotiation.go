package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func salesPlan(view app.SalesPlanView) *prv1.SalesPlan {
	h := view.Header
	items := make([]*prv1.SalesPlanItem, 0, len(view.Items))
	for _, row := range view.Items {
		items = append(items, &prv1.SalesPlanItem{Id: row.ID, SourcingLineId: row.SourcingLineID,
			ProcurementPlanItemId: row.ProcurementPlanItemID, ShippingPlanItemId: row.ShippingPlanItemID,
			OptionType: row.OptionType, Priority: row.Priority, ProductName: row.ProductName,
			QuotedQty: row.QuotedQty, UomCode: row.UomCode, CustomerCurrency: row.CustomerCurrency,
			CustomerUnitPrice: row.CustomerUnitPrice, PromisedDeliveryDate: row.PromisedDeliveryDate, LineNote: row.LineNote})
	}
	return &prv1.SalesPlan{Id: h.ID, CaseId: h.CaseID, PlanNo: h.PlanNo, VersionNo: h.VersionNo,
		RequirementVersionNo: h.RequirementVersionNo, ProcurementPlanId: h.ProcurementPlanID,
		ShippingPlanId: h.ShippingPlanID, Status: h.Status, ValidUntil: h.ValidUntil,
		CustomerNote: h.CustomerNote, InternalNote: h.InternalNote, CreatedBy: h.CreatedBy,
		CreatedByName: h.CreatedByName, PresentedAt: ts(h.PresentedAt), CreatedAt: ts(h.CreatedAt), Items: items}
}

func customerFeedback(row store.ListCustomerFeedbackRow) *prv1.CustomerFeedback {
	return &prv1.CustomerFeedback{Id: row.ID, CaseId: row.CaseID, SalesPlanId: row.SalesPlanID,
		ContactName: row.ContactName, Channel: row.Channel, Result: row.Result, Summary: row.Summary,
		ContactedAt: ts(row.ContactedAt), CreatedBy: row.CreatedBy, CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt)}
}

func shippingRework(row store.ListShippingReworksRow) *prv1.ShippingReworkRequest {
	return &prv1.ShippingReworkRequest{Id: row.ID, CaseId: row.CaseID, SalesPlanId: row.SalesPlanID,
		SourcingLineId: row.SourcingLineID, ShippingOptionLineId: row.ShippingOptionLineID,
		RequestType: row.RequestType, ScopeType: row.ScopeType, AssignedShippingId: row.AssignedShippingID,
		AssignedShippingName: row.AssignedShippingName, CarrierForwarder: row.CarrierForwarder,
		ProductName: row.ProductName, Reason: row.Reason, Status: row.Status, CreatedBy: row.CreatedBy,
		CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt), ResolvedBy: row.ResolvedBy,
		ResolvedByName: row.ResolvedByName, ResolvedAt: ts(row.ResolvedAt), ResolutionNote: row.ResolutionNote}
}

func myShippingRework(row store.ListMyShippingReworksRow) *prv1.ShippingReworkRequest {
	return &prv1.ShippingReworkRequest{Id: row.ID, CaseId: row.CaseID, SalesPlanId: row.SalesPlanID,
		SourcingLineId: row.SourcingLineID, ShippingOptionLineId: row.ShippingOptionLineID,
		RequestType: row.RequestType, ScopeType: row.ScopeType, AssignedShippingId: row.AssignedShippingID,
		AssignedShippingName: row.AssignedShippingName, CarrierForwarder: row.CarrierForwarder,
		ProductName: row.ProductName, Reason: row.Reason, Status: row.Status, CreatedBy: row.CreatedBy,
		CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt), ResolvedBy: row.ResolvedBy,
		ResolvedByName: row.ResolvedByName, ResolvedAt: ts(row.ResolvedAt), ResolutionNote: row.ResolutionNote,
		CaseNo: row.CaseNo, CaseTitle: row.CaseTitle}
}

func (h *SourcingHandler) CreateSalesPlan(ctx context.Context, req *prv1.CreateSalesPlanRequest) (*prv1.CreateSalesPlanResponse, error) {
	in := app.NewSalesPlan{CaseID: req.GetCaseId(), ProcurementPlanID: req.GetProcurementPlanId(),
		ShippingPlanID: req.GetShippingPlanId(), ValidUntil: req.GetValidUntil(),
		CustomerNote: req.GetCustomerNote(), InternalNote: req.GetInternalNote()}
	for _, row := range req.GetItems() {
		in.Items = append(in.Items, app.SalesPlanItemInput{SourcingLineID: row.GetSourcingLineId(),
			ProcurementPlanItemID: row.GetProcurementPlanItemId(), ShippingPlanItemID: row.GetShippingPlanItemId(),
			OptionType: row.GetOptionType(), Priority: row.GetPriority(), CustomerCurrency: row.GetCustomerCurrency(),
			CustomerUnitPrice: row.GetCustomerUnitPrice(), PromisedDeliveryDate: row.GetPromisedDeliveryDate(), LineNote: row.GetLineNote()})
	}
	view, err := h.svc.CreateSalesPlan(ctx, grpcx.TenantID(ctx), in, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSalesPlanResponse{SalesPlan: salesPlan(view)}, nil
}

func (h *SourcingHandler) ListSalesPlans(ctx context.Context, req *prv1.ListSalesPlansRequest) (*prv1.ListSalesPlansResponse, error) {
	rows, err := h.svc.ListSalesPlans(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SalesPlan, 0, len(rows))
	for _, row := range rows {
		out = append(out, salesPlan(row))
	}
	return &prv1.ListSalesPlansResponse{SalesPlans: out}, nil
}

func (h *SourcingHandler) AddCustomerFeedback(ctx context.Context, req *prv1.AddCustomerFeedbackRequest) (*prv1.AddCustomerFeedbackResponse, error) {
	rows, err := h.svc.AddCustomerFeedback(ctx, grpcx.TenantID(ctx), app.CustomerFeedbackInput{CaseID: req.GetCaseId(),
		SalesPlanID: req.GetSalesPlanId(), ContactName: req.GetContactName(), Channel: req.GetChannel(),
		Result: req.GetResult(), Summary: req.GetSummary(), ContactedAt: req.GetContactedAt()}, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.CustomerFeedback, 0, len(rows))
	for _, row := range rows {
		out = append(out, customerFeedback(row))
	}
	return &prv1.AddCustomerFeedbackResponse{Feedback: out}, nil
}

func (h *SourcingHandler) ListCustomerFeedback(ctx context.Context, req *prv1.ListCustomerFeedbackRequest) (*prv1.ListCustomerFeedbackResponse, error) {
	rows, err := h.svc.ListCustomerFeedback(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.CustomerFeedback, 0, len(rows))
	for _, row := range rows {
		out = append(out, customerFeedback(row))
	}
	return &prv1.ListCustomerFeedbackResponse{Feedback: out}, nil
}

func (h *SourcingHandler) CreateSalesProcurementRework(ctx context.Context, req *prv1.CreateSalesProcurementReworkRequest) (*prv1.CreateSalesProcurementReworkResponse, error) {
	row, err := h.svc.CreateSalesProcurementRework(ctx, grpcx.TenantID(ctx), req.GetSalesPlanId(), app.NewProcurementRework{
		CaseID: req.GetCaseId(), SourcingLineID: req.GetSourcingLineId(), SupplierQuoteLineID: req.GetSupplierQuoteLineId(),
		RequestType: req.GetRequestType(), Reason: req.GetReason()}, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSalesProcurementReworkResponse{ReworkRequest: procurementRework(row)}, nil
}

func (h *SourcingHandler) CreateShippingRework(ctx context.Context, req *prv1.CreateShippingReworkRequest) (*prv1.CreateShippingReworkResponse, error) {
	row, err := h.svc.CreateShippingRework(ctx, grpcx.TenantID(ctx), app.ShippingReworkInput{CaseID: req.GetCaseId(),
		SalesPlanID: req.GetSalesPlanId(), SourcingLineID: req.GetSourcingLineId(), ShippingOptionLineID: req.GetShippingOptionLineId(),
		RequestType: req.GetRequestType(), Reason: req.GetReason()}, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.CreateShippingReworkResponse{ReworkRequest: shippingRework(row)}, nil
}

func (h *SourcingHandler) ListShippingReworks(ctx context.Context, req *prv1.ListShippingReworksRequest) (*prv1.ListShippingReworksResponse, error) {
	rows, err := h.svc.ListShippingReworks(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.ShippingReworkRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, shippingRework(row))
	}
	return &prv1.ListShippingReworksResponse{ReworkRequests: out}, nil
}

func (h *SourcingHandler) ListMyShippingReworks(ctx context.Context, _ *prv1.ListMyShippingReworksRequest) (*prv1.ListMyShippingReworksResponse, error) {
	rows, err := h.svc.ListMyShippingReworks(ctx, grpcx.TenantID(ctx), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.ShippingReworkRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, myShippingRework(row))
	}
	return &prv1.ListMyShippingReworksResponse{ReworkRequests: out}, nil
}

func (h *SourcingHandler) ResolveShippingRework(ctx context.Context, req *prv1.ResolveShippingReworkRequest) (*prv1.ResolveShippingReworkResponse, error) {
	if err := h.svc.ResolveShippingRework(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetResolutionNote(), sourcingOperator(ctx)); err != nil {
		return nil, err
	}
	return &prv1.ResolveShippingReworkResponse{}, nil
}
