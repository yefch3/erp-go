package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// All full-order methods deliberately remain nil: calling one panics.
type remarksOnlyOrders struct {
	prv1.PurchaseOrderServiceClient
	calls int
	deny  bool
}

func (s *remarksOnlyOrders) ListShippingPurchaseRemarks(_ context.Context, r *prv1.ListShippingPurchaseRemarksRequest, _ ...grpc.CallOption) (*prv1.ListShippingPurchaseRemarksResponse, error) {
	s.calls++
	if r.ContractId != 7 || r.ContractNo != "CT-OWN" {
		panic("caller-controlled contract reached procurement")
	}
	if s.deny {
		return nil, status.Error(codes.PermissionDenied, "denied")
	}
	return &prv1.ListShippingPurchaseRemarksResponse{Items: []*prv1.ShippingPurchaseRemark{
		{SupplierName: "Factory A", PurchaseOrderRemark: "A packing\nno mixing"},
		{SupplierName: "Factory B", PurchaseOrderRemark: "B packing"},
	}}, nil
}

func TestShippingOnlyPurchaseRemarksBoundary(t *testing.T) {
	for _, tc := range []struct {
		name                                       string
		login, allowed, handoffDenied, orderDenied bool
		want, calls                                int
	}{
		{"shipping only and injected contract ignored", true, true, false, false, 200, 1},
		{"no shipping permission", true, false, false, false, 403, 0},
		{"no login", false, true, false, false, 401, 0},
		{"handoff rejected", true, true, true, false, 403, 0},
		{"projection rejected", true, true, false, true, 403, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orders := &remarksOnlyOrders{deny: tc.orderDenied}
			s := &Server{Orders: orders, Shipping: handoffDeliveryShipping{deny: tc.handoffDenied}, Access: officePermStub{allowed: map[string]bool{"shipping:schedule:read": tc.allowed}}}
			r := httptest.NewRequest("GET", "/api/shipping/contract-handoffs/1/purchase-remarks?contract_id=999&contract_no=CT-OTHER", nil)
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", "1")
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			if tc.login {
				r = r.WithContext(grpcx.WithOperator(r.Context(), grpcx.Operator{TenantID: 1, EmployeeID: 9}))
			}
			w := httptest.NewRecorder()
			s.perm("shipping:schedule:read")(http.HandlerFunc(s.getHandoffPurchaseRemarks)).ServeHTTP(w, r)
			if w.Code != tc.want || orders.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, orders.calls, w.Body.String())
			}
			if tc.want != 200 {
				return
			}
			var payload struct {
				Data struct {
					Items []map[string]any `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Data.Items) != 2 {
				t.Fatal(payload)
			}
			for _, row := range payload.Data.Items {
				if len(row) != 2 || row["supplierName"] == nil || row["purchaseOrderRemark"] == nil {
					t.Fatalf("unexpected field exposure: %+v", row)
				}
			}
		})
	}
}
