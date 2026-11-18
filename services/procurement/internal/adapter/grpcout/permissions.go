package grpcout

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
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

// Holders lists the active employees who hold a permission — the same
// question as Allowed, asked about everybody at once, and answered by IAM
// with the same rule. It backs "pick a colleague who can do this" lists.
func (p *PermissionChecker) Holders(ctx context.Context, code string) ([]app.EmployeeIdentity, error) {
	resp, err := p.client.ListPermissionHolders(ctx, &iamv1.ListPermissionHoldersRequest{PermissionCode: code})
	if err != nil {
		return nil, err
	}
	out := make([]app.EmployeeIdentity, 0, len(resp.GetHolders()))
	for _, h := range resp.GetHolders() {
		out = append(out, app.EmployeeIdentity{ID: h.GetEmployeeId(), Name: h.GetName(), DepartmentName: h.GetDepartmentName()})
	}
	return out, nil
}
