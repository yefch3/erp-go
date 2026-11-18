package grpcin

import (
	"context"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

func (h *OrderHandler) SaveHistoricalOrder(ctx context.Context, r *prv1.SaveHistoricalOrderRequest) (*prv1.SaveHistoricalOrderResponse, error) {
	lines := make([]app.HistoricalOrderLine, 0, len(r.GetLines()))
	for _, l := range r.GetLines() {
		lines = append(lines, app.HistoricalOrderLine{ID: l.GetId(), ProductID: l.GetProductId(), ProductCode: l.GetProductCode(), UomID: l.GetUomId(), ProductName: l.GetProductName(), Spec: l.GetSpec(), UOM: l.GetUom(), Qty: l.GetQty(), UnitPrice: l.GetUnitPrice()})
	}
	v, e := h.svc.SaveHistoricalOrder(ctx, grpcx.TenantID(ctx), app.HistoricalOrderInput{ExportContractNo: r.GetExportContractNo(), DestinationPort: r.GetDestinationPort(), ID: r.GetId(), PayableDueDate: r.GetPayableDueDate(), SupplierID: r.GetSupplierId(), PONo: r.GetPoNo(), OriginalDate: r.GetOriginalDate(), Currency: r.GetCurrency(), ExpectedDate: r.GetExpectedDate(), ContactName: r.GetContactName(), ContactPhone: r.GetContactPhone(), Remark: r.GetRemark(), DeliveryAddress: r.GetDeliveryAddress(), Confirm: r.GetConfirm(), Lines: lines}, currentOp(ctx))
	if e != nil {
		return nil, e
	}
	return &prv1.SaveHistoricalOrderResponse{Id: v.ID, PoNo: v.PONo, Status: v.Status}, nil
}
func (h *OrderHandler) applyHistoricalMeta(ctx context.Context, o *prv1.PurchaseOrder) error {
	m, e := h.svc.HistoricalOrderMeta(ctx, grpcx.TenantID(ctx), o.Id)
	if e != nil {
		return e
	}
	o.Historical = m.Historical
	o.OriginalDate = m.OriginalDate
	o.ContactName = m.ContactName
	o.ContactPhone = m.ContactPhone
	o.PricesComplete = m.PricesComplete
	o.RecordedBy = m.RecordedBy
	o.RecordedAt = m.RecordedAt
	if m.Historical {
		o.ExportContractNo = m.ExportContractNo
		o.DestinationPort = m.DestinationPort
	}
	if m.Historical && !m.PricesComplete {
		o.TotalAmount = ""
	}
	return nil
}

func (h *OrderHandler) UpdateHistoricalShippingContext(ctx context.Context, r *prv1.UpdateHistoricalShippingContextRequest) (*prv1.UpdateHistoricalShippingContextResponse, error) {
	if err := h.svc.UpdateHistoricalShippingContext(ctx, grpcx.TenantID(ctx), r.GetId(), r.GetExportContractNo(), r.GetDestinationPort(), currentOp(ctx)); err != nil {
		return nil, err
	}
	return &prv1.UpdateHistoricalShippingContextResponse{}, nil
}

func (h *OrderHandler) ListOrderBuyerCandidates(ctx context.Context, _ *prv1.ListOrderBuyerCandidatesRequest) (*prv1.ListOrderBuyerCandidatesResponse, error) {
	people, err := h.svc.ListOrderBuyerCandidates(ctx, grpcx.TenantID(ctx), currentOp(ctx))
	if err != nil {
		return nil, err
	}
	out := &prv1.ListOrderBuyerCandidatesResponse{Candidates: make([]*prv1.OrderBuyerCandidate, 0, len(people))}
	for _, p := range people {
		out.Candidates = append(out.Candidates, &prv1.OrderBuyerCandidate{EmployeeId: p.ID, Name: p.Name, DepartmentName: p.DepartmentName})
	}
	return out, nil
}

func (h *OrderHandler) ChangeOrderBuyer(ctx context.Context, r *prv1.ChangeOrderBuyerRequest) (*prv1.ChangeOrderBuyerResponse, error) {
	v, err := h.svc.ChangeOrderBuyer(ctx, grpcx.TenantID(ctx), r.GetId(), r.GetBuyerId(), r.GetReason(), currentOp(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ChangeOrderBuyerResponse{BuyerId: v.BuyerID, BuyerName: v.BuyerName, Changed: v.Changed}, nil
}

func (h *OrderHandler) ListShippingHistoricalPurchases(ctx context.Context, r *prv1.ListShippingHistoricalPurchasesRequest) (*prv1.ListShippingHistoricalPurchasesResponse, error) {
	rows, err := h.svc.ListShippingHistoricalPurchases(ctx, grpcx.TenantID(ctx), r.GetContractNo(), currentOp(ctx))
	if err != nil {
		return nil, err
	}
	out := &prv1.ListShippingHistoricalPurchasesResponse{}
	for _, row := range rows {
		out.Items = append(out.Items, &prv1.ShippingHistoricalPurchase{PoNo: row.PONo, SupplierName: row.SupplierName, ExportContractNo: row.ExportContractNo, DestinationPort: row.DestinationPort, Remark: row.Remark})
	}
	return out, nil
}
