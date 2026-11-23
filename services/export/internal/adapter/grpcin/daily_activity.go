package grpcin

import (
	"context"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *Handler) GetDailyQuotationActivity(ctx context.Context, req *exv1.GetDailyQuotationActivityRequest) (*exv1.GetDailyQuotationActivityResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.DailyQuotationActivity(ctx, grpcx.TenantID(ctx), op.EmployeeID, req.GetDayStart(), req.GetAsOf(), req.GetTrendUnit())
	if err != nil {
		return nil, err
	}
	return &exv1.GetDailyQuotationActivityResponse{Rows: rows}, nil
}
