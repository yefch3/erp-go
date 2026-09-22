package grpcin

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *ContractHandler) CheckContractExecution(ctx context.Context, r *exv1.CheckContractExecutionRequest) (*exv1.CheckContractExecutionResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:contract:read", "procurement:order:write", "procurement:requirement:write", "shipping:schedule:write", "export:shipment:write"); err != nil {
		return nil, err
	}
	allowed, reason, err := h.svc.CheckContractExecution(ctx, grpcx.TenantID(ctx), r.GetId())
	if err != nil {
		return nil, err
	}
	return &exv1.CheckContractExecutionResponse{Allowed: allowed, Reason: reason}, nil
}
