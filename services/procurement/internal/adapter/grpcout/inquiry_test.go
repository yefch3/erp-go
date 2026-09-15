package grpcout

import (
	"context"
	"testing"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type unavailableInquiryClient struct {
	exv1.QuotationServiceClient
	err error
}

func (c unavailableInquiryClient) SetInquiryAvailability(context.Context, *exv1.SetInquiryAvailabilityRequest, ...grpc.CallOption) (*exv1.SetInquiryAvailabilityResponse, error) {
	return &exv1.SetInquiryAvailabilityResponse{}, c.err
}

func TestInquiryAvailabilityConnectionErrors(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded, codes.PermissionDenied, codes.OK} {
		t.Run(code.String(), func(t *testing.T) {
			original := status.Error(code, "test dependency error")
			d := &InquiryDocuments{client: unavailableInquiryClient{err: original}}
			for _, available := range []bool{false, true} {
				err := d.SetAvailability(context.Background(), 1, available)
				if code == codes.Unavailable || code == codes.DeadlineExceeded {
					if apierr.CodeFromError(err) != "INQUIRY_EXPORT_UNAVAILABLE" {
						t.Fatalf("unexpected error: %v", err)
					}
				} else if err != original {
					t.Fatalf("changed business error: %v", err)
				}
			}
		})
	}
}
