package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
)

type supplierCapabilityScope struct {
	iamv1.AccessServiceClient
	all           bool
	tenant, actor int64
}

func (s *supplierCapabilityScope) VisibleEmployees(ctx context.Context, req *iamv1.VisibleEmployeesRequest, _ ...grpc.CallOption) (*iamv1.VisibleEmployeesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	s.tenant = op.TenantID
	s.actor = req.EmployeeId
	if req.Module != "supplier" {
		panic("wrong module")
	}
	return &iamv1.VisibleEmployeesResponse{All: s.all}, nil
}

func TestSupplierCapabilitiesUseTenantScopedHighestRole(t *testing.T) {
	for _, all := range []bool{false, true} {
		access := &supplierCapabilityScope{all: all}
		server := &Server{Access: access}
		req := httptest.NewRequest("GET", "/api/suppliers/access", nil)
		req = req.WithContext(grpcx.WithOperator(req.Context(), grpcx.Operator{TenantID: 21, EmployeeID: 7}))
		w := httptest.NewRecorder()
		server.supplierAccessCapabilities(w, req)
		var body struct {
			Success bool `json:"success"`
			Data    struct {
				CanDelete       bool `json:"canDelete"`
				CanManageOwners bool `json:"canManageOwners"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if !body.Success || body.Data.CanDelete != all || body.Data.CanManageOwners != all || access.tenant != 21 || access.actor != 7 {
			t.Fatalf("wrong capability response: %s", w.Body.String())
		}
	}
}
