package grpcin

import (
	"context"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
)

func (h *Handler) GetContractDispatchProgress(ctx context.Context, r *shippingv1.GetContractDispatchProgressRequest) (*shippingv1.GetContractDispatchProgressResponse, error) {
	lines, err := h.svc.ContractDispatchProgress(ctx, grpcx.TenantID(ctx), r.ContractId)
	if err != nil {
		return nil, err
	}
	out := &shippingv1.GetContractDispatchProgressResponse{Lines: []*shippingv1.ContractDispatchLine{}}
	for _, line := range lines {
		out.Lines = append(out.Lines, &shippingv1.ContractDispatchLine{ContractItemId: line.ItemID, ProductCode: line.ProductCode, ProductName: line.ProductName, Specification: line.Specification, UomCode: line.UOM, Quantity: line.Quantity})
	}
	return out, nil
}
func (h *Handler) CorrectPortMetadata(ctx context.Context, r *shippingv1.CorrectPortMetadataRequest) (*shippingv1.CorrectPortMetadataResponse, error) {
	out, err := h.svc.CorrectPortMetadata(ctx, grpcx.TenantID(ctx), r.Id, app.PortMetadataCorrection{Terminal: r.Terminal, Name: r.PortName, Code: r.PortCode, Timezone: r.Timezone, Reason: r.Reason, PortID: r.PortId, RouteVersion: r.RouteVersion}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.CorrectPortMetadataResponse{Schedule: scheduleToProto(out)}, nil
}
