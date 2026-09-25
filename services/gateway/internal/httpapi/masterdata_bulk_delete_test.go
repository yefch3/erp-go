package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
)

type bulkDeleteStub struct {
	mdv1.MasterDataBulkDeleteServiceClient
	called  bool
	execute bool
	tenant  int64
}

func (s *bulkDeleteStub) BulkDeleteMasterData(ctx context.Context, in *mdv1.BulkDeleteMasterDataRequest, _ ...grpc.CallOption) (*mdv1.BulkDeleteMasterDataResponse, error) {
	s.called = true
	s.execute = in.Execute
	s.tenant = grpcx.TenantID(ctx)
	return &mdv1.BulkDeleteMasterDataResponse{}, nil
}
func TestMasterDeleteRequiresHighestRoleAndForcesPreview(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		client := &bulkDeleteStub{}
		server := &Server{Access: &customerCapabilityScope{all: allowed}, MasterDelete: client}
		req := httptest.NewRequest("POST", "/api/masterdata/bulk-delete/preview", strings.NewReader(`{"entity":"CUSTOMER","execute":true}`))
		req = req.WithContext(grpcx.WithOperator(req.Context(), grpcx.Operator{TenantID: 24, EmployeeID: 7}))
		w := httptest.NewRecorder()
		server.masterDeletePreview(w, req)
		if !allowed {
			if w.Code != 403 || client.called {
				t.Fatal("ordinary user reached deletion")
			}
		} else if w.Code != 200 || !client.called || client.execute || client.tenant != 24 {
			t.Fatalf("preview trust boundary failed: %d %+v", w.Code, client)
		}
	}
}
