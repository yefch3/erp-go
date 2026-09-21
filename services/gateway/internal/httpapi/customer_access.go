package httpapi

import (
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"net/http"
)

func (s *Server) customerAccessCapabilities(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	scope, err := s.Access.VisibleEmployees(r.Context(), &iamv1.VisibleEmployeesRequest{EmployeeId: op.EmployeeID, Module: "customer"})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeJSON(w, map[string]any{"canDelete": scope.GetAll(), "tenantId": op.TenantID})
}
