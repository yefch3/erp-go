package httpapi

import (
	"net/http"
	"strconv"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (s *Server) myTodoReception(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	s.readTodoReception(w, r, &iamv1.GetTodoReceptionRequest{EmployeeId: op.EmployeeID})
}

func (s *Server) readTodoReception(w http.ResponseWriter, r *http.Request, req *iamv1.GetTodoReceptionRequest) {
	response, err := s.Access.GetTodoReception(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, response)
}

func (s *Server) roleTodoReception(w http.ResponseWriter, r *http.Request) {
	s.readTodoReception(w, r, &iamv1.GetTodoReceptionRequest{RoleId: idFromPath(r)})
}

func (s *Server) employeeTodoReception(w http.ResponseWriter, r *http.Request) {
	s.readTodoReception(w, r, &iamv1.GetTodoReceptionRequest{EmployeeId: idFromPath(r)})
}

func (s *Server) saveRoleTodoReception(w http.ResponseWriter, r *http.Request) {
	s.saveTodoReception(w, r, true)
}

func (s *Server) saveEmployeeTodoReception(w http.ResponseWriter, r *http.Request) {
	s.saveTodoReception(w, r, false)
}

func (s *Server) saveTodoReception(w http.ResponseWriter, r *http.Request, role bool) {
	request := &iamv1.SetTodoReceptionRequest{}
	if !s.decodeBody(w, r, request) {
		return
	}
	request.RoleId, request.EmployeeId = 0, 0
	if role {
		request.RoleId = idFromPath(r)
	} else {
		request.EmployeeId = idFromPath(r)
	}
	response, err := s.Access.SetTodoReception(r.Context(), request)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, response)
}

// The dispatch preview exposes only the flag needed for a warning.
func (s *Server) checkAssignmentTodoReception(w http.ResponseWriter, r *http.Request) {
	var request struct {
		EmployeeIDs []string `json:"employeeIds"`
	}
	if !s.decodeJSON(w, r, &request) {
		return
	}
	if len(request.EmployeeIDs) > 100 {
		s.writeError(w, http.StatusBadRequest, "TODO_RECIPIENT_LIMIT", "一次最多检查 100 位负责人")
		return
	}
	result := make(map[string]bool, len(request.EmployeeIDs))
	for _, raw := range request.EmployeeIDs {
		if _, ok := result[raw]; ok {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			s.writeError(w, http.StatusBadRequest, "TODO_RECIPIENT_INVALID", "负责人编号无效")
			return
		}
		setting, err := s.Access.GetTodoReception(r.Context(), &iamv1.GetTodoReceptionRequest{EmployeeId: id})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		result[raw] = setting.GetReception().GetApprovalsOnly()
	}
	s.writeJSON(w, map[string]any{"approvalsOnly": result})
}
