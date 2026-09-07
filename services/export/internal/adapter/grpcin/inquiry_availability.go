package grpcin

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/inquiryproof"
	"google.golang.org/grpc/metadata"
	"os"
)

func (h *Handler) SetInquiryAvailability(ctx context.Context, r *exv1.SetInquiryAvailabilityRequest) (*exv1.SetInquiryAvailabilityResponse, error) {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.EmployeeID <= 0 {
		return nil, apierr.Permission("INQUIRY_OPERATOR_REQUIRED", "需要登录")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	proof := md.Get("x-inquiry-proof")
	if len(proof) != 1 || !inquiryproof.Verify(os.Getenv("INTERNAL_SIGNING_KEY"), proof[0], op.TenantID, op.EmployeeID, r.GetCaseId(), r.GetAvailable()) {
		return nil, apierr.Permission("INQUIRY_SERVICE_REQUIRED", "需要询盘服务授权")
	}
	if err := h.svc.SetInquiryAvailability(ctx, op.TenantID, r.GetCaseId(), r.GetAvailable()); err != nil {
		return nil, err
	}
	return &exv1.SetInquiryAvailabilityResponse{}, nil
}
