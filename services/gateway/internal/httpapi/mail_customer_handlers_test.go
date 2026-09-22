package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http/httptest"
	"strings"
	"testing"
)

type mailCustomerMailStub struct {
	mailv1.EmailServiceClient
	denied bool
	id     int64
}

func (s *mailCustomerMailStub) GetInbound(_ context.Context, r *mailv1.GetInboundRequest, _ ...grpc.CallOption) (*mailv1.GetInboundResponse, error) {
	s.id = r.Id
	if s.denied {
		return nil, status.Error(codes.PermissionDenied, "not your mailbox")
	}
	return &mailv1.GetInboundResponse{}, nil
}

type mailCustomerSaveStub struct {
	mdv1.CustomerServiceClient
	request *mdv1.SaveMailCustomerRequest
	tenant  int64
}

func (s *mailCustomerSaveStub) SaveMailCustomer(ctx context.Context, r *mdv1.SaveMailCustomerRequest, _ ...grpc.CallOption) (*mdv1.SaveMailCustomerResponse, error) {
	s.request = r
	op, _ := grpcx.OperatorFromContext(ctx)
	s.tenant = op.TenantID
	return &mdv1.SaveMailCustomerResponse{}, nil
}
func TestMailCustomerSaveAuthorizesSourceAndOverridesBodyID(t *testing.T) {
	for _, denied := range []bool{true, false} {
		mailbox := &mailCustomerMailStub{denied: denied}
		customers := &mailCustomerSaveStub{}
		server := &Server{Emails: mailbox, Customers: customers}
		r := httptest.NewRequest("POST", "/api/inbound-mails/12/customer-link", strings.NewReader(`{"inboundId":"999","action":"CREATE","companyName":"A","email":"a@example.com"}`))
		route := chi.NewRouteContext()
		route.URLParams.Add("id", "12")
		ctx := context.WithValue(r.Context(), chi.RouteCtxKey, route)
		ctx = grpcx.WithOperator(ctx, grpcx.Operator{TenantID: 22, EmployeeID: 7})
		w := httptest.NewRecorder()
		server.saveMailCustomer(w, r.WithContext(ctx))
		if mailbox.id != 12 {
			t.Fatal("wrong source authorized")
		}
		if denied {
			if customers.request != nil || w.Code != 403 {
				t.Fatalf("unauthorized mail reached masterdata: %d", w.Code)
			}
		} else if customers.request == nil || customers.request.InboundId != 12 || customers.tenant != 22 || w.Code != 200 {
			t.Fatalf("untrusted source or tenant: %+v %d", customers, w.Code)
		}
	}
}
