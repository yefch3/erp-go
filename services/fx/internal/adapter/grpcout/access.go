package grpcout

import (
	"context"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"google.golang.org/grpc"
)

type Access struct{ client iamv1.AccessServiceClient }

func NewAccess(conn *grpc.ClientConn) *Access {
	return &Access{client: iamv1.NewAccessServiceClient(conn)}
}
func (a *Access) HasPermission(ctx context.Context, employee int64, permission string) (bool, error) {
	r, err := a.client.CheckPermission(ctx, &iamv1.CheckPermissionRequest{EmployeeId: employee, PermissionCode: permission})
	if err != nil {
		return false, err
	}
	return r.GetAllowed(), nil
}
