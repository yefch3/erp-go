package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
)

type shippingEditSnapshotClient struct {
	shippingv1.ShippingServiceClient
	received *shippingv1.UpdateScheduleRequest
}

func (s *shippingEditSnapshotClient) GetSchedule(context.Context, *shippingv1.GetScheduleRequest, ...grpc.CallOption) (*shippingv1.GetScheduleResponse, error) {
	return &shippingv1.GetScheduleResponse{Schedule: &shippingv1.Schedule{
		Id: 9, CustomerId: 7, CustomerName: "原客户", CarrierId: 8, CarrierForwarder: "原货代",
		LoadingPortId: 1, PortOfLoading: "上海", LoadingPortTimezone: "Asia/Shanghai",
		DischargePortId: 2, PortOfDischarge: "洛杉矶", DischargePortTimezone: "America/Los_Angeles",
	}}, nil
}

func (s *shippingEditSnapshotClient) UpdateSchedule(_ context.Context, req *shippingv1.UpdateScheduleRequest, _ ...grpc.CallOption) (*shippingv1.UpdateScheduleResponse, error) {
	s.received = req
	return &shippingv1.UpdateScheduleResponse{Schedule: &shippingv1.Schedule{Id: req.Id}}, nil
}

func TestShippingEditRetainsAuthorizedScheduleSnapshots(t *testing.T) {
	client := &shippingEditSnapshotClient{}
	s := &Server{Shipping: client}
	r := httptest.NewRequest(http.MethodPut, "/api/shipping/schedules/9", strings.NewReader(`{"schedule":{"customerId":"7","customerName":"伪造客户名","carrierId":"8","carrierForwarder":"伪造货代名","loadingPortId":"1","dischargePortId":"2","bookingNo":"BOOK-9"}}`))
	params := chi.NewRouteContext()
	params.URLParams.Add("id", "9")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, params))
	w := httptest.NewRecorder()
	s.updateShippingSchedule(w, r)
	if w.Code != http.StatusOK || client.received == nil {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	in := client.received.Schedule
	if in.CustomerName != "原客户" || in.CarrierForwarder != "原货代" || in.PortOfLoading != "上海" || in.DischargePortTimezone != "America/Los_Angeles" || in.BookingNo != "BOOK-9" {
		t.Fatalf("snapshot was lost or caller text trusted: %+v", in)
	}
}
