// Package grpcin adapts gRPC requests onto the app layer: proto <-> app
// conversion only, no business logic.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

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
	iamv1.UnimplementedPlatformServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

// ---------------------------------------------------------------- auth

func (h *Handler) Login(ctx context.Context, req *iamv1.LoginRequest) (*iamv1.LoginResponse, error) {
	// No tenant from the context: what was typed decides which company this
	// is. A login page serving twenty of them is the same page.
	// account 是新字段（登录名，任意字符串）；老前端只发 email，两个都收。
	account := req.GetAccount()
	if account == "" {
		account = req.GetEmail()
	}
	res, err := h.svc.Login(ctx, account, req.GetPassword())
	if err != nil {
		return nil, err
	}
	return &iamv1.LoginResponse{
		AccessToken:        res.Token,
		ExpiresInSeconds:   res.ExpiresInSeconds,
		Employee:           employeeRowToProto(res.Employee, nil),
		PermissionCodes:    res.PermissionCodes,
		MustChangePassword: res.MustChangePassword,
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
	op, _ := grpcx.OperatorFromContext(ctx)
	d, err := h.svc.CreateDepartment(ctx, grpcx.TenantID(ctx), app.CreateDepartmentInput{
		Code: req.GetCode(), Name: req.GetName(), ParentID: req.GetParentId(),
		SortOrder: req.GetSortOrder(), OperatorID: op.EmployeeID,
	})
	if err != nil {
		return nil, err
	}
	return &iamv1.CreateDepartmentResponse{Department: departmentToProto(d)}, nil
}

func (h *Handler) UpdateDepartment(ctx context.Context, req *iamv1.UpdateDepartmentRequest) (*iamv1.UpdateDepartmentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	d, err := h.svc.UpdateDepartment(ctx, grpcx.TenantID(ctx), app.UpdateDepartmentInput{
		ID: req.GetId(), Code: req.GetCode(), Name: req.GetName(), ParentID: req.GetParentId(),
		SortOrder: req.GetSortOrder(), LeaderEmployeeID: req.GetLeaderEmployeeId(),
		Status: req.GetStatus(), ExpectedVersion: req.GetExpectedVersion(), OperatorID: op.EmployeeID,
	})
	if err != nil {
		return nil, err
	}
	return &iamv1.UpdateDepartmentResponse{Department: departmentToProto(d)}, nil
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
	op, _ := grpcx.OperatorFromContext(ctx)
	emp, err := h.svc.CreateEmployee(ctx, grpcx.TenantID(ctx), app.CreateEmployeeInput{
		Code: req.GetCode(), Name: req.GetName(), DepartmentID: req.GetDepartmentId(),
		Position: req.GetPosition(), Email: req.GetEmail(), Phone: req.GetPhone(),
		Username: req.GetUsername(), InitialPassword: req.GetInitialPassword(),
		ManagerID: req.GetManagerId(), EnglishName: req.GetEnglishName(),
		HireDate: req.GetHireDate(), Remark: req.GetRemark(), OperatorID: op.EmployeeID,
	})
	if err != nil {
		return nil, err
	}
	// 员工表本身不保存用户名；创建成功后把本次一并建立的账号回填给调用方。
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
	rows, total, err := h.svc.ListEmployees(ctx, grpcx.TenantID(ctx), app.ListEmployeesFilter{
		DepartmentID: req.GetDepartmentId(), ManagerID: req.GetManagerId(), RoleID: req.GetRoleId(),
		Keyword: req.GetKeyword(), AccountStatus: req.GetAccountStatus(),
		EmploymentStatus: req.GetEmploymentStatus(), Page: page, Size: size,
	})
	if err != nil {
		return nil, err
	}
	// 账号和待激活邀请均按整页一次读取，避免员工列表产生逐行查询。
	accounts, err := h.svc.ListAccounts(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	pending, err := h.svc.PendingInvitations(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.Employee, len(rows))
	for i, r := range rows {
		var inviteExpires int64
		if due, waiting := pending[r.ID]; waiting {
			inviteExpires = due.Unix()
		}
		out[i] = employeeListRowToProto(r, accounts[r.ID], inviteExpires)
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

func (h *Handler) UpdateEmployee(ctx context.Context, req *iamv1.UpdateEmployeeRequest) (*iamv1.UpdateEmployeeResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	emp, err := h.svc.UpdateEmployee(ctx, grpcx.TenantID(ctx), app.UpdateEmployeeInput{
		ID: req.GetId(), Code: req.GetCode(), Name: req.GetName(), EnglishName: req.GetEnglishName(),
		DepartmentID: req.GetDepartmentId(), Position: req.GetPosition(), Email: req.GetEmail(),
		Phone: req.GetPhone(), ManagerID: req.GetManagerId(), HireDate: req.GetHireDate(),
		LeaveDate: req.GetLeaveDate(), Remark: req.GetRemark(), ExpectedVersion: req.GetExpectedVersion(),
		OperatorID: op.EmployeeID,
	})
	if err != nil {
		return nil, err
	}
	return &iamv1.UpdateEmployeeResponse{Employee: employeeRowToProto(emp, nil)}, nil
}

func (h *Handler) DeactivateEmployee(ctx context.Context, req *iamv1.DeactivateEmployeeRequest) (*iamv1.DeactivateEmployeeResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.DeactivateEmployee(ctx, grpcx.TenantID(ctx), req.GetId(), op.EmployeeID); err != nil {
		return nil, err
	}
	return &iamv1.DeactivateEmployeeResponse{}, nil
}

func (h *Handler) ActivateEmployee(ctx context.Context, req *iamv1.ActivateEmployeeRequest) (*iamv1.ActivateEmployeeResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.ActivateEmployee(ctx, grpcx.TenantID(ctx), req.GetId(), op.EmployeeID); err != nil {
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
			PermissionCodes: codes[r.ID], Status: r.Status,
		}
	}
	return &iamv1.ListRolesResponse{Roles: out}, nil
}

func (h *Handler) ListAllRoles(ctx context.Context, _ *iamv1.ListAllRolesRequest) (*iamv1.ListAllRolesResponse, error) {
	roles, codes, err := h.svc.ListRolesForAdmin(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.Role, len(roles))
	for i, r := range roles {
		out[i] = &iamv1.Role{
			Id: r.ID, Code: r.Code, Name: r.Name, Description: r.Description,
			PermissionCodes: codes[r.ID], Status: r.Status,
		}
	}
	return &iamv1.ListAllRolesResponse{Roles: out}, nil
}

func (h *Handler) SetRoleStatus(ctx context.Context, req *iamv1.SetRoleStatusRequest) (*iamv1.SetRoleStatusResponse, error) {
	if err := h.svc.SetRoleStatus(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetStatus()); err != nil {
		return nil, err
	}
	return &iamv1.SetRoleStatusResponse{}, nil
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
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.GrantRolePermissions(ctx, grpcx.TenantID(ctx), req.GetRoleId(), req.GetPermissionCodes(), op.EmployeeID); err != nil {
		return nil, err
	}
	return &iamv1.GrantRolePermissionsResponse{}, nil
}

func (h *Handler) AssignEmployeeRoles(ctx context.Context, req *iamv1.AssignEmployeeRolesRequest) (*iamv1.AssignEmployeeRolesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.AssignEmployeeRoles(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetRoleIds(), op.EmployeeID); err != nil {
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
	op, _ := grpcx.OperatorFromContext(ctx)
	username, err := h.svc.OpenAccount(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), op.EmployeeID,
		req.GetUsername(), req.GetInitialPassword())
	if err != nil {
		return nil, err
	}
	return &iamv1.OpenAccountResponse{Username: username}, nil
}

func (h *Handler) ResetPassword(ctx context.Context, req *iamv1.ResetPasswordRequest) (*iamv1.ResetPasswordResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.ResetPassword(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), op.EmployeeID, req.GetNewPassword()); err != nil {
		return nil, err
	}
	return &iamv1.ResetPasswordResponse{Reset_: true}, nil
}

func (h *Handler) CreatePasswordReset(ctx context.Context, req *iamv1.CreatePasswordResetRequest) (*iamv1.CreatePasswordResetResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	reset, err := h.svc.CreatePasswordReset(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), op.EmployeeID)
	if err != nil {
		return nil, err
	}
	return &iamv1.CreatePasswordResetResponse{
		Token: reset.Token, Email: reset.Email, Name: reset.Name,
		ExpiresAt: reset.ExpiresAt.Unix(),
	}, nil
}

func (h *Handler) RecordAccountEvent(ctx context.Context, req *iamv1.RecordAccountEventRequest) (*iamv1.RecordAccountEventResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	h.svc.RecordAccountEvent(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), op.EmployeeID, req.GetAction())
	return &iamv1.RecordAccountEventResponse{Recorded: true}, nil
}

