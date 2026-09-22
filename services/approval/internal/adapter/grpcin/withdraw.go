package grpcin

import (
	"context"
	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *Handler) Withdraw(ctx context.Context, r *apv1.WithdrawRequest) (*apv1.WithdrawResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	if err = h.svc.Withdraw(ctx, grpcx.TenantID(ctx), r.GetId(), actor, r.GetReason()); err != nil {
		return nil, err
	}
	return &apv1.WithdrawResponse{Status: "CANCELLED"}, nil
}
