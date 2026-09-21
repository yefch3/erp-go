package httpapi

import (
	"context"
	"encoding/json"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
	"net/http/httptest"
	"testing"
)

type customerCapabilityScope struct {
	iamv1.AccessServiceClient
	all           bool
	tenant, actor int64
}

func (s *customerCapabilityScope) VisibleEmployees(ctx context.Context, req *iamv1.VisibleEmployeesRequest, _ ...grpc.CallOption) (*iamv1.VisibleEmployeesResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	s.tenant = op.TenantID
	s.actor = req.EmployeeId
	if req.Module != "customer" {
		panic("wrong module")
	}
	return &iamv1.VisibleEmployeesResponse{All: s.all}, nil
}
func TestCustomerCapabilitiesUseTenantScopedHighestRole(t *testing.T) {
	for _, all := range []bool{false, true} {
		access := &customerCapabilityScope{all: all}
		server := &Server{Access: access}
		req := httptest.NewRequest("GET", "/api/customers/access", nil)
		req = req.WithContext(grpcx.WithOperator(req.Context(), grpcx.Operator{TenantID: 21, EmployeeID: 7}))
		w := httptest.NewRecorder()
		server.customerAccessCapabilities(w, req)
		var body struct {
			Success bool `json:"success"`
			Data    struct {
				CanDelete bool `json:"canDelete"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if !body.Success || body.Data.CanDelete != all || access.tenant != 21 || access.actor != 7 {
			t.Fatalf("wrong capability response: %s", w.Body.String())
		}
	}
}
