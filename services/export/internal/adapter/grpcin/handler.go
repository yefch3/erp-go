// Package grpcin exposes quotations over gRPC.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
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
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read"); err != nil {
		return nil, err
	}
	rows, total, err := h.svc.ListQuotations(ctx, grpcx.TenantID(ctx), app.QuotationFilter{
		Keyword: req.GetKeyword(), CustomerID: req.GetCustomerId(), Status: req.GetStatus(),
		WithoutContract: req.GetWithoutContract(), SourceSourcingCaseID: req.GetSourceSourcingCaseId(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize(), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.Quotation, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.Quotation{
			Id: r.ID, QuoteNo: r.QuoteNo, CustomerId: r.CustomerID, CustomerName: r.CustomerName,
			Currency: r.Currency, TotalAmount: r.TotalAmount, BaseAmount: r.BaseAmount,
			Status: r.Status, SalesEmployeeId: r.SalesEmployeeID, SalesEmployee: r.SalesEmployee,
			ValidUntil:           r.ValidUntil,
			CreatedAt:            ts(r.CreatedAt),
			SourceCostScenarioId: r.SourceCostScenarioID, SourceCostScenarioNo: r.SourceCostScenarioNo,
			SourceCustomerSelectionId: r.SourceCustomerSelectionID, SourceCustomerSelectionNo: r.SourceCustomerSelectionNo,
			SourceCustomerSelectionVersion: r.SourceCustomerSelectionVersion,
			SourceSourcingCaseId:           r.SourceSourcingCaseID,
			RespondNote:                    r.RespondNote,
		})
	}
	return &exv1.ListQuotationsResponse{Quotations: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *Handler) GetQuotation(ctx context.Context, req *exv1.GetQuotationRequest) (*exv1.GetQuotationResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read"); err != nil {
		return nil, err
	}
	q, items, err := h.svc.GetQuotationFor(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	shipments, err := h.svc.ListQuotationShipments(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &exv1.GetQuotationResponse{Quotation: quotationToProto(q), Items: itemsToProto(items), Shipments: shipmentsToProto(shipments)}, nil
}

func (h *Handler) CreateQuotation(ctx context.Context, req *exv1.CreateQuotationRequest) (*exv1.CreateQuotationResponse, error) {
	return nil, apierr.Conflict("OFFER_ENTRY_REPLACED", "请在询盘对应的客户报价页面保存、导出或确认成交")
}

func shipmentsFromProto(rows []*exv1.QuotationShipmentInput) []app.QuotationShipmentInput {
	out := make([]app.QuotationShipmentInput, 0, len(rows))
	for _, row := range rows {
		out = append(out, app.QuotationShipmentInput{
			SourceCustomerSelectionShipmentID: row.GetSourceCustomerSelectionShipmentId(), ShipmentGroupKey: row.GetShipmentGroupKey(),
			CarrierForwarder: row.GetCarrierForwarder(), ServiceOptionName: row.GetServiceOptionName(), CustomerManaged: row.GetCustomerManaged(),
			Currency: row.GetCurrency(), FreightAmount: row.GetFreightAmount(), ChargeBasis: row.GetChargeBasis(),
			PortOfLoading: row.GetPortOfLoading(), PortOfDischarge: row.GetPortOfDischarge(),
			EstimatedDeparture: row.GetEstimatedDeparture(), EstimatedArrival: row.GetEstimatedArrival(), ValidUntil: row.GetValidUntil(), Remark: row.GetRemark(),
		})
	}
	return out
}

func shipmentsToProto(rows []store.ListQuotationShipmentsRow) []*exv1.QuotationShipment {
	out := make([]*exv1.QuotationShipment, 0, len(rows))
	for _, row := range rows {
		out = append(out, &exv1.QuotationShipment{
			Id: row.ID, BatchNo: row.BatchNo, SourceCustomerSelectionShipmentId: row.SourceCustomerSelectionShipmentID,
			ShipmentGroupKey: row.ShipmentGroupKey, CarrierForwarder: row.CarrierForwarder, ServiceOptionName: row.ServiceOptionName,
			CustomerManaged: row.CustomerManaged, Currency: row.Currency, FreightAmount: row.FreightAmount, ChargeBasis: row.ChargeBasis,
			PortOfLoading: row.PortOfLoading, PortOfDischarge: row.PortOfDischarge, EstimatedDeparture: row.EstimatedDeparture,
			EstimatedArrival: row.EstimatedArrival, ValidUntil: row.ValidUntil, Remark: row.Remark,
		})
	}
	return out
}

func (h *Handler) UpdateQuotation(ctx context.Context, req *exv1.UpdateQuotationRequest) (*exv1.UpdateQuotationResponse, error) {
	return nil, apierr.Conflict("OFFER_ENTRY_REPLACED", "请在询盘对应的客户报价页面保存、导出或确认成交")
}

func (h *Handler) SendQuotation(ctx context.Context, req *exv1.SendQuotationRequest) (*exv1.SendQuotationResponse, error) {
	return nil, apierr.Conflict("OFFER_ENTRY_REPLACED", "请在询盘对应的客户报价页面保存、导出或确认成交")
}

func (h *Handler) RespondQuotation(ctx context.Context, req *exv1.RespondQuotationRequest) (*exv1.RespondQuotationResponse, error) {
	return nil, apierr.Conflict("OFFER_ENTRY_REPLACED", "请在询盘对应的客户报价页面保存、导出或确认成交")
}

func (h *Handler) CancelQuotation(ctx context.Context, req *exv1.CancelQuotationRequest) (*exv1.CancelQuotationResponse, error) {
	return nil, apierr.Conflict("OFFER_ENTRY_REPLACED", "请在询盘对应的客户报价页面保存、导出或确认成交")
}

func (h *Handler) GetQuotationWorkbook(ctx context.Context, req *exv1.GetQuotationWorkbookRequest) (*exv1.GetQuotationWorkbookResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read"); err != nil {
		return nil, err
	}
	name, data, err := h.svc.GetQuotationWorkbook(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.GetQuotationWorkbookResponse{FileName: name, FileData: data}, nil
}

func (h *Handler) GetQuotationPdf(ctx context.Context, req *exv1.GetQuotationPdfRequest) (*exv1.GetQuotationPdfResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read"); err != nil {
		return nil, err
	}
	name, data, err := h.svc.GetQuotationPDF(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.GetQuotationPdfResponse{FileName: name, FileData: data}, nil
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
			SourceCostScenarioLineID: i.GetSourceCostScenarioLineId(),
			ProductCode:              i.GetProductCode(), ProductName: i.GetProductName(), UomCode: i.GetUomCode(),
			OpeningProcuredQty: i.GetOpeningProcuredQty(), OpeningArrivedQty: i.GetOpeningArrivedQty(),
			OpeningShippedQty: i.GetOpeningShippedQty(),
			PurchaseUnitPrice: i.GetPurchaseUnitPrice(),
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
		SourceCostScenarioId: q.SourceCostScenarioID, SourceCostScenarioNo: q.SourceCostScenarioNo,
		SourceCustomerSelectionId: q.SourceCustomerSelectionID, SourceCustomerSelectionNo: q.SourceCustomerSelectionNo,
		SourceCustomerSelectionVersion: q.SourceCustomerSelectionVersion,
		SourceSourcingCaseId:           q.SourceSourcingCaseID,
		RespondNote:                    q.RespondNote,
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
			SourceCostScenarioLineId: i.SourceCostScenarioLineID,
		})
	}
	return out
}
