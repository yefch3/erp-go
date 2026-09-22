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
	v, e := h.svc.SaveHistoricalOrder(ctx, grpcx.TenantID(ctx), app.HistoricalOrderInput{ID: r.GetId(), PayableDueDate: r.GetPayableDueDate(), SupplierID: r.GetSupplierId(), PONo: r.GetPoNo(), OriginalDate: r.GetOriginalDate(), Currency: r.GetCurrency(), ExpectedDate: r.GetExpectedDate(), ContactName: r.GetContactName(), ContactPhone: r.GetContactPhone(), Remark: r.GetRemark(), DeliveryAddress: r.GetDeliveryAddress(), Confirm: r.GetConfirm(), Lines: lines}, currentOp(ctx))
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
	if m.Historical && !m.PricesComplete {
		o.TotalAmount = ""
	}
	return nil
}
