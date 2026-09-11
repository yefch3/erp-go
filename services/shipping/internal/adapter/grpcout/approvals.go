package grpcout

import (
	"context"
	approvalv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
	"google.golang.org/grpc"
)

type Approvals struct {
	client approvalv1.ApprovalServiceClient
}

func NewApprovals(conn *grpc.ClientConn) *Approvals {
	return &Approvals{client: approvalv1.NewApprovalServiceClient(conn)}
}
func (a *Approvals) Submit(ctx context.Context, in app.ApprovalSubmission) (int64, error) {
	r, err := a.client.Submit(ctx, &approvalv1.SubmitRequest{BizType: in.BizType, BizId: in.BizID, BizNo: in.BizNo, BizSummary: in.Summary, SubmitterId: in.SubmitterID, SubmitterName: in.SubmitterName, Amount: in.Amount})
	if err != nil {
		return 0, err
	}
	return r.GetInstance().GetId(), nil
}
