package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *OrderHandler) ListShippingPurchaseRemarks(ctx context.Context, req *prv1.ListShippingPurchaseRemarksRequest) (*prv1.ListShippingPurchaseRemarksResponse, error) {
	rows, err := h.svc.ListShippingPurchaseRemarks(ctx, grpcx.TenantID(ctx), req.GetContractId(), req.GetContractNo(), currentOp(ctx))
	if err != nil {
		return nil, err
	}
	out := &prv1.ListShippingPurchaseRemarksResponse{}
	for _, row := range rows {
		out.Items = append(out.Items, &prv1.ShippingPurchaseRemark{SupplierName: row.SupplierName, PurchaseOrderRemark: row.PurchaseOrderRemark})
	}
	return out, nil
}
