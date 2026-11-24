package grpcout

import (
	"context"
	"slices"

	"google.golang.org/grpc"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

// Scopes asks iam whose documents the caller may see. Export holds no copy of
// the organisation chart and no opinion about who owns what.
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

// Directory reads one employee out of iam. Export snapshots the name onto the
// documents it owns; it holds no copy of the organisation chart.
type Directory struct {
	client iamv1.DirectoryServiceClient
	access iamv1.AccessServiceClient
}

func NewDirectory(conn *grpc.ClientConn) *Directory {
	return &Directory{client: iamv1.NewDirectoryServiceClient(conn), access: iamv1.NewAccessServiceClient(conn)}
}

func (d *Directory) IsSuperAdmin(ctx context.Context, employeeID int64) (bool, error) {
	employee, err := d.client.GetEmployee(ctx, &iamv1.GetEmployeeRequest{Id: employeeID})
	if err != nil {
		return false, err
	}
	actor := employee.GetEmployee()
	if actor.GetId() != employeeID || actor.GetStatus() != "ACTIVE" {
		return false, nil
	}
	roles, err := d.access.ListRoles(ctx, &iamv1.ListRolesRequest{})
	if err != nil {
		return false, err
	}
	for _, role := range roles.GetRoles() {
		if role.GetCode() == "SUPER_ADMIN" && role.GetStatus() == "ACTIVE" && slices.Contains(actor.GetRoleIds(), role.GetId()) {
			return true, nil
		}
	}
	return false, nil
}
func (d *Directory) Get(ctx context.Context, employeeID int64) (app.Employee, error) {
	resp, err := d.client.GetEmployee(ctx, &iamv1.GetEmployeeRequest{Id: employeeID})
	if err != nil {
		return app.Employee{}, err
	}
	e := resp.GetEmployee()
	return app.Employee{ID: e.GetId(), Name: e.GetName(), Status: e.GetStatus()}, nil
}
