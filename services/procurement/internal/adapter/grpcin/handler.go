// Package grpcin exposes procurement over gRPC and maps store rows to
// protobuf. Quantities travel as strings: they are decimals, and a float
// would round them.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type Handler struct {
	prv1.UnimplementedRequirementServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListRequirements(ctx context.Context, req *prv1.ListRequirementsRequest) (*prv1.ListRequirementsResponse, error) {
	rows, total, err := h.svc.ListRequirements(ctx, grpcx.TenantID(ctx), app.RequirementFilter{
		Status: req.GetStatus(), ContractID: req.GetContractId(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.Requirement, 0, len(rows))
	for _, r := range rows {
		out = append(out, &prv1.Requirement{
			Id: r.ID, ContractId: r.ContractID, ContractNo: r.ContractNo,
			ContractVersionId: r.ContractVersionID, VersionNo: r.VersionNo,
			ContractItemId: r.ContractItemID, CustomerName: r.CustomerName,
			ProductId: r.ProductID, SkuId: r.SkuID,
			ProductCode: r.ProductCode, ProductName: r.ProductName, Spec: r.Spec,
			UomId: r.UomID, UomCode: r.UomCode,
			RequiredQty: r.RequiredQty, OrderedQty: r.OrderedQty, ReceivedQty: r.ReceivedQty,
			RequiredDate: r.RequiredDate, Source: r.Source, Status: r.Status,
			ClosedReason: r.ClosedReason, CreatedAt: ts(r.CreatedAt),
			QuotationId: r.QuotationID, QuotationNo: r.QuotationNo,
			CostScenarioId: r.CostScenarioID, CostScenarioNo: r.CostScenarioNo,
			SourcingCaseId: r.SourcingCaseID, SourcingLineId: r.SourcingLineID,
			SupplierQuoteLineId: r.SupplierQuoteLineID,
			SupplierId:          r.SupplierID, SupplierCode: r.SupplierCode, SupplierName: r.SupplierName,
			FactoryId: r.FactoryID, FactoryCode: r.FactoryCode, FactoryName: r.FactoryName,
			SourceCurrency: r.SourceCurrency, SourceUnitPrice: r.SourceUnitPrice,
			Moq: r.Moq, LeadTime: r.LeadTime,
		})
	}
	return &prv1.ListRequirementsResponse{
		Requirements: out, Meta: &commonv1.PageMeta{Total: total},
	}, nil
}

func (h *Handler) ExportPurchaseTemplate(ctx context.Context, req *prv1.ExportPurchaseTemplateRequest) (*prv1.ExportPurchaseTemplateResponse, error) {
	book, err := h.svc.ExportPurchaseTemplate(ctx, grpcx.TenantID(ctx), req.GetRequirementIds())
	if err != nil {
		return nil, err
	}
	return &prv1.ExportPurchaseTemplateResponse{FileName: book.FileName, FileData: book.Data, TemplateVersion: book.Version}, nil
}

func (h *Handler) CreateRequirement(ctx context.Context, req *prv1.CreateRequirementRequest) (*prv1.CreateRequirementResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.CreateRequirement(ctx, grpcx.TenantID(ctx), app.ManualRequirement{
		ProductID: req.GetProductId(), SkuID: req.GetSkuId(),
		ProductCode: req.GetProductCode(), ProductName: req.GetProductName(),
		Spec: req.GetSpec(), UomID: req.GetUomId(), UomCode: req.GetUomCode(),
		RequiredQty: req.GetRequiredQty(), RequiredDate: req.GetRequiredDate(),
		Remark: req.GetRemark(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateRequirementResponse{Requirement: requirementToProto(row)}, nil
}

func (h *Handler) GetRequirement(ctx context.Context, req *prv1.GetRequirementRequest) (*prv1.GetRequirementResponse, error) {
	r, err := h.svc.GetRequirement(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetRequirementResponse{Requirement: requirementToProto(r)}, nil
}

func (h *Handler) CancelRequirement(ctx context.Context, req *prv1.CancelRequirementRequest) (*prv1.CancelRequirementResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	status, err := h.svc.CancelRequirement(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetReason(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CancelRequirementResponse{Status: status}, nil
}

func requirementToProto(r store.GetRequirementRow) *prv1.Requirement {
	return &prv1.Requirement{
		Id: r.ID, ContractId: r.ContractID, ContractNo: r.ContractNo,
		ContractVersionId: r.ContractVersionID, VersionNo: r.VersionNo,
		ContractItemId: r.ContractItemID, CustomerName: r.CustomerName,
		ProductId: r.ProductID, SkuId: r.SkuID,
		ProductCode: r.ProductCode, ProductName: r.ProductName, Spec: r.Spec,
		UomId: r.UomID, UomCode: r.UomCode,
		RequiredQty: r.RequiredQty, OrderedQty: r.OrderedQty, ReceivedQty: r.ReceivedQty,
		RequiredDate: r.RequiredDate, Source: r.Source, Status: r.Status,
		ClosedReason: r.ClosedReason, CreatedAt: ts(r.CreatedAt),
		QuotationId: r.QuotationID, QuotationNo: r.QuotationNo,
		CostScenarioId: r.CostScenarioID, CostScenarioNo: r.CostScenarioNo,
		SourcingCaseId: r.SourcingCaseID, SourcingLineId: r.SourcingLineID,
		SupplierQuoteLineId: r.SupplierQuoteLineID,
		SupplierId:          r.SupplierID, SupplierCode: r.SupplierCode, SupplierName: r.SupplierName,
		FactoryId: r.FactoryID, FactoryCode: r.FactoryCode, FactoryName: r.FactoryName,
		SourceCurrency: r.SourceCurrency, SourceUnitPrice: r.SourceUnitPrice,
		Moq: r.Moq, LeadTime: r.LeadTime,
	}
}

func ts(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func (h *Handler) ReopenRequirement(ctx context.Context, req *prv1.ReopenRequirementRequest) (*prv1.ReopenRequirementResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	status, err := h.svc.ReopenRequirement(ctx, grpcx.TenantID(ctx), req.GetId(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ReopenRequirementResponse{Status: status}, nil
}

func (h *Handler) ListRequirementOrders(ctx context.Context, req *prv1.ListRequirementOrdersRequest) (*prv1.ListRequirementOrdersResponse, error) {
	rows, err := h.svc.RequirementOrders(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.CoveringOrder, 0, len(rows))
	for _, r := range rows {
		out = append(out, &prv1.CoveringOrder{
			PoId: r.ID, PoNo: r.PoNo, SupplierName: r.SupplierName, Status: r.Status,
			Currency: r.Currency, ExpectedDate: r.ExpectedDate, Qty: r.Qty,
			UnitPrice: r.UnitPrice, ReceivedQty: r.ReceivedQty, CreatedAt: ts(r.CreatedAt),
		})
	}
	return &prv1.ListRequirementOrdersResponse{Orders: out}, nil
}
