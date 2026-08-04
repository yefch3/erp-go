package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"

	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

type shippingClientStub struct{}

func (shippingClientStub) GetModuleStatus(context.Context, *shippingv1.GetModuleStatusRequest, ...grpc.CallOption) (*shippingv1.GetModuleStatusResponse, error) {
	return &shippingv1.GetModuleStatusResponse{Module: "shipping", Status: "READY", SchemaVersion: 1}, nil
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

func TestShippingRouteRequiresAuthentication(t *testing.T) {
	s := &Server{JWTSecret: "test-secret"}
	recorder := httptest.NewRecorder()
	s.Router().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/shipping/status", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for registered protected route", recorder.Code)
	}
}
