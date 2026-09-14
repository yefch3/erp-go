package grpcin

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
)

func (h *Handler) CustomerOffer(ctx context.Context, req *exv1.CustomerOfferRequest) (*exv1.CustomerOfferResponse, error) {
	result, err := h.svc.CustomerOffer(ctx, req.GetCommandJson())
	if err != nil {
		return nil, err
	}
	return &exv1.CustomerOfferResponse{ResultJson: result}, nil
}