func (h *Handler) RequestPasswordReset(ctx context.Context, req *iamv1.RequestPasswordResetRequest) (*iamv1.RequestPasswordResetResponse, error) {
	reset, found, err := h.svc.RequestPasswordResetByEmail(ctx, req.GetEmail())
	if err != nil {
		return nil, err
	}
	if !found {
		return &iamv1.RequestPasswordResetResponse{Found: false}, nil
	}
	return &iamv1.RequestPasswordResetResponse{
		Found: true, Token: reset.Token, Email: reset.Email, Name: reset.Name,
		TenantId: reset.TenantID, EmployeeId: reset.EmployeeID,
		SenderEmployeeId: reset.SenderID, ExpiresAt: reset.ExpiresAt.Unix(),
	}, nil
}

func (h *Handler) PeekPasswordReset(ctx context.Context, req *iamv1.PeekPasswordResetRequest) (*iamv1.PeekPasswordResetResponse, error) {
	target, err := h.svc.PeekPasswordReset(ctx, req.GetToken())
	if err != nil {
		return nil, err
	}
	return &iamv1.PeekPasswordResetResponse{Name: target.Name, Email: target.Email}, nil
}

func (h *Handler) RedeemPasswordReset(ctx context.Context, req *iamv1.RedeemPasswordResetRequest) (*iamv1.RedeemPasswordResetResponse, error) {
	res, err := h.svc.RedeemPasswordReset(ctx, req.GetToken(), req.GetPassword())
	if err != nil {
		return nil, err
	}
	return &iamv1.RedeemPasswordResetResponse{
		Email: res.Email, TenantId: res.TenantID, EmployeeId: res.EmployeeID,
	}, nil
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
	return &iamv1.Department{
		Id: d.ID, Code: d.Code, Name: d.Name, ParentId: parent, Status: d.Status,
		Path: d.Path, Level: d.Level, SortOrder: d.SortOrder,
		LeaderEmployeeId: deref(d.LeaderEmployeeID), Version: d.Version,
	}
}

