package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
)

type contractSchedulesClient struct {
	shippingv1.ShippingServiceClient
}

func (contractSchedulesClient) ListContractShippingHandoffs(context.Context, *shippingv1.ListContractShippingHandoffsRequest, ...grpc.CallOption) (*shippingv1.ListContractShippingHandoffsResponse, error) {
	return &shippingv1.ListContractShippingHandoffsResponse{Handoffs: []*shippingv1.ContractShippingHandoff{
		{Id: 1, ContractId: 3, BatchNo: 2, ScheduleId: 9, CargoItems: []*shippingv1.ContractShippingCargoItem{{ProductName: "钢板", Specification: "5mm", Quantity: "20", UomCode: "MT"}}},
		{Id: 2, ContractId: 77, BatchNo: 1, ScheduleId: 15},
		{Id: 3, ContractId: 3, BatchNo: 1, ScheduleId: 8, CargoItems: []*shippingv1.ContractShippingCargoItem{{ProductName: "钢板", Specification: "3mm", Quantity: "30", UomCode: "MT"}}},
	}}, nil
}

func (contractSchedulesClient) GetSchedule(_ context.Context, req *shippingv1.GetScheduleRequest, _ ...grpc.CallOption) (*shippingv1.GetScheduleResponse, error) {
	state := "IN_TRANSIT"
	if req.Id == 9 {
		state = "COMPLETED"
	}
	return &shippingv1.GetScheduleResponse{Schedule: &shippingv1.Schedule{Id: req.Id, ContractId: 3, ScheduleNo: "SCH-TEST", Status: state, FreightAmount: "9999.99"}}, nil
}

func (contractSchedulesClient) GetContractShippingHandoff(ctx context.Context, req *shippingv1.GetContractShippingHandoffRequest, opts ...grpc.CallOption) (*shippingv1.GetContractShippingHandoffResponse, error) {
	list, err := (contractSchedulesClient{}).ListContractShippingHandoffs(ctx, &shippingv1.ListContractShippingHandoffsRequest{}, opts...)
	for _, h := range list.GetHandoffs() {
		if h.Id == req.Id {
			return &shippingv1.GetContractShippingHandoffResponse{Handoff: h}, err
		}
	}
	return &shippingv1.GetContractShippingHandoffResponse{}, err
}

func TestContractDetailsIncludeIndependentFormalBatches(t *testing.T) {
	s := &Server{Contracts: contractVesselVisibilityStub{}, Shipments: &contractVesselShipmentStub{}, Shipping: contractSchedulesClient{}}
	r := httptest.NewRequest(http.MethodGet, "/api/contracts/3/vessels", nil)
	params := chi.NewRouteContext()
	params.URLParams.Add("id", "3")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, params))
	w := httptest.NewRecorder()
	s.listContractVessels(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Schedules []contractShippingSchedule `json:"schedules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	rows := body.Data.Schedules
	if len(rows) != 2 || rows[0].BatchNo != 1 || rows[0].Status != "IN_TRANSIT" || rows[0].CargoItems[0].Quantity != "30" || rows[1].Status != "COMPLETED" || rows[1].CargoItems[0].Quantity != "20" {
		t.Fatalf("batch details mixed or missing: %+v", rows)
	}
	if strings.Contains(w.Body.String(), "9999.99") || strings.Contains(w.Body.String(), "freightAmount") {
		t.Fatal("sales contract view exposed internal freight amounts")
	}
}
