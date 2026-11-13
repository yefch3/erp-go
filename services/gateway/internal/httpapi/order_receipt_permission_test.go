package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
)

type receiptPermissionOrders struct {
	prv1.PurchaseOrderServiceClient
	mode     string
	received bool
}

func (s *receiptPermissionOrders) GetOrder(context.Context, *prv1.GetOrderRequest, ...grpc.CallOption) (*prv1.GetOrderResponse, error) {
	return &prv1.GetOrderResponse{Order: &prv1.PurchaseOrder{FulfillmentMode: s.mode}}, nil
}
func (s *receiptPermissionOrders) ReceiveOrder(context.Context, *prv1.ReceiveOrderRequest, ...grpc.CallOption) (*prv1.ReceiveOrderResponse, error) {
	s.received = true
	return &prv1.ReceiveOrderResponse{ReceiptNo: "TEST"}, nil
}
func TestReceiptPermissionUsesPersistedFulfillmentMode(t *testing.T) {
	for _, tc := range []struct {
		name, mode, permission string
		warehouse              int
		status                 int
	}{
		{"buyer confirms factory delivery", "DIRECT_SHIP", "procurement:order:write", 0, 200},
		{"logistics cannot confirm factory delivery", "DIRECT_SHIP", "procurement:receipt:write", 0, 403},
		{"warehouse ID cannot disguise factory delivery", "DIRECT_SHIP", "procurement:receipt:write", 4, 403},
		{"buyer cannot post factory delivery into warehouse", "DIRECT_SHIP", "procurement:order:write", 4, 400},
		{"warehouse staff retain receipt permission", "WAREHOUSE", "procurement:receipt:write", 4, 200},
		{"buyer cannot receive warehouse stock", "WAREHOUSE", "procurement:order:write", 4, 403},
		{"buyer cannot disguise warehouse as direct", "WAREHOUSE", "procurement:order:write", 0, 403},
		{"warehouse mode must match even with permission", "WAREHOUSE", "procurement:receipt:write", 0, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orders := &receiptPermissionOrders{mode: tc.mode}
			s := &Server{Orders: orders, Access: officePermStub{allowed: map[string]bool{tc.permission: true}}}
			body := `{"warehouse_id":0,"lines":[]}`
			if tc.warehouse > 0 {
				body = `{"warehouse_id":4,"lines":[]}`
			}
			r := httptest.NewRequest("POST", "/api/purchase-orders/1/receive", strings.NewReader(body))
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", "1")
			ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
			ctx = grpcx.WithOperator(ctx, grpcx.Operator{TenantID: 1, EmployeeID: 9})
			r = r.WithContext(ctx)
			w := httptest.NewRecorder()
			s.receiveOrder(w, r)
			if w.Code != tc.status || orders.received != (tc.status == 200) {
				t.Fatalf("status=%d received=%v body=%s", w.Code, orders.received, w.Body.String())
			}
		})
	}
}
