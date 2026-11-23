package grpcin

import (
	"context"

	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *Handler) GetDailyShippingActivity(ctx context.Context, req *shippingv1.GetDailyShippingActivityRequest) (*shippingv1.GetDailyShippingActivityResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.DailyShippingActivity(ctx, grpcx.TenantID(ctx), op.EmployeeID, req.GetDayStart(), req.GetAsOf(), req.GetTrendUnit())
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetDailyShippingActivityResponse{Rows: rows}, nil
}
