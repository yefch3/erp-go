package grpcout

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"google.golang.org/grpc"
)

type PermissionChecker struct{ client iamv1.AccessServiceClient }

func NewPermissionChecker(conn *grpc.ClientConn) *PermissionChecker {
	return &PermissionChecker{client: iamv1.NewAccessServiceClient(conn)}
}

func (p *PermissionChecker) Allowed(ctx context.Context, employeeID int64, code string) (bool, error) {
	resp, err := p.client.CheckPermission(ctx, &iamv1.CheckPermissionRequest{EmployeeId: employeeID, PermissionCode: code})
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}
