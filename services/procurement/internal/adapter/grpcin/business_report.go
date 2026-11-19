package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func (h *SourcingHandler) GetReportInquiries(ctx context.Context, req *prv1.GetReportInquiriesRequest) (*prv1.GetReportInquiriesResponse, error) {
	return h.svc.ReportInquiries(ctx, req)
}

func (h *SourcingHandler) GetReportContractCosts(ctx context.Context, req *prv1.GetReportContractCostsRequest) (*prv1.GetReportContractCostsResponse, error) {
	return h.svc.ReportContractCosts(ctx, req)
}