func employeeRowToProto(e store.GetEmployeeRow, roleIDs []int64) *iamv1.Employee {
	return &iamv1.Employee{
		Id: e.ID, Code: e.Code, Name: e.Name,
		DepartmentId: e.DepartmentID, DepartmentName: e.DepartmentName,
		Position: e.Position, Email: e.Email, Phone: e.Phone, Status: e.Status,
		RoleIds: roleIDs, ManagerId: deref(e.ManagerID), ManagerName: e.ManagerName,
		EmailVerified: e.EmailVerifiedAt.Valid,
		EnglishName:   e.EnglishName, HireDate: dateText(e.HireDate), LeaveDate: dateText(e.LeaveDate),
		Remark: e.Remark, Version: e.Version, AvatarKey: e.AvatarKey,
	}
}

func employeeListRowToProto(e store.ListEmployeesFilteredRow, username string, inviteExpires int64) *iamv1.Employee {
	return &iamv1.Employee{
		Id: e.ID, Code: e.Code, Name: e.Name, EnglishName: e.EnglishName,
		DepartmentId: e.DepartmentID, DepartmentName: e.DepartmentName,
		Position: e.Position, Email: e.Email, Phone: e.Phone, Status: e.Status,
		Username: username, ManagerId: deref(e.ManagerID), ManagerName: e.ManagerName,
		EmailVerified: e.EmailVerifiedAt.Valid, InviteExpiresAt: inviteExpires,
		HireDate: dateText(e.HireDate), LeaveDate: dateText(e.LeaveDate), Remark: e.Remark, Version: e.Version,
		AvatarKey: e.AvatarKey,
	}
}

