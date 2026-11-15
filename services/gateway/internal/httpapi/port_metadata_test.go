package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type portCorrectionClient struct {
	shippingv1.ShippingServiceClient
	received       *shippingv1.CorrectPortMetadataRequest
	dispatchCalled bool
}

func (s *portCorrectionClient) GetSchedule(context.Context, *shippingv1.GetScheduleRequest, ...grpc.CallOption) (*shippingv1.GetScheduleResponse, error) {
	return &shippingv1.GetScheduleResponse{Schedule: &shippingv1.Schedule{Id: 9, PortOfDischarge: "洛杉矶港"}}, nil
}
func (s *portCorrectionClient) CorrectPortMetadata(_ context.Context, r *shippingv1.CorrectPortMetadataRequest, _ ...grpc.CallOption) (*shippingv1.CorrectPortMetadataResponse, error) {
	s.received = r
	return &shippingv1.CorrectPortMetadataResponse{}, nil
}
func (s *portCorrectionClient) GetContractDispatchProgress(context.Context, *shippingv1.GetContractDispatchProgressRequest, ...grpc.CallOption) (*shippingv1.GetContractDispatchProgressResponse, error) {
	s.dispatchCalled = true
	return nil, status.Error(codes.Unavailable, "offline")
}
func contractProgressRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "9")
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}
func TestPortCorrectionUsesMasterdataAndRejectsAnotherPhysicalPort(t *testing.T) {
	for _, tc := range []struct {
		port string
		want int
	}{{"2", 200}, {"1", 400}} {
		client := &portCorrectionClient{}
		s := &Server{Shipping: client, Ports: shippingPortClientStub{}}
		w := httptest.NewRecorder()
		s.correctShippingPortMetadata(w, contractProgressRequest("POST", "/api/shipping/schedules/9/port-metadata", `{"terminal":"DESTINATION","portId":"`+tc.port+`","portName":"FORGED","portCode":"FORGED","timezone":"UTC","reason":"repair","routeVersion":1}`))
		if w.Code != tc.want {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if tc.want == 200 {
			if client.received == nil || client.received.Id != 9 || client.received.PortName != "洛杉矶港" || client.received.PortCode != "USLAX" || client.received.Timezone != "America/Los_Angeles" {
				t.Fatalf("not authoritative: %+v", client.received)
			}
		} else if client.received != nil {
			t.Fatal("different physical port was sent for correction")
		}
	}
}

type progressContractClient struct{ exv1.ContractServiceClient }

func (progressContractClient) GetContract(context.Context, *exv1.GetContractRequest, ...grpc.CallOption) (*exv1.GetContractResponse, error) {
	v := progressFixture()
	v.Contract.Id = 9
	return v, nil
}
func TestContractProgressChecksScopeAndDegradesExplicitly(t *testing.T) {
	client := &portCorrectionClient{}
	s := &Server{Contracts: contractVesselVisibilityStub{err: status.Error(codes.PermissionDenied, "invisible")}, Shipping: client}
	w := httptest.NewRecorder()
	s.getContract(w, contractProgressRequest("GET", "/api/contracts/9", ""))
	if w.Code != 403 || client.dispatchCalled {
		t.Fatal("read dispatch facts before scope check")
	}
	s.Contracts = progressContractClient{}
	w = httptest.NewRecorder()
	s.getContract(w, contractProgressRequest("GET", "/api/contracts/9", ""))
	if w.Code != 200 || !client.dispatchCalled || !strings.Contains(w.Body.String(), `"shipmentProgressAvailable":false`) {
		t.Fatalf("missing unavailable signal: %d %s", w.Code, w.Body.String())
	}
}
