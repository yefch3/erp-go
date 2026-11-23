package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *OrderHandler) GetDailyPurchaseActivity(ctx context.Context, req *prv1.GetDailyPurchaseActivityRequest) (*prv1.GetDailyPurchaseActivityResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.DailyPurchaseActivity(ctx, grpcx.TenantID(ctx), op.EmployeeID, req.GetDayStart(), req.GetAsOf(), req.GetTrendUnit())
	if err != nil {
		return nil, err
	}
	return &prv1.GetDailyPurchaseActivityResponse{Rows: rows}, nil
}
