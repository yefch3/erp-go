// Package grpcin adapts gRPC requests onto the app layer: proto <-> app
// conversion only, no business logic.
package grpcin

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
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
	res, err := // No tenant from the context: the address decides which company
		// this is. A login page serving twenty of them is the same page.
		h.svc.Login(ctx, req.GetEmail(), req.GetPassword())
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

// PeekInvitation and ActivateAccount take no tenant and no operator, and that
// is the point: the caller has neither yet. The token is the whole of their
// claim, and it carries which company and which employee inside it.

func (h *Handler) PeekInvitation(ctx context.Context, req *iamv1.PeekInvitationRequest) (*iamv1.PeekInvitationResponse, error) {
	t, err := h.svc.PeekInvitation(ctx, req.GetToken())
	if err != nil {
		return nil, err
	}
	return &iamv1.PeekInvitationResponse{Name: t.Name, Email: t.Email}, nil
}

func (h *Handler) ActivateAccount(ctx context.Context, req *iamv1.ActivateAccountRequest) (*iamv1.ActivateAccountResponse, error) {
	email, err := h.svc.ActivateAccount(ctx, req.GetToken(), req.GetPassword())
	if err != nil {
		return nil, err
	}
	return &iamv1.ActivateAccountResponse{Email: email}, nil
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
		ManagerID: req.GetManagerId(),
	})
	if err != nil {
		return nil, err
	}
	// Echo the account back so the caller sees what it just created; the row
	// itself carries no username.
	out := employeeRowToProto(emp, nil)
	out.Username = req.GetUsername()
	return &iamv1.CreateEmployeeResponse{Employee: out}, nil
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
	// One lookup for the whole page instead of a query per row.
	accounts, err := h.svc.ListAccounts(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.Employee, len(rows))
	for i, r := range rows {
		out[i] = &iamv1.Employee{
			Id: r.ID, Code: r.Code, Name: r.Name,
			DepartmentId: r.DepartmentID, DepartmentName: r.DepartmentName,
			Position: r.Position, Email: r.Email, Phone: r.Phone, Status: r.Status,
			Username:  accounts[r.ID],
			ManagerId: deref(r.ManagerID), ManagerName: r.ManagerName,
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
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.DeactivateEmployee(ctx, grpcx.TenantID(ctx), req.GetId(), op.EmployeeID); err != nil {
		return nil, err
	}
	return &iamv1.DeactivateEmployeeResponse{}, nil
}

func (h *Handler) ActivateEmployee(ctx context.Context, req *iamv1.ActivateEmployeeRequest) (*iamv1.ActivateEmployeeResponse, error) {
	if err := h.svc.ActivateEmployee(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &iamv1.ActivateEmployeeResponse{Activated: true}, nil
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

func (h *Handler) OpenAccount(ctx context.Context, req *iamv1.OpenAccountRequest) (*iamv1.OpenAccountResponse, error) {
	username, err := h.svc.OpenAccount(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(),
		req.GetUsername(), req.GetInitialPassword())
	if err != nil {
		return nil, err
	}
	return &iamv1.OpenAccountResponse{Username: username}, nil
}

func (h *Handler) ResetPassword(ctx context.Context, req *iamv1.ResetPasswordRequest) (*iamv1.ResetPasswordResponse, error) {
	if err := h.svc.ResetPassword(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetNewPassword()); err != nil {
		return nil, err
	}
	return &iamv1.ResetPasswordResponse{Reset_: true}, nil
}

func (h *Handler) ChangePassword(ctx context.Context, req *iamv1.ChangePasswordRequest) (*iamv1.ChangePasswordResponse, error) {
	// The account changed is always the caller's own, taken from the verified
	// token rather than the request body.
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.EmployeeID == 0 {
		return nil, apierr.Unauthorized("IAM_ACTOR_REQUIRED", "缺少操作人身份")
	}
	if err := h.svc.ChangePassword(ctx, grpcx.TenantID(ctx), op.EmployeeID,
		req.GetOldPassword(), req.GetNewPassword()); err != nil {
		return nil, err
	}
	return &iamv1.ChangePasswordResponse{Changed: true}, nil
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
		RoleIds: roleIDs, ManagerId: deref(e.ManagerID), ManagerName: e.ManagerName,
	}
}

func deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func (h *Handler) ListManagers(ctx context.Context, req *iamv1.ListManagersRequest) (*iamv1.ListManagersResponse, error) {
	ids, err := h.svc.ManagersOf(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetLevels())
	if err != nil {
		return nil, err
	}
	return &iamv1.ListManagersResponse{EmployeeIds: ids}, nil
}

func (h *Handler) InviteEmployee(ctx context.Context, req *iamv1.InviteEmployeeRequest) (*iamv1.InviteEmployeeResponse, error) {
	// Who invited whom is recorded, so the operator is required rather than
	// defaulted: an invitation with nobody behind it is not a thing that can
	// happen — the mail has to leave from somebody's mailbox.
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.EmployeeID == 0 {
		return nil, apierr.Unauthorized("IAM_ACTOR_REQUIRED", "缺少操作人身份")
	}
	inv, err := h.svc.InviteEmployee(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), op.EmployeeID)
	if err != nil {
		return nil, err
	}
	return &iamv1.InviteEmployeeResponse{
		Token: inv.Token, Email: inv.Email, Name: inv.Name,
		ExpiresAt: inv.ExpiresAt.Unix(),
	}, nil
}

func (h *Handler) SetManager(ctx context.Context, req *iamv1.SetManagerRequest) (*iamv1.SetManagerResponse, error) {
	if err := h.svc.SetManager(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetManagerId()); err != nil {
		return nil, err
	}
	return &iamv1.SetManagerResponse{Changed: true}, nil
}

func (h *Handler) VisibleEmployees(ctx context.Context, req *iamv1.VisibleEmployeesRequest) (*iamv1.VisibleEmployeesResponse, error) {
	v, err := h.svc.VisibleEmployees(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetModule())
	if err != nil {
		return nil, err
	}
	return &iamv1.VisibleEmployeesResponse{
		All: v.All, EmployeeIds: v.EmployeeIDs, ScopeType: v.ScopeType,
	}, nil
}

func (h *Handler) ListDataScopes(ctx context.Context, _ *iamv1.ListDataScopesRequest) (*iamv1.ListDataScopesResponse, error) {
	rows, err := h.svc.ListDataScopes(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.DataScope, 0, len(rows))
	for _, r := range rows {
		out = append(out, &iamv1.DataScope{
			RoleId: r.RoleID, Module: r.Module, ScopeType: r.ScopeType,
			DepartmentIds: r.CustomDeptIds,
		})
	}
	return &iamv1.ListDataScopesResponse{Scopes: out}, nil
}

func (h *Handler) SetDataScope(ctx context.Context, req *iamv1.SetDataScopeRequest) (*iamv1.SetDataScopeResponse, error) {
	sc := req.GetScope()
	if err := h.svc.SetDataScope(ctx, grpcx.TenantID(ctx), sc.GetRoleId(),
		sc.GetModule(), sc.GetScopeType(), sc.GetDepartmentIds()); err != nil {
		return nil, err
	}
	return &iamv1.SetDataScopeResponse{Saved: true}, nil
}
