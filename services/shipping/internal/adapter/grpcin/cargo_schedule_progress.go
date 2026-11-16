package grpcin

import (
	"context"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *Handler) GetCargoScheduleProgress(ctx context.Context, req *shippingv1.GetCargoScheduleProgressRequest) (*shippingv1.GetCargoScheduleProgressResponse, error) {
	row, err := h.svc.GetCargoScheduleProgress(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), req.GetContractId(), req.GetContractItemIds())
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetCargoScheduleProgressResponse{Schedule: &shippingv1.CargoScheduleProgress{Id: row.ID, ContractId: row.ContractID, ScheduleNo: row.ScheduleNo, Status: row.Status, Etd: row.ETD, Eta: row.ETA, Atd: row.ATD, Ata: row.ATA}}, nil
}
