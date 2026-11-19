package grpcin

import (
	"context"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
)

func (h *ContractHandler) GetBusinessReport(ctx context.Context, req *exv1.GetBusinessReportRequest) (*exv1.GetBusinessReportResponse, error) {
	return h.svc.BusinessReport(ctx, req)
}
