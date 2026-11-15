package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
)

type historicalShippingOrders struct {
	prv1.PurchaseOrderServiceClient
	listCalls int
	update    *prv1.UpdateHistoricalShippingContextRequest
}

func (s *historicalShippingOrders) ListShippingHistoricalPurchases(_ context.Context, r *prv1.ListShippingHistoricalPurchasesRequest, _ ...grpc.CallOption) (*prv1.ListShippingHistoricalPurchasesResponse, error) {
	s.listCalls++
	if r.ContractNo != "CT-OWN" {
		panic("caller controlled contract")
	}
	return &prv1.ListShippingHistoricalPurchasesResponse{Items: []*prv1.ShippingHistoricalPurchase{{PoNo: "OLD-1", SupplierName: "Factory A", ExportContractNo: "CT-OWN", DestinationPort: "Callao", Remark: "pack separately"}}}, nil
}
func (s *historicalShippingOrders) UpdateHistoricalShippingContext(_ context.Context, r *prv1.UpdateHistoricalShippingContextRequest, _ ...grpc.CallOption) (*prv1.UpdateHistoricalShippingContextResponse, error) {
	s.update = r
	return &prv1.UpdateHistoricalShippingContextResponse{}, nil
}

func historyRequest(method, path, body string, login bool) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	if login {
		r = r.WithContext(grpcx.WithOperator(r.Context(), grpcx.Operator{TenantID: 1, EmployeeID: 9}))
	}
	return r
}
func TestHandoffHistoricalPurchasesNarrowAccess(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		login, allowed, denyHandoff bool
		want, calls                 int
	}{
		{"shipping only and contract injection ignored", true, true, false, 200, 1}, {"no login", false, true, false, 401, 0}, {"no shipping permission", true, false, false, 403, 0}, {"handoff scope denied", true, true, true, 403, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orders := &historicalShippingOrders{}
			s := &Server{Orders: orders, Shipping: handoffDeliveryShipping{deny: tc.denyHandoff}, Access: officePermStub{allowed: map[string]bool{"shipping:schedule:read": tc.allowed}}}
			r := historyRequest("GET", "/api/shipping/contract-handoffs/1/historical-purchases?contract_no=OTHER", "", tc.login)
			w := httptest.NewRecorder()
			s.perm("shipping:schedule:read")(http.HandlerFunc(s.getHandoffHistoricalPurchases)).ServeHTTP(w, r)
			if w.Code != tc.want || orders.listCalls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, orders.listCalls, w.Body.String())
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
			if len(payload.Data.Items) != 1 {
				t.Fatal(payload)
			}
			row := payload.Data.Items[0]
			if len(row) != 5 {
				t.Fatalf("unexpected fields: %+v", row)
			}
			for _, key := range []string{"poNo", "supplierName", "exportContractNo", "destinationPort", "remark"} {
				if row[key] == nil {
					t.Fatalf("missing %s: %+v", key, row)
				}
			}
		})
	}
}
func TestUpdateHistoricalShippingContextPathAndPermission(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		orders := &historicalShippingOrders{}
		s := &Server{Orders: orders, Access: officePermStub{allowed: map[string]bool{"procurement:order:read": true, "procurement:order:write": allowed}}}
		r := historyRequest("PUT", "/api/purchase-orders/1/historical-shipping-context", `{"id":"999","exportContractNo":"CT-OWN","destinationPort":"Callao"}`, true)
		w := httptest.NewRecorder()
		s.perm("procurement:order:read")(s.perm("procurement:order:write")(http.HandlerFunc(s.updateHistoricalShippingContext))).ServeHTTP(w, r)
		if allowed {
			if w.Code != 200 || orders.update == nil || orders.update.Id != 1 || orders.update.ExportContractNo != "CT-OWN" || orders.update.DestinationPort != "Callao" {
				t.Fatalf("not forwarded: %d %+v", w.Code, orders.update)
			}
		} else if w.Code != 403 || orders.update != nil {
			t.Fatal("write permission bypass")
		}
	}
}

func TestHistoricalShippingContextRejectsFinancialFields(t *testing.T) {
	orders := &historicalShippingOrders{}
	s := &Server{Orders: orders}
	w := httptest.NewRecorder()
	s.updateHistoricalShippingContext(w, historyRequest("PUT", "/api/purchase-orders/1/historical-shipping-context", `{"exportContractNo":"CT-OWN","destinationPort":"Callao","unitPrice":"1"}`, true))
	if w.Code != 400 || orders.update != nil {
		t.Fatalf("financial field accepted: %d %s", w.Code, w.Body.String())
	}
}
