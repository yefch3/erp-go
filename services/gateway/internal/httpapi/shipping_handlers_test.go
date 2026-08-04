package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

type shippingClientStub struct {
	shippingv1.ShippingServiceClient
}

func (shippingClientStub) GetModuleStatus(context.Context, *shippingv1.GetModuleStatusRequest, ...grpc.CallOption) (*shippingv1.GetModuleStatusResponse, error) {
	return &shippingv1.GetModuleStatusResponse{Module: "shipping", Status: "READY", SchemaVersion: 1}, nil
}

func (shippingClientStub) ListSchedules(context.Context, *shippingv1.ListSchedulesRequest, ...grpc.CallOption) (*shippingv1.ListSchedulesResponse, error) {
	return &shippingv1.ListSchedulesResponse{
		Schedules: []*shippingv1.Schedule{{Id: 9, ScheduleNo: "SCH-TEST-0001"}},
		Meta:      &commonv1.PageMeta{Total: 1},
	}, nil
}

func (shippingClientStub) CreateSchedule(context.Context, *shippingv1.CreateScheduleRequest, ...grpc.CallOption) (*shippingv1.CreateScheduleResponse, error) {
	return &shippingv1.CreateScheduleResponse{Schedule: &shippingv1.Schedule{Id: 9, ScheduleNo: "SCH-TEST-0001"}}, nil
}

func TestGetShippingStatus(t *testing.T) {
	s := &Server{Shipping: shippingClientStub{}}
	recorder := httptest.NewRecorder()
	s.getShippingStatus(recorder, httptest.NewRequest(http.MethodGet, "/api/shipping/status", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"module":"shipping"`) || !strings.Contains(body, `"status":"READY"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestListShippingSchedules(t *testing.T) {
	s := &Server{Shipping: shippingClientStub{}}
	recorder := httptest.NewRecorder()
	s.listShippingSchedules(recorder, httptest.NewRequest(http.MethodGet, "/api/shipping/schedules?page=1", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"scheduleNo":"SCH-TEST-0001"`) {
		t.Fatalf("unexpected list response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateShippingSchedule(t *testing.T) {
	s := &Server{Shipping: shippingClientStub{}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/shipping/schedules", strings.NewReader(`{"schedule":{"vesselName":"Test"}}`))
	s.createShippingSchedule(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"scheduleNo":"SCH-TEST-0001"`) {
		t.Fatalf("unexpected create response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestShippingRouteRequiresAuthentication(t *testing.T) {
	s := &Server{JWTSecret: "test-secret"}
	recorder := httptest.NewRecorder()
	s.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/shipping/status", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for registered protected route", recorder.Code)
	}
}

func TestShippingListRouteRequiresAuthentication(t *testing.T) {
	s := &Server{JWTSecret: "test-secret"}
	recorder := httptest.NewRecorder()
	s.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/shipping/schedules", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}
