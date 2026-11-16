package grpcin

import (
	"context"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

func (h *OrderHandler) AddOrderFollowUp(ctx context.Context, req *prv1.AddOrderFollowUpRequest) (*prv1.AddOrderFollowUpResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.AddOrderFollowUp(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetNote(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.AddOrderFollowUpResponse{FollowUp: followUpToProto(row)}, nil
}

func followUpToProto(row app.OrderFollowUp) *prv1.OrderFollowUp {
	return &prv1.OrderFollowUp{Id: row.ID, Note: row.Note, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt}
}
