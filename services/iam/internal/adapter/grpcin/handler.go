// Package grpcin adapts gRPC requests onto the app layer: proto <-> app
// conversion only, no business logic.
package grpcin

import (
	"context"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/iam/internal/app"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

type Handler struct {
	iamv1.UnimplementedAuthServiceServer
	iamv1.UnimplementedDirectoryServiceServer
	iamv1.UnimplementedAccessServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

// ---------------------------------------------------------------- auth

func (h *Handler) Login(ctx context.Context, req *iamv1.LoginRequest) (*iamv1.LoginResponse, error) {
	res, err := h.svc.Login(ctx, grpcx.TenantID(ctx), req.GetUsername(), req.GetPassword())
	if err != nil {
		return nil, err
	}
	return &iamv1.LoginResponse{
		AccessToken:      res.Token,
		ExpiresInSeconds: res.ExpiresInSeconds,
		Employee:         employeeRowToProto(res.Employee, nil),
		PermissionCodes:  res.PermissionCodes,
	}, nil
}

// ---------------------------------------------------------------- directory

func (h *Handler) CreateDepartment(ctx context.Context, req *iamv1.CreateDepartmentRequest) (*iamv1.CreateDepartmentResponse, error) {
	d, err := h.svc.CreateDepartment(ctx, grpcx.TenantID(ctx), req.GetCode(), req.GetName(), req.GetParentId())
	if err != nil {
		return nil, err
	}
	return &iamv1.CreateDepartmentResponse{Department: departmentToProto(d)}, nil
}

func (h *Handler) ListDepartments(ctx context.Context, _ *iamv1.ListDepartmentsRequest) (*iamv1.ListDepartmentsResponse, error) {
	ds, err := h.svc.ListDepartments(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.Department, len(ds))
	for i, d := range ds {
		out[i] = departmentToProto(d)
	}
	return &iamv1.ListDepartmentsResponse{Departments: out}, nil
}

func (h *Handler) CreateEmployee(ctx context.Context, req *iamv1.CreateEmployeeRequest) (*iamv1.CreateEmployeeResponse, error) {
	emp, err := h.svc.CreateEmployee(ctx, grpcx.TenantID(ctx), app.CreateEmployeeInput{
		Code: req.GetCode(), Name: req.GetName(), DepartmentID: req.GetDepartmentId(),
		Position: req.GetPosition(), Email: req.GetEmail(), Phone: req.GetPhone(),
		Username: req.GetUsername(), InitialPassword: req.GetInitialPassword(),
	})
	if err != nil {
		return nil, err
	}
	return &iamv1.CreateEmployeeResponse{Employee: employeeRowToProto(emp, nil)}, nil
}

func (h *Handler) GetEmployee(ctx context.Context, req *iamv1.GetEmployeeRequest) (*iamv1.GetEmployeeResponse, error) {
	emp, roleIDs, err := h.svc.GetEmployee(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &iamv1.GetEmployeeResponse{Employee: employeeRowToProto(emp, roleIDs)}, nil
}

func (h *Handler) ListEmployees(ctx context.Context, req *iamv1.ListEmployeesRequest) (*iamv1.ListEmployeesResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.ListEmployees(ctx, grpcx.TenantID(ctx),
		req.GetDepartmentId(), req.GetKeyword(), page, size)
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.Employee, len(rows))
	for i, r := range rows {
		out[i] = &iamv1.Employee{
			Id: r.ID, Code: r.Code, Name: r.Name,
			DepartmentId: r.DepartmentID, DepartmentName: r.DepartmentName,
			Position: r.Position, Email: r.Email, Phone: r.Phone, Status: r.Status,
		}
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	return &iamv1.ListEmployeesResponse{
		Employees: out,
		Meta:      &commonv1.PageMeta{Total: total, Page: page, PageSize: size},
	}, nil
}

func (h *Handler) DeactivateEmployee(ctx context.Context, req *iamv1.DeactivateEmployeeRequest) (*iamv1.DeactivateEmployeeResponse, error) {
	if err := h.svc.DeactivateEmployee(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &iamv1.DeactivateEmployeeResponse{}, nil
}

// ---------------------------------------------------------------- access

func (h *Handler) CreateRole(ctx context.Context, req *iamv1.CreateRoleRequest) (*iamv1.CreateRoleResponse, error) {
	r, err := h.svc.CreateRole(ctx, grpcx.TenantID(ctx), req.GetCode(), req.GetName(), req.GetDescription())
	if err != nil {
		return nil, err
	}
	return &iamv1.CreateRoleResponse{Role: &iamv1.Role{
		Id: r.ID, Code: r.Code, Name: r.Name, Description: r.Description,
	}}, nil
}

func (h *Handler) ListRoles(ctx context.Context, _ *iamv1.ListRolesRequest) (*iamv1.ListRolesResponse, error) {
	roles, codes, err := h.svc.ListRoles(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.Role, len(roles))
	for i, r := range roles {
		out[i] = &iamv1.Role{
			Id: r.ID, Code: r.Code, Name: r.Name, Description: r.Description,
			PermissionCodes: codes[r.ID],
		}
	}
	return &iamv1.ListRolesResponse{Roles: out}, nil
}

func (h *Handler) ListPermissions(ctx context.Context, _ *iamv1.ListPermissionsRequest) (*iamv1.ListPermissionsResponse, error) {
	ps, err := h.svc.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.Permission, len(ps))
	for i, p := range ps {
		out[i] = &iamv1.Permission{Id: p.ID, Code: p.Code, Name: p.Name, Module: p.Module}
	}
	return &iamv1.ListPermissionsResponse{Permissions: out}, nil
}

func (h *Handler) GrantRolePermissions(ctx context.Context, req *iamv1.GrantRolePermissionsRequest) (*iamv1.GrantRolePermissionsResponse, error) {
	if err := h.svc.GrantRolePermissions(ctx, grpcx.TenantID(ctx), req.GetRoleId(), req.GetPermissionCodes()); err != nil {
		return nil, err
	}
	return &iamv1.GrantRolePermissionsResponse{}, nil
}

func (h *Handler) AssignEmployeeRoles(ctx context.Context, req *iamv1.AssignEmployeeRolesRequest) (*iamv1.AssignEmployeeRolesResponse, error) {
	if err := h.svc.AssignEmployeeRoles(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetRoleIds()); err != nil {
		return nil, err
	}
	return &iamv1.AssignEmployeeRolesResponse{}, nil
}

func (h *Handler) CheckPermission(ctx context.Context, req *iamv1.CheckPermissionRequest) (*iamv1.CheckPermissionResponse, error) {
	allowed, err := h.svc.CheckPermission(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetPermissionCode())
	if err != nil {
		return nil, err
	}
	return &iamv1.CheckPermissionResponse{Allowed: allowed}, nil
}

func (h *Handler) ListEmployeePermissions(ctx context.Context, req *iamv1.ListEmployeePermissionsRequest) (*iamv1.ListEmployeePermissionsResponse, error) {
	codes, err := h.svc.ListEmployeePermissions(ctx, grpcx.TenantID(ctx), req.GetEmployeeId())
	if err != nil {
		return nil, err
	}
	return &iamv1.ListEmployeePermissionsResponse{PermissionCodes: codes}, nil
}

func (h *Handler) ListRoleMembers(ctx context.Context, req *iamv1.ListRoleMembersRequest) (*iamv1.ListRoleMembersResponse, error) {
	rows, err := h.svc.ListRoleMembers(ctx, grpcx.TenantID(ctx), req.GetRoleId())
	if err != nil {
		return nil, err
	}
	members := make([]*iamv1.RoleMember, 0, len(rows))
	for _, r := range rows {
		members = append(members, &iamv1.RoleMember{EmployeeId: r.EmployeeID, Name: r.Name})
	}
	return &iamv1.ListRoleMembersResponse{Members: members}, nil
}

// ---------------------------------------------------------------- mapping

func departmentToProto(d store.Department) *iamv1.Department {
	var parent int64
	if d.ParentID != nil {
		parent = *d.ParentID
	}
	return &iamv1.Department{Id: d.ID, Code: d.Code, Name: d.Name, ParentId: parent, Status: d.Status}
}

func employeeRowToProto(e store.GetEmployeeRow, roleIDs []int64) *iamv1.Employee {
	return &iamv1.Employee{
		Id: e.ID, Code: e.Code, Name: e.Name,
		DepartmentId: e.DepartmentID, DepartmentName: e.DepartmentName,
		Position: e.Position, Email: e.Email, Phone: e.Phone, Status: e.Status,
		RoleIds: roleIDs,
	}
}
