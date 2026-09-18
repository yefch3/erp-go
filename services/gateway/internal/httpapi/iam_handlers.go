package httpapi

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"sort"
	"strconv"
	"strings"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// ---------------------------------------------------------------- directory

func (s *Server) listDepartments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Directory.ListDepartments(r.Context(), &iamv1.ListDepartmentsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createDepartment(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.CreateDepartmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Directory.CreateDepartment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateDepartment(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.UpdateDepartmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Directory.UpdateDepartment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listEmployees(w http.ResponseWriter, r *http.Request) {
	departmentID, _ := strconv.ParseInt(r.URL.Query().Get("department_id"), 10, 64)
	resp, err := s.Directory.ListEmployees(r.Context(), &iamv1.ListEmployeesRequest{
		Page:             pageFromQuery(r),
		DepartmentId:     departmentID,
		Keyword:          r.URL.Query().Get("keyword"),
		ManagerId:        int64FromQuery(r, "manager_id"),
		RoleId:           int64FromQuery(r, "role_id"),
		AccountStatus:    r.URL.Query().Get("account_status"),
		EmploymentStatus: r.URL.Query().Get("employment_status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateEmployee(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.UpdateEmployeeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Directory.UpdateEmployee(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getEmployee(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Directory.GetEmployee(r.Context(), &iamv1.GetEmployeeRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createEmployee(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.CreateEmployeeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Directory.CreateEmployee(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateEmployee(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r)
	resp, err := s.Directory.DeactivateEmployee(r.Context(), &iamv1.DeactivateEmployeeRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// Marking somebody as left already stops them signing in again. It did
	// nothing about the session they are holding right now, so somebody
	// walked out at noon and kept reading customer correspondence until their
	// token expired the next morning. Closing the door and clearing the room
	// are two separate acts and this is where they belong together.
	//
	// After the deactivation, not before: revoking first and then failing to
	// deactivate would sign somebody out who is still employed. Best-effort,
	// because the deactivation has already happened and reporting it as a
	// failure would invite an administrator to press it again — the honest
	// remedy is the 结束登录 button, which is right there.
	if s.Revocations != nil {
		op, _ := grpcx.OperatorFromContext(r.Context())
		if rErr := s.Revocations.Revoke(r.Context(), op.TenantID, id); rErr != nil {
			s.Log.Error("employee deactivated but their sessions were not revoked",
				"employee", id, "err", rErr)
		}
	}
	s.writeProto(w, resp)
}

func (s *Server) activateEmployee(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Directory.ActivateEmployee(r.Context(), &iamv1.ActivateEmployeeRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) openAccount(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.OpenAccountRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.EmployeeId = idFromPath(r)
	resp, err := s.Directory.OpenAccount(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.ResetPasswordRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.EmployeeId = idFromPath(r)
	resp, err := s.Directory.ResetPassword(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// changeOwnPassword needs no permission code: it only ever touches the
// caller's own account, which iam derives from the propagated identity.
func (s *Server) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.ChangePasswordRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Directory.ChangePassword(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) assignRoles(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.AssignEmployeeRolesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.EmployeeId = idFromPath(r)
	resp, err := s.Access.AssignEmployeeRoles(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- access

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Access.ListRoles(r.Context(), &iamv1.ListRolesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// 角色管理页要看到停用的角色——否则停掉之后没有任何入口能把它启用回来。
// 别处（分配角色的候选、审批按编码找角色）一律只拿启用的。
func (s *Server) listAllRoles(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Access.ListAllRoles(r.Context(), &iamv1.ListAllRolesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setRoleStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "IAM_ROLE_ID_INVALID", "角色编号不正确")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_JSON", "请求格式不正确")
		return
	}
	resp, err := s.Access.SetRoleStatus(r.Context(), &iamv1.SetRoleStatusRequest{
		Id: id, Status: input.Status,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.CreateRoleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Access.CreateRole(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) grantRolePermissions(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.GrantRolePermissionsRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.RoleId = idFromPath(r)
	resp, err := s.Access.GrantRolePermissions(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listRoleMembers(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Access.ListRoleMembers(r.Context(), &iamv1.ListRoleMembersRequest{RoleId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listPermissions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Access.ListPermissions(r.Context(), &iamv1.ListPermissionsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// accessFinding is deliberately phrased for an administrator rather than as
// an internal permission-engine error.  This endpoint exists to answer the
// practical question “why can this employee not see it?” from the values the
// services actually enforce, not from a second set of guessed UI rules.
type accessFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Action   string `json:"action,omitempty"`
}

type diagnosticRole struct {
	ID     string `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type diagnosticScope struct {
	Module       string `json:"module"`
	ScopeType    string `json:"scopeType"`
	Configured   bool   `json:"configured"`
	VisibleCount int    `json:"visibleCount,omitempty"`
	All          bool   `json:"all"`
}

type permissionGroup struct {
	Module string   `json:"module"`
	Codes  []string `json:"codes"`
}

type employeeAccessDiagnostic struct {
	EmployeeID      string            `json:"employeeId"`
	EmployeeName    string            `json:"employeeName"`
	EmployeeStatus  string            `json:"employeeStatus"`
	Username        string            `json:"username"`
	ManagerID       string            `json:"managerId"`
	ManagerName     string            `json:"managerName"`
	SuperAdmin      bool              `json:"superAdmin"`
	CanRecover      bool              `json:"canRecoverApprovals"`
	Roles           []diagnosticRole  `json:"roles"`
	PermissionCount int               `json:"permissionCount"`
	Permissions     []permissionGroup `json:"permissions"`
	Scopes          []diagnosticScope `json:"scopes"`
	Findings        []accessFinding   `json:"findings"`
}

var diagnosticScopeModules = []string{
	"export", "procurement_requirement", "procurement_sourcing",
	"procurement_order", "shipping", "quality", "mail",
}

var diagnosticScopePermissionPrefixes = map[string][]string{
	"export":                  {"export:", "sales:"},
	"procurement_requirement": {"procurement:requirement:"},
	"procurement_sourcing":    {"procurement:sourcing:"},
	"procurement_order":       {"procurement:order:"},
	"shipping":                {"shipping:"},
	"quality":                 {"quality:"},
	"mail":                    {"mail:"},
}

var diagnosticScopeNames = map[string]string{
	"export":                  "销售与出口",
	"procurement_requirement": "采购需求",
	"procurement_sourcing":    "采购寻源",
	"procurement_order":       "采购订单",
	"shipping":                "船期管理",
	"quality":                 "质检任务",
	"mail":                    "邮件",
}

func employeeUsesScope(module string, permissions map[string]bool) bool {
	for code := range permissions {
		for _, prefix := range diagnosticScopePermissionPrefixes[module] {
			if strings.HasPrefix(code, prefix) {
				return true
			}
		}
	}
	return false
}

// diagnoseEmployeeAccess returns the final, effective access of one employee:
// enabled roles, the permissions those roles currently grant, and the data
// scope IAM will actually apply.  It is read-only and is guarded by both
// employee and role administration permissions in the router.
func (s *Server) diagnoseEmployeeAccess(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r)
	detail, err := s.Directory.GetEmployee(r.Context(), &iamv1.GetEmployeeRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	emp := detail.GetEmployee()
	rolesResp, err := s.Access.ListAllRoles(r.Context(), &iamv1.ListAllRolesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// Recovery buttons describe the CALLER, not the employee being inspected.
	// Confusing those two would hide recovery exactly when an administrator
	// opens a departed ordinary employee, and show it when inspecting another
	// super administrator.
	op, _ := grpcx.OperatorFromContext(r.Context())
	actor := emp
	if op.EmployeeID != emp.GetId() {
		actorDetail, actorErr := s.Directory.GetEmployee(r.Context(), &iamv1.GetEmployeeRequest{Id: op.EmployeeID})
		if actorErr != nil {
			s.writeGRPCError(w, actorErr)
			return
		}
		actor = actorDetail.GetEmployee()
	}
	actorRoleIDs := make(map[int64]bool, len(actor.GetRoleIds()))
	for _, roleID := range actor.GetRoleIds() {
		actorRoleIDs[roleID] = true
	}
	callerSuperAdmin := false
	for _, role := range rolesResp.GetRoles() {
		if actorRoleIDs[role.GetId()] && role.GetStatus() == "ACTIVE" && role.GetCode() == "SUPER_ADMIN" {
			callerSuperAdmin = true
			break
		}
	}
	permsResp, err := s.Access.ListEmployeePermissions(r.Context(), &iamv1.ListEmployeePermissionsRequest{EmployeeId: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allPerms, err := s.Access.ListPermissions(r.Context(), &iamv1.ListPermissionsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	scopesResp, err := s.Access.ListDataScopes(r.Context(), &iamv1.ListDataScopesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}

	result := employeeAccessDiagnostic{
		EmployeeID: strconv.FormatInt(emp.GetId(), 10), EmployeeName: emp.GetName(),
		EmployeeStatus: emp.GetStatus(), Username: emp.GetUsername(),
		ManagerID: strconv.FormatInt(emp.GetManagerId(), 10), ManagerName: emp.GetManagerName(),
		CanRecover: callerSuperAdmin,
		Roles:      []diagnosticRole{}, Permissions: []permissionGroup{}, Scopes: []diagnosticScope{},
		Findings: []accessFinding{},
	}
	assigned := make(map[int64]bool, len(emp.GetRoleIds()))
	activeAssigned := make(map[int64]bool, len(emp.GetRoleIds()))
	for _, roleID := range emp.GetRoleIds() {
		assigned[roleID] = true
	}
	for _, role := range rolesResp.GetRoles() {
		if !assigned[role.GetId()] {
			continue
		}
		result.Roles = append(result.Roles, diagnosticRole{
			ID: strconv.FormatInt(role.GetId(), 10), Code: role.GetCode(), Name: role.GetName(), Status: role.GetStatus(),
		})
		if role.GetStatus() == "ACTIVE" {
			activeAssigned[role.GetId()] = true
			if role.GetCode() == "SUPER_ADMIN" {
				result.SuperAdmin = true
			}
		} else {
			result.Findings = append(result.Findings, accessFinding{
				Code: "INACTIVE_ROLE", Severity: "warning",
				Message: "员工仍分配了已停用角色“" + role.GetName() + "”，该角色不会授予任何权限。",
				Action:  "启用该角色，或为员工改分配一个启用中的岗位角色。",
			})
		}
	}

	if emp.GetStatus() != "ACTIVE" {
		result.Findings = append(result.Findings, accessFinding{Code: "EMPLOYEE_INACTIVE", Severity: "error", Message: "员工已离职，现有登录令牌和全部角色权限都会失效。", Action: "确认人员状态；需要继续使用时先恢复在职。"})
	}
	if strings.TrimSpace(emp.GetUsername()) == "" {
		result.Findings = append(result.Findings, accessFinding{Code: "ACCOUNT_UNOPENED", Severity: "error", Message: "员工还没有可登录的系统账号。", Action: "在员工管理中开通账号或重新发送邀请。"})
	}
	if len(activeAssigned) == 0 {
		result.Findings = append(result.Findings, accessFinding{Code: "NO_ACTIVE_ROLE", Severity: "error", Message: "员工没有任何启用中的岗位角色。", Action: "为员工分配至少一个启用中的岗位角色。"})
	}
	if emp.GetManagerId() == 0 && !result.SuperAdmin {
		result.Findings = append(result.Findings, accessFinding{Code: "NO_MANAGER", Severity: "warning", Message: "员工没有直属上级；使用“直属上级审批”的流程可能找不到审批人。", Action: "在组织关系中设置直属上级，或在审批设置中使用岗位审批人。"})
	}

	permissionSet := make(map[string]bool, len(permsResp.GetPermissionCodes()))
	for _, code := range permsResp.GetPermissionCodes() {
		permissionSet[code] = true
	}
	result.PermissionCount = len(permissionSet)
	grouped := map[string][]string{}
	for _, permission := range allPerms.GetPermissions() {
		if permissionSet[permission.GetCode()] {
			grouped[permission.GetModule()] = append(grouped[permission.GetModule()], permission.GetCode())
		}
	}
	groupNames := make([]string, 0, len(grouped))
	for module := range grouped {
		groupNames = append(groupNames, module)
	}
	sort.Strings(groupNames)
	for _, module := range groupNames {
		sort.Strings(grouped[module])
		result.Permissions = append(result.Permissions, permissionGroup{Module: module, Codes: grouped[module]})
	}

	explicitScope := map[string]bool{}
	for _, scope := range scopesResp.GetScopes() {
		if activeAssigned[scope.GetRoleId()] {
			explicitScope[scope.GetModule()] = true
		}
	}
	for _, module := range diagnosticScopeModules {
		visible, err := s.Access.VisibleEmployees(r.Context(), &iamv1.VisibleEmployeesRequest{EmployeeId: id, Module: module})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		result.Scopes = append(result.Scopes, diagnosticScope{
			Module: module, ScopeType: visible.GetScopeType(), Configured: explicitScope[module],
			VisibleCount: len(visible.GetEmployeeIds()), All: visible.GetAll(),
		})
		if !explicitScope[module] && !result.SuperAdmin && employeeUsesScope(module, permissionSet) {
			result.Findings = append(result.Findings, accessFinding{
				Code: "DEFAULT_SELF_SCOPE", Severity: "info",
				Message: "“" + diagnosticScopeNames[module] + "”没有配置数据范围，系统当前按“仅本人数据”处理。",
				Action:  "若岗位需要查看同部门或全公司数据，请在岗位权限中设置对应范围。",
			})
		}
	}
	if result.SuperAdmin && len(permissionSet) != len(allPerms.GetPermissions()) {
		result.Findings = append(result.Findings, accessFinding{Code: "SUPER_ADMIN_PERMISSION_DRIFT", Severity: "error", Message: "超级管理员的生效权限不完整。", Action: "重启 IAM 服务以运行自动修复；若仍存在，请检查角色数据。"})
	}
	if len(result.Findings) == 0 {
		result.Findings = append(result.Findings, accessFinding{Code: "ACCESS_OK", Severity: "success", Message: "账号、岗位权限和数据范围均已生效，未发现基础权限异常。"})
	}
	s.writeJSON(w, result)
}

// me returns the caller's own identity and permission codes, so a page can
// re-read them after a role change without logging out and in again.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	resp, err := s.Access.ListEmployeePermissions(r.Context(), &iamv1.ListEmployeePermissionsRequest{
		EmployeeId: op.EmployeeID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setManager(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ManagerID string `json:"managerId"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	managerID, _ := strconv.ParseInt(body.ManagerID, 10, 64)
	resp, err := s.Directory.SetManager(r.Context(), &iamv1.SetManagerRequest{
		EmployeeId: idFromPath(r), ManagerId: managerID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listDepartmentChanges(w http.ResponseWriter, r *http.Request) {
	s.listDirectoryChanges(w, r, "DEPARTMENT")
}

func (s *Server) listEmployeeChanges(w http.ResponseWriter, r *http.Request) {
	s.listDirectoryChanges(w, r, "EMPLOYEE")
}

func (s *Server) listDirectoryChanges(w http.ResponseWriter, r *http.Request, entityType string) {
	resp, err := s.Directory.ListDirectoryChanges(r.Context(), &iamv1.ListDirectoryChangesRequest{
		EntityType: entityType, EntityId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func int64FromQuery(r *http.Request, key string) int64 {
	value, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return value
}
