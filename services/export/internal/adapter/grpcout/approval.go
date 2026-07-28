package grpcout

import (
	"context"

	"google.golang.org/grpc"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

// Approvals submits documents to the approval engine. The call is
// synchronous on purpose: the submitter must be told immediately whether a
// flow exists and who it went to, unlike the decision, which comes back
// asynchronously over Kafka.
type Approvals struct{ client apv1.ApprovalServiceClient }

func NewApprovals(conn *grpc.ClientConn) *Approvals {
	return &Approvals{client: apv1.NewApprovalServiceClient(conn)}
}

func (a *Approvals) Submit(ctx context.Context, in app.ApprovalSubmission) (int64, error) {
	resp, err := a.client.Submit(ctx, &apv1.SubmitRequest{
		BizType: in.BizType, BizId: in.BizID, BizNo: in.BizNo, BizSummary: in.Summary,
		SubmitterId: in.SubmitterID, SubmitterName: in.SubmitterName,
		Amount: in.Amount,
	})
	if err == nil {
		return resp.GetInstance().GetId(), nil
	}
	// A flow already running for this document means an earlier submit got
	// as far as the approval service but not as far as our own commit.
	// Reusing it is what makes the retry converge instead of dead-ending.
	if apierr.CodeFromStatus(err) != "AP_ALREADY_RUNNING" {
		return 0, err
	}
	running, lookupErr := a.client.ListInstances(ctx, &apv1.ListInstancesRequest{
		BizType: in.BizType, BizId: in.BizID,
	})
	if lookupErr != nil {
		return 0, err
	}
	for _, inst := range running.GetInstances() {
		if inst.GetStatus() == "RUNNING" {
			return inst.GetId(), nil
		}
	}
	return 0, err
}

// MyDocuments asks which contracts this person has been given an approval
// task on. Export unions it with the data scope so an approver can always
// open what they are asked to decide.
func (a *Approvals) MyDocuments(ctx context.Context, employeeID int64, bizType string) ([]int64, error) {
	resp, err := a.client.MyInvolvedDocuments(ctx, &apv1.MyInvolvedDocumentsRequest{
		BizType: bizType, EmployeeId: employeeID,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetBizIds(), nil
}
