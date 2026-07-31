// Package grpcout holds procurement's outbound dependencies. Everything here
// is a question asked at write time — never a read of somebody else's tables.
package grpcout

import (
	"context"

	"google.golang.org/grpc"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

type Numbering struct{ client mdv1.NumberingServiceClient }

func NewNumbering(conn *grpc.ClientConn) *Numbering {
	return &Numbering{client: mdv1.NewNumberingServiceClient(conn)}
}

func (n *Numbering) Next(ctx context.Context, bizType string) (string, error) {
	resp, err := n.client.NextNumber(ctx, &mdv1.NextNumberRequest{BizType: bizType})
	if err != nil {
		return "", err
	}
	return resp.GetNumber(), nil
}

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
	// A flow already running for this document means an earlier submit got as
	// far as the approval service but not as far as our own commit. Reusing it
	// is what makes the retry converge instead of dead-ending on a document
	// that can never be submitted again.
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
