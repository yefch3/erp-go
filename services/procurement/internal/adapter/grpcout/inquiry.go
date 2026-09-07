package grpcout

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/inquiryproof"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"os"
)

func (s *Scopes) HasPermission(ctx context.Context, id int64, code string) (bool, error) {
	r, e := s.client.CheckPermission(ctx, &iamv1.CheckPermissionRequest{EmployeeId: id, PermissionCode: code})
	if e != nil {
		return false, e
	}
	return r.GetAllowed(), nil
}

type InquiryDocuments struct{ client exv1.QuotationServiceClient }

func NewInquiryDocuments(c *grpc.ClientConn) *InquiryDocuments {
	return &InquiryDocuments{client: exv1.NewQuotationServiceClient(c)}
}
func (d *InquiryDocuments) SetAvailability(ctx context.Context, id int64, available bool) error {
	op, _ := grpcx.OperatorFromContext(ctx)
	ctx = metadata.AppendToOutgoingContext(ctx, "x-inquiry-proof", inquiryproof.Sign(os.Getenv("INTERNAL_SIGNING_KEY"), op.TenantID, op.EmployeeID, id, available))
	_, e := d.client.SetInquiryAvailability(ctx, &exv1.SetInquiryAvailabilityRequest{CaseId: id, Available: available})
	return e
}
