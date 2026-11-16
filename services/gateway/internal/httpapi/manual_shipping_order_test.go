package httpapi

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type manualShippingBatchClient struct {
	shippingv1.ShippingServiceClient
	rows  []*shippingv1.ContractShippingHandoff
	saved int
}

func (c *manualShippingBatchClient) ListContractShippingHandoffs(context.Context, *shippingv1.ListContractShippingHandoffsRequest, ...grpc.CallOption) (*shippingv1.ListContractShippingHandoffsResponse, error) {
	return &shippingv1.ListContractShippingHandoffsResponse{Handoffs: c.rows}, nil
}
func (c *manualShippingBatchClient) SaveManualShippingOrder(_ context.Context, r *shippingv1.SaveManualShippingOrderRequest, _ ...grpc.CallOption) (*shippingv1.SaveManualShippingOrderResponse, error) {
	if r.Order.LinkedContractId != 0 || r.Order.CustomerName != "Manual customer" || r.Order.PortOfLoading != "Manual port" {
		panic("historical input was overwritten")
	}
	c.saved++
	return &shippingv1.SaveManualShippingOrderResponse{Order: r.Order}, nil
}

func TestHistoricalOrderReferenceIsOptionalAndNeverAutoLinked(t *testing.T) {
	for _, number := range []string{"", "CT-11", "NOT-IN-SYSTEM"} {
		t.Run(number, func(t *testing.T) {
			shipping := &manualShippingBatchClient{}
			refs := &shippingReferencesClient{rows: []*exv1.ShippingContractReference{{Id: 11, ContractNo: "CT-11"}}}
			server := &Server{Contracts: refs, Shipping: shipping}
			req := httptest.NewRequest(http.MethodPost, "/api/shipping/manual-orders", strings.NewReader(`{"order":{"contractNo":"`+number+`","customerName":"Manual customer","portOfLoading":"Manual port","finalCurrency":"USD"}}`))
			recorder := httptest.NewRecorder()
			server.saveManualShippingOrder(recorder, req)
			if recorder.Code != http.StatusOK || shipping.saved != 1 || refs.req != nil {
				t.Fatalf("status=%d body=%s reference queried=%v", recorder.Code, recorder.Body.String(), refs.req != nil)
			}
		})
	}
}

type shippingReferencesClient struct {
	exv1.ContractServiceClient
	rows []*exv1.ShippingContractReference
	req  *exv1.ListShippingContractReferencesRequest
}

func (c *shippingReferencesClient) ListShippingContractReferences(ctx context.Context, r *exv1.ListShippingContractReferencesRequest, opts ...grpc.CallOption) (*exv1.ListShippingContractReferencesResponse, error) {
	c.req = r
	return &exv1.ListShippingContractReferencesResponse{Contracts: c.rows}, nil
}
func TestResolveShippingContract(t *testing.T) {
	c := &shippingReferencesClient{}
	s := &Server{Contracts: c}
	ctx := context.Background()
	if _, err := s.resolveShippingContract(ctx, 0, "", false); err == nil {
		t.Fatal("empty number accepted")
	}
	if r, err := s.resolveShippingContract(ctx, 0, "OLD", false); err != nil || r != nil {
		t.Fatal("missing should remain pending")
	}
	c.rows = []*exv1.ShippingContractReference{{Id: 11, ContractNo: "CT-11", ExternalContractNo: "OLD"}}
	r, err := s.resolveShippingContract(ctx, 0, " OLD ", false)
	if err != nil || r.ContractNo != "CT-11" || c.req.Keyword != "OLD" || !c.req.Exact {
		t.Fatal("canonical match", r, err)
	}
	c.rows = append(c.rows, &exv1.ShippingContractReference{Id: 12, ContractNo: "CT-12", ExternalContractNo: "OLD"})
	if r, err = s.resolveShippingContract(ctx, 0, "OLD", false); err != nil || r != nil {
		t.Fatal("ambiguous auto link")
	}
	if _, err = s.resolveShippingContract(ctx, 0, "OLD", true); err == nil {
		t.Fatal("ambiguous explicit match accepted")
	}
	c.rows = nil
	if _, err = s.resolveShippingContract(ctx, 999, "CT-OTHER-TENANT", false); err == nil {
		t.Fatal("missing selected ID accepted")
	}
}
