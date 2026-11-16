package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

func (h *OrderHandler) CheckDeliveryQuality(ctx context.Context, req *prv1.CheckDeliveryQualityRequest) (*prv1.CheckDeliveryQualityResponse, error) {
	tenant := grpcx.TenantID(ctx)
	if err := h.svc.AuthorizeDeliveryQualityCheck(ctx, tenant); err != nil {
		return nil, err
	}
	in := app.DeliveryQualityInput{POIDs: req.GetPoIds(), ContractID: req.GetContractId(), ContractItemIDs: req.GetContractItemIds()}
	for _, line := range req.GetLines() {
		in.Lines = append(in.Lines, app.DeliveryQualityLine{ContractItemID: line.GetContractItemId(), Qty: line.GetQty()})
	}
	result, err := h.svc.CheckDeliveryQuality(ctx, tenant, in)
	if err != nil {
		return nil, err
	}
	return &prv1.CheckDeliveryQualityResponse{Allowed: result.Allowed, Reason: result.Reason, Code: result.Code}, nil
}
