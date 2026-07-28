package httpapi

import (
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

func (s *Server) listEmployees(w http.ResponseWriter, r *http.Request) {
	departmentID, _ := strconv.ParseInt(r.URL.Query().Get("department_id"), 10, 64)
	resp, err := s.Directory.ListEmployees(r.Context(), &iamv1.ListEmployeesRequest{
		Page:         pageFromQuery(r),
		DepartmentId: departmentID,
		Keyword:      r.URL.Query().Get("keyword"),
	})
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
	resp, err := s.Directory.DeactivateEmployee(r.Context(), &iamv1.DeactivateEmployeeRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
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