func dateText(value pgtype.Date) string {
	if !value.Valid {
		return ""
	}
	return value.Time.Format(time.DateOnly)
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

func (h *Handler) ImportEmployees(ctx context.Context, req *iamv1.ImportEmployeesRequest) (*iamv1.ImportEmployeesResponse, error) {
	rows := make([]app.ImportRow, len(req.GetRows()))
	for i, r := range req.GetRows() {
		rows[i] = app.ImportRow{
			Code: r.GetCode(), Name: r.GetName(), Department: r.GetDepartment(),
			Position: r.GetPosition(), Email: r.GetEmail(), Phone: r.GetPhone(),
			ManagerCode: r.GetManagerCode(),
		}
	}
	res, err := h.svc.ImportEmployees(ctx, grpcx.TenantID(ctx), rows, req.GetDryRun())
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.ImportRowVerdict, len(res.Verdicts))
	for i, v := range res.Verdicts {
		out[i] = &iamv1.ImportRowVerdict{
			Line: v.Line, Code: v.Code, Name: v.Name, Ok: v.OK, Reason: v.Reason,
		}
	}
	return &iamv1.ImportEmployeesResponse{
		Verdicts: out, Ready: res.Ready, Blocked: res.Blocked, Imported: res.Imported,
	}, nil
}

func (h *Handler) SetManager(ctx context.Context, req *iamv1.SetManagerRequest) (*iamv1.SetManagerResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.SetManager(ctx, grpcx.TenantID(ctx), req.GetEmployeeId(), req.GetManagerId(), op.EmployeeID); err != nil {
		return nil, err
	}
	return &iamv1.SetManagerResponse{Changed: true}, nil
}

func (h *Handler) ListDirectoryChanges(ctx context.Context, req *iamv1.ListDirectoryChangesRequest) (*iamv1.ListDirectoryChangesResponse, error) {
	rows, err := h.svc.ListDirectoryChanges(ctx, grpcx.TenantID(ctx), req.GetEntityType(), req.GetEntityId())
	if err != nil {
		return nil, err
	}
	out := make([]*iamv1.DirectoryChange, 0, len(rows))
	for _, row := range rows {
		createdAt := ""
		if row.CreatedAt.Valid {
			createdAt = row.CreatedAt.Time.Format(time.RFC3339)
		}
		out = append(out, &iamv1.DirectoryChange{
			Id: row.ID, EntityType: row.EntityType, EntityId: row.EntityID, Action: row.Action,
			BeforeJson: string(row.BeforeData), AfterJson: string(row.AfterData),
			OperatorId: row.OperatorID, CreatedAt: createdAt,
		})
	}
	return &iamv1.ListDirectoryChangesResponse{Changes: out}, nil
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
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.SetDataScope(ctx, grpcx.TenantID(ctx), sc.GetRoleId(),
		sc.GetModule(), sc.GetScopeType(), sc.GetDepartmentIds(), op.EmployeeID); err != nil {
		return nil, err
	}
	return &iamv1.SetDataScopeResponse{Saved: true}, nil
}
