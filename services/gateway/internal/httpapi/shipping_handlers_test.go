package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

type shippingClientStub struct {
	shippingv1.ShippingServiceClient
}

type shippingPortClientStub struct{ mdv1.PortServiceClient }

func (shippingPortClientStub) GetPort(_ context.Context, req *mdv1.GetPortRequest, _ ...grpc.CallOption) (*mdv1.GetPortResponse, error) {
	if req.GetId() == 1 {
		return &mdv1.GetPortResponse{Port: &mdv1.Port{Id: 1, Unlocode: "CNSHA", NameZh: "上海港", NameEn: "Shanghai", Timezone: "Asia/Shanghai", Status: "ACTIVE"}}, nil
	}
	return &mdv1.GetPortResponse{Port: &mdv1.Port{Id: 2, Unlocode: "USLAX", NameZh: "洛杉矶港", NameEn: "Los Angeles", Timezone: "America/Los_Angeles", Status: "ACTIVE"}}, nil
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

func (shippingClientStub) ListArrivalNotifications(context.Context, *shippingv1.ListArrivalNotificationsRequest, ...grpc.CallOption) (*shippingv1.ListArrivalNotificationsResponse, error) {
	return &shippingv1.ListArrivalNotificationsResponse{Reminders: []*shippingv1.ArrivalReminder{{Id: 7, Title: "船期将在 7 天内到港"}}, UnreadCount: 1}, nil
}

func (shippingClientStub) CleanupExpiredArrivalReminders(context.Context, *shippingv1.CleanupExpiredArrivalRemindersRequest, ...grpc.CallOption) (*shippingv1.CleanupExpiredArrivalRemindersResponse, error) {
	return &shippingv1.CleanupExpiredArrivalRemindersResponse{DeletedCount: 2}, nil
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
	s := &Server{Shipping: shippingClientStub{}, Ports: shippingPortClientStub{}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/shipping/schedules", strings.NewReader(`{"schedule":{"vesselName":"Test","loadingPortId":"1","dischargePortId":"2"}}`))
	s.createShippingSchedule(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"scheduleNo":"SCH-TEST-0001"`) {
		t.Fatalf("unexpected create response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestListShippingArrivalNotifications(t *testing.T) {
	s := &Server{Shipping: shippingClientStub{}}
	recorder := httptest.NewRecorder()
	s.listShippingArrivalNotifications(recorder, httptest.NewRequest(http.MethodGet, "/api/shipping/reminders", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"title":"船期将在 7 天内到港"`) || !strings.Contains(recorder.Body.String(), `"unreadCount":"1"`) {
		t.Fatalf("unexpected reminder response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCleanupExpiredShippingArrivalReminders(t *testing.T) {
	s := &Server{Shipping: shippingClientStub{}}
	recorder := httptest.NewRecorder()
	s.cleanupExpiredShippingArrivalReminders(recorder, httptest.NewRequest(http.MethodDelete, "/api/shipping/reminders/expired", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"deletedCount":"2"`) {
		t.Fatalf("unexpected cleanup response: status=%d body=%s", recorder.Code, recorder.Body.String())
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

func TestShippingDocumentRoutesRequireAuthentication(t *testing.T) {
	s := &Server{JWTSecret: "test-secret"}
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/api/shipping/schedules/1/documents"},
		{http.MethodPost, "/api/shipping/schedules/1/documents/2/download"},
		{http.MethodGet, "/api/shipping/reminders"},
		{http.MethodDelete, "/api/shipping/reminders/expired"},
		{http.MethodPost, "/api/shipping/reminders/7/read"},
	} {
		recorder := httptest.NewRecorder()
		s.Router().ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.target, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d, want 401", tc.target, recorder.Code)
		}
	}
}
