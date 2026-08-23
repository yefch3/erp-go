package grpcout

import (
	"context"

	"google.golang.org/grpc"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
)

type Scopes struct{ client iamv1.AccessServiceClient }

func NewScopes(conn *grpc.ClientConn) *Scopes {
	return &Scopes{client: iamv1.NewAccessServiceClient(conn)}
}

func (s *Scopes) VisibleEmployees(ctx context.Context, employeeID int64, module string) (app.Visibility, error) {
	resp, err := s.client.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{
		EmployeeId: employeeID, Module: module,
	})
	if err != nil {
		return app.Visibility{}, err
	}
	return app.Visibility{
		All: resp.GetAll(), EmployeeIDs: resp.GetEmployeeIds(), ScopeType: resp.GetScopeType(),
	}, nil
}

type CustomerAccess struct{ client mdv1.CustomerServiceClient }

func NewCustomerAccess(conn *grpc.ClientConn) *CustomerAccess {
	return &CustomerAccess{client: mdv1.NewCustomerServiceClient(conn)}
}

func (c *CustomerAccess) CustomerIDsOwnedBy(ctx context.Context, employeeIDs []int64) ([]int64, error) {
	resp, err := c.client.ListCustomerIdsByOwnerEmployees(ctx, &mdv1.ListCustomerIdsByOwnerEmployeesRequest{
		EmployeeIds: employeeIDs,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetCustomerIds(), nil
}

// Directory 按角色码找人：先把角色码换成 id，再列这个角色下的在职员工。
//
// 两次调用而不是一个 RPC，是因为 IAM 没有「按角色码列人」这一个接口，
// 而为一次每天跑一遍的扫描新增 RPC 不值得。真变成热路径了再说。
type Directory struct {
	access    iamv1.AccessServiceClient    // 角色在这边
	directory iamv1.DirectoryServiceClient // 员工在这边
}

func NewDirectory(conn *grpc.ClientConn) *Directory {
	return &Directory{
		access:    iamv1.NewAccessServiceClient(conn),
		directory: iamv1.NewDirectoryServiceClient(conn),
	}
}

func (d *Directory) EmployeeIDsByRole(ctx context.Context, roleCode string) ([]int64, error) {
	roles, err := d.access.ListRoles(ctx, &iamv1.ListRolesRequest{})
	if err != nil {
		return nil, err
	}
	var roleID int64
	for _, r := range roles.GetRoles() {
		if r.GetCode() == roleCode {
			roleID = r.GetId()
			break
		}
	}
	if roleID == 0 {
		// 这家公司没配这个角色，不是错误——提醒退回只发负责人。
		return nil, nil
	}
	resp, err := d.directory.ListEmployees(ctx, &iamv1.ListEmployeesRequest{
		RoleId: roleID, EmploymentStatus: "ACTIVE",
		Page: &commonv1.PageRequest{Page: 1, PageSize: 200},
	})
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(resp.GetEmployees()))
	for _, e := range resp.GetEmployees() {
		out = append(out, e.GetId())
	}
	return out, nil
}
