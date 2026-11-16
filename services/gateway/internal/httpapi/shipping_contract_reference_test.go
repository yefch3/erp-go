package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
)

type contractUploadReferenceShipping struct {
	shippingv1.ShippingServiceClient
	handoff *shippingv1.ContractShippingHandoff
	request *shippingv1.SaveContractShippingContractRequest
}

func (s *contractUploadReferenceShipping) SaveContractShippingContract(_ context.Context, r *shippingv1.SaveContractShippingContractRequest, _ ...grpc.CallOption) (*shippingv1.SaveContractShippingContractResponse, error) {
	s.request = r
	return &shippingv1.SaveContractShippingContractResponse{Handoff: s.handoff}, nil
}

type contractUploadReferenceOrders struct {
	prv1.PurchaseOrderServiceClient
	request *prv1.CreateExternalPayableRequest
}

func (s *contractUploadReferenceOrders) CreateExternalPayable(_ context.Context, r *prv1.CreateExternalPayableRequest, _ ...grpc.CallOption) (*prv1.CreateExternalPayableResponse, error) {
	s.request = r
	return &prv1.CreateExternalPayableResponse{}, nil
}

func TestShippingContractUploadUsesSalesContractAndBatchForFinance(t *testing.T) {
	for _, batch := range []int32{1, 2} {
		ship := &contractUploadReferenceShipping{handoff: &shippingv1.ContractShippingHandoff{ContractNo: "CT-TEST", BatchNo: batch, ForwarderContractNo: "LC-202611-000001", ForwarderContractNoGenerated: true, SignedContractKey: "signed-file", SignedContractName: "signed.pdf"}}
		orders := &contractUploadReferenceOrders{}
		s := &Server{Shipping: ship, Orders: orders}
		req := httptest.NewRequest(http.MethodPost, "/api/shipping/contract-handoffs/99/contract", strings.NewReader(`{"file_key":"signed-file","file_name":"signed.pdf"}`))
		route := chi.NewRouteContext()
		route.URLParams.Add("id", "99")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
		w := httptest.NewRecorder()
		s.saveContractShippingContract(w, req)
		if w.Code != http.StatusOK || ship.request.GetContractNo() != "" || orders.request == nil {
			t.Fatalf("file-only upload failed: status=%d body=%s", w.Code, w.Body.String())
		}
		if orders.request.GetBusinessDocumentNo() != "LC-202611-000001" || !orders.request.GetBusinessDocumentNoGenerated() || orders.request.GetTransportBatchNo() != batch || orders.request.GetSourceBusinessId() != 99 || orders.request.GetExportContractNo() != "CT-TEST" || orders.request.GetSignedContractKey() != "signed-file" {
			t.Fatalf("incorrect finance reference: %+v", orders.request)
		}
	}
}
