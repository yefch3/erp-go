package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type handoffDeliveryOrders struct {
	prv1.PurchaseOrderServiceClient
	deny bool
}

func (s handoffDeliveryOrders) ListOrders(_ context.Context, r *prv1.ListOrdersRequest, _ ...grpc.CallOption) (*prv1.ListOrdersResponse, error) {
	if r.GetPage().GetPage() == 1 {
		return &prv1.ListOrdersResponse{Meta: &commonv1.PageMeta{Total: 101}}, nil
	}
	return &prv1.ListOrdersResponse{Meta: &commonv1.PageMeta{Total: 101}, Orders: []*prv1.PurchaseOrder{{Id: 1, Status: "PARTIALLY_RECEIVED", FulfillmentMode: "DIRECT_SHIP", SourceBusinessId: 7}, {Id: 2, Status: "ORDERED", SourceBusinessId: 8}, {Id: 3, Status: "DRAFT", SourceBusinessId: 7}, {Id: 4, Status: "RECEIVED", FulfillmentMode: "WAREHOUSE", SourceBusinessId: 7}}}, nil
}
func (s handoffDeliveryOrders) GetOrder(_ context.Context, r *prv1.GetOrderRequest, _ ...grpc.CallOption) (*prv1.GetOrderResponse, error) {
	if s.deny {
		return nil, status.Error(codes.PermissionDenied, "out of scope")
	}
	if r.Id != 1 {
		panic("unrelated order fetched")
	}
	return &prv1.GetOrderResponse{Order: &prv1.PurchaseOrder{Id: 1, PoNo: "PO-ONE", TotalAmount: "PRIVATE-COST"}, Items: []*prv1.PurchaseOrderItem{{ContractNo: "CT-OWN", ProductName: "Steel", Spec: "3mm", UomCode: "MT", Qty: "10", ReceivedQty: "4", UnitPrice: "PRIVATE-PRICE"}, {ContractNo: "CT-OTHER", ProductName: "Steel", Spec: "3mm", UomCode: "MT", Qty: "99"}, {ContractNo: "CT-OWN", ProductName: "Other", Spec: "3mm", UomCode: "MT", Qty: "88"}}}, nil
}

type handoffDeliveryShipping struct {
	shippingv1.ShippingServiceClient
	deny bool
}

func (s handoffDeliveryShipping) GetContractShippingHandoff(context.Context, *shippingv1.GetContractShippingHandoffRequest, ...grpc.CallOption) (*shippingv1.GetContractShippingHandoffResponse, error) {
	if s.deny {
		return nil, status.Error(codes.PermissionDenied, "handoff denied")
	}
	return &shippingv1.GetContractShippingHandoffResponse{Handoff: &shippingv1.ContractShippingHandoff{ContractId: 7, ContractNo: "CT-OWN", CargoItems: []*shippingv1.ContractShippingCargoItem{{ContractItemId: 71, ProductName: "Steel", Specification: "3mm", UomCode: "MT"}}}}, nil
}
func TestHandoffPurchaseDeliveriesScopeAndProjection(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		orderDenied, handoffDenied bool
		want                       int
	}{{"matched goods across pages", false, false, 200}, {"order scope denied", true, false, 403}, {"handoff scope denied", false, true, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Orders: handoffDeliveryOrders{deny: tc.orderDenied}, Shipping: handoffDeliveryShipping{deny: tc.handoffDenied}}
			r := httptest.NewRequest("GET", "/api/shipping/contract-handoffs/1/purchase-deliveries", nil)
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", "1")
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			w := httptest.NewRecorder()
			s.getHandoffPurchaseDeliveries(w, r)
			body := w.Body.String()
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, body)
			}
			if tc.want == 200 && (!strings.Contains(body, `"deliveredQty":"4"`) || strings.Contains(body, "PRIVATE") || strings.Contains(body, `"99"`) || strings.Contains(body, `"88"`)) {
				t.Fatalf("wrong projection %s", body)
			}
		})
	}
}
