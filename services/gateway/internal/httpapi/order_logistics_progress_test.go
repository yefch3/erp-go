package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type progressOrderStub struct {
	prv1.PurchaseOrderServiceClient
	denied   bool
	noSource bool
}

func (s progressOrderStub) GetOrder(_ context.Context, r *prv1.GetOrderRequest, _ ...grpc.CallOption) (*prv1.GetOrderResponse, error) {
	if s.denied {
		return nil, status.Error(codes.PermissionDenied, "order not in scope")
	}
	reqID := int64(101)
	if s.noSource {
		reqID = 0
	}
	return &prv1.GetOrderResponse{Order: &prv1.PurchaseOrder{Id: r.Id}, Items: []*prv1.PurchaseOrderItem{{RequirementId: reqID}}}, nil
}

type progressRequirementStub struct{ prv1.RequirementServiceClient }

func (progressRequirementStub) GetRequirement(_ context.Context, _ *prv1.GetRequirementRequest, _ ...grpc.CallOption) (*prv1.GetRequirementResponse, error) {
	return &prv1.GetRequirementResponse{Requirement: &prv1.Requirement{ContractId: 7, ContractItemId: 71}}, nil
}

type progressShippingStub struct {
	shippingv1.ShippingServiceClient
	fail bool
}

func (s progressShippingStub) ListContractShippingHandoffs(_ context.Context, _ *shippingv1.ListContractShippingHandoffsRequest, _ ...grpc.CallOption) (*shippingv1.ListContractShippingHandoffsResponse, error) {
	if s.fail {
		return nil, status.Error(codes.Unavailable, "shipping offline")
	}
	return &shippingv1.ListContractShippingHandoffsResponse{Handoffs: []*shippingv1.ContractShippingHandoff{{Id: 1, ContractId: 7}, {Id: 2, ContractId: 7}, {Id: 3, ContractId: 8}}}, nil
}
func (progressShippingStub) GetContractShippingHandoff(_ context.Context, r *shippingv1.GetContractShippingHandoffRequest, _ ...grpc.CallOption) (*shippingv1.GetContractShippingHandoffResponse, error) {
	line := int64(71)
	if r.Id == 2 {
		line = 72
	}
	return &shippingv1.GetContractShippingHandoffResponse{Handoff: &shippingv1.ContractShippingHandoff{Id: r.Id, ContractId: 7, ContractNo: "CT-OWN", BatchNo: int32(r.Id), ScheduleId: 9, SignedContractName: "private.pdf", FinalFreightAmount: "900", CargoItems: []*shippingv1.ContractShippingCargoItem{{ContractItemId: line}}}}, nil
}
func (progressShippingStub) GetCargoScheduleProgress(_ context.Context, r *shippingv1.GetCargoScheduleProgressRequest, _ ...grpc.CallOption) (*shippingv1.GetCargoScheduleProgressResponse, error) {
	if r.GetScheduleId() != 9 || r.GetContractId() != 7 || len(r.GetContractItemIds()) != 1 || r.GetContractItemIds()[0] != 71 {
		return nil, status.Error(codes.PermissionDenied, "wrong cargo scope")
	}
	return &shippingv1.GetCargoScheduleProgressResponse{Schedule: &shippingv1.CargoScheduleProgress{Id: 9, ContractId: 7, ScheduleNo: "SCH-9", Status: "SAILED", Atd: "2026-11-12"}}, nil
}

func TestOrderLogisticsProgressMatchesCargoAndExcludesSensitiveFields(t *testing.T) {
	s := &Server{Orders: progressOrderStub{}, Requirements: progressRequirementStub{}, Shipping: progressShippingStub{}}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/purchase-orders/1/logistics-progress", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	s.getOrderLogisticsProgress(rec, r)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Items []orderLogisticsProgress `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].ID != "1" || body.Data.Items[0].ScheduleStatus != "SAILED" || body.Data.Items[0].ATD == "" {
		t.Fatalf("wrong cargo progress: %s", rec.Body.String())
	}
	for _, private := range []string{"private.pdf", "900", "signedContract", "freightAmount"} {
		if strings.Contains(rec.Body.String(), private) {
			t.Fatalf("leaked shipping data: %s", rec.Body.String())
		}
	}
}
func TestOrderLogisticsProgressFailsClosedAndSkipsUnlinkedOrders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		orders   progressOrderStub
		shipping shippingv1.ShippingServiceClient
		want     int
	}{
		{"order denied before shipping lookup", progressOrderStub{denied: true}, nil, 403},
		{"unlinked order needs no shipping lookup", progressOrderStub{noSource: true}, nil, 200},
		{"shipping failure is not empty success", progressOrderStub{}, progressShippingStub{fail: true}, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Orders: tc.orders, Requirements: progressRequirementStub{}, Shipping: tc.shipping}
			r := httptest.NewRequest("GET", "/api/purchase-orders/1/logistics-progress", nil)
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", "1")
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			rec := httptest.NewRecorder()
			s.getOrderLogisticsProgress(rec, r)
			if rec.Code != tc.want {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
