package httpapi

import (
	"context"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"google.golang.org/grpc"
	"testing"
)

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
