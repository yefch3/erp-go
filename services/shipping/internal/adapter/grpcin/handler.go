// Package grpcin adapts shipping gRPC requests onto the application layer.
package grpcin

import (
	"context"

	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
)

type Handler struct {
	shippingv1.UnimplementedShippingServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) GetModuleStatus(ctx context.Context, _ *shippingv1.GetModuleStatusRequest) (*shippingv1.GetModuleStatusResponse, error) {
	if err := h.svc.ModuleStatus(ctx); err != nil {
		return nil, err
	}
	return &shippingv1.GetModuleStatusResponse{
		Module:        "shipping",
		Status:        "READY",
		SchemaVersion: app.SchemaVersion,
	}, nil
}
