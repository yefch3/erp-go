package httpapi

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"

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
