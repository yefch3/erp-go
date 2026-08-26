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

// RoleMembersByCode answers "who holds this role in the caller's company".
//
// By code rather than by id, because a role id belongs to exactly one company:
// the id that names 采购经理 at the first company names nothing at the second.
// The roles listing is already scoped to the caller's company, so the same code
// resolves to each company's own role.
func (c *IAM) RoleMembersByCode(ctx context.Context, roleCode string) ([]int64, bool, error) {
	// ListRoles 只返回启用的角色，所以「停用了」和「从来没有」在这里是同一个
	// 答案——对调用方来说也确实是同一件事：现在没有一个能用的这个角色。
	roles, err := c.client.ListRoles(ctx, &iamv1.ListRolesRequest{})
	if err != nil {
		return nil, false, err
	}
	for _, r := range roles.GetRoles() {
		if r.GetCode() == roleCode {
			ids, err := c.RoleMembers(ctx, r.GetId())
			return ids, true, err
		}
	}
	// 这家公司没有（或停用了）这个角色，不是错误：调用方要据此说一句人能
	// 照着做的话。
	return nil, false, nil
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
