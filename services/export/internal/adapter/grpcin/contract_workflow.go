package grpcin

import (
	"context"
	"encoding/json"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

func (h *ContractHandler) ContractWorkflow(ctx context.Context, r *exv1.ContractWorkflowRequest) (*exv1.ContractWorkflowResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:contract:read"); err != nil {
		return nil, err
	}
	value, err := h.svc.ContractWorkflow(ctx, grpcx.TenantID(ctx), app.WorkflowCommand{ID: r.GetId(), Action: r.GetAction(), Reason: r.GetReason(), Data: json.RawMessage(r.GetDataJson()), Revision: r.GetRevision()}, operator(ctx))
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	return &exv1.ContractWorkflowResponse{DataJson: string(raw)}, err
}
