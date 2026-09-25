package httpapi

import (
	"net/http"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (s *Server) masterDeleteAllowed(w http.ResponseWriter, r *http.Request) (bool, bool) {
	op, ok := grpcx.OperatorFromContext(r.Context())
	if !ok || op.EmployeeID <= 0 || op.TenantID <= 0 || s.Access == nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false, false
	}
	scope, err := s.Access.VisibleEmployees(r.Context(), &iamv1.VisibleEmployeesRequest{EmployeeId: op.EmployeeID, Module: "customer"})
	if err != nil {
		s.writeGRPCError(w, err)
		return false, false
	}
	return scope.GetAll(), true
}
func (s *Server) masterDeleteAccess(w http.ResponseWriter, r *http.Request) {
	allowed, ok := s.masterDeleteAllowed(w, r)
	if ok {
		s.writeJSON(w, map[string]bool{"allowed": allowed})
	}
}
func (s *Server) masterDeletePreview(w http.ResponseWriter, r *http.Request) {
	s.masterDelete(w, r, false)
}
func (s *Server) masterDeleteExecute(w http.ResponseWriter, r *http.Request) {
	s.masterDelete(w, r, true)
}
func (s *Server) masterDelete(w http.ResponseWriter, r *http.Request, execute bool) {
	allowed, ok := s.masterDeleteAllowed(w, r)
	if !ok {
		return
	}
	if !allowed {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	in := &mdv1.BulkDeleteMasterDataRequest{}
	if !s.decodeBody(w, r, in) {
		return
	}
	in.Execute = execute
	if s.MasterDelete == nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	out, err := s.MasterDelete.BulkDeleteMasterData(r.Context(), in)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, out)
}
