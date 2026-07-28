// Package grpcout holds the clients approval uses to ask other services
// questions. It only ever reads; approval owns no data but its own.
package grpcout

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
)

// IAM answers "who holds this role" for flow nodes that name a role.
type IAM struct {
	// Two clients: role membership is an access question, the reporting line
	// is an org-chart one, and they live on different services.
	client    iamv1.AccessServiceClient
	directory iamv1.DirectoryServiceClient
}

func NewIAM(conn *grpc.ClientConn) *IAM {
	return &IAM{
		client:    iamv1.NewAccessServiceClient(conn),
		directory: iamv1.NewDirectoryServiceClient(conn),
	}
}

func (c *IAM) RoleMembers(ctx context.Context, roleID int64) ([]int64, error) {
	resp, err := c.client.ListRoleMembers(ctx, &iamv1.ListRoleMembersRequest{RoleId: roleID})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(resp.GetMembers()))
	for _, m := range resp.GetMembers() {
		ids = append(ids, m.GetEmployeeId())
	}
	return ids, nil
}

// ManagersOf resolves who the submitter reports to, for a flow node that says
// "my manager" rather than naming a role.
func (c *IAM) ManagersOf(ctx context.Context, employeeID int64, levels int32) ([]int64, error) {
	resp, err := c.directory.ListManagers(ctx, &iamv1.ListManagersRequest{EmployeeId: employeeID, Levels: levels})
	if err != nil {
		return nil, err
	}
	return resp.GetEmployeeIds(), nil
}
