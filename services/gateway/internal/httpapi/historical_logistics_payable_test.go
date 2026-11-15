package httpapi

import (
	"context"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type historyPayableShipping struct {
	shippingv1.ShippingServiceClient
	source *shippingv1.ContractShippingHandoff
}

func (c historyPayableShipping) GetContractShippingHandoff(context.Context, *shippingv1.GetContractShippingHandoffRequest, ...grpc.CallOption) (*shippingv1.GetContractShippingHandoffResponse, error) {
	return &shippingv1.GetContractShippingHandoffResponse{Handoff: c.source}, nil
}
func (c historyPayableShipping) ListContractShippingHandoffs(context.Context, *shippingv1.ListContractShippingHandoffsRequest, ...grpc.CallOption) (*shippingv1.ListContractShippingHandoffsResponse, error) {
	return &shippingv1.ListContractShippingHandoffsResponse{Handoffs: []*shippingv1.ContractShippingHandoff{c.source, {Id: 2, Status: "SCHEDULED", ScheduleId: 5}}}, nil
}

type historyPayableOrders struct {
	prv1.PurchaseOrderServiceClient
	req *prv1.CreateManualPayableRequest
}

func (c *historyPayableOrders) CreateManualPayable(_ context.Context, r *prv1.CreateManualPayableRequest, _ ...grpc.CallOption) (*prv1.CreateManualPayableResponse, error) {
	c.req = r
	return &prv1.CreateManualPayableResponse{}, nil
}
func (c *historyPayableOrders) ListSupplierRecon(context.Context, *prv1.ListSupplierReconRequest, ...grpc.CallOption) (*prv1.ListSupplierReconResponse, error) {
	return &prv1.ListSupplierReconResponse{}, nil
}
func TestHistoricalLogisticsPayableUsesTrustedSnapshot(t *testing.T) {
	orders := &historyPayableOrders{}
	source := &shippingv1.ContractShippingHandoff{Id: 99, ManualOrderNo: "LO-HISTORY", Status: "SCHEDULED", ScheduleId: 12, FinalForwarderId: 8, FinalForwarderName: "Real forwarder", FinalCurrency: "USD", FinalFreightAmount: "125.50", ContractNo: "REF", PaymentTerms: "Net30"}
	server := &Server{Orders: orders, Shipping: historyPayableShipping{source: source}}
	recorder := httptest.NewRecorder()
	server.createManualPayable(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"businessType":"LOGISTICS","sourceBusinessId":"99","supplierName":"Forged","sourceSupplierId":"777","orderNo":"OTHER","currency":"CNY","totalAmount":"1","paidAmount":"25","exportContractNo":"FORGED"}`)))
	r := orders.req
	if recorder.Code != 200 || r == nil || r.SupplierName != "Real forwarder" || r.SourceSupplierId != 8 || r.OrderNo != "LO-HISTORY" || r.Currency != "USD" || r.TotalAmount != "125.50" || r.ExportContractNo != "REF" || r.PaymentTerms != "Net30" || r.PaidAmount != "25" {
		t.Fatalf("status=%d req=%+v body=%s", recorder.Code, r, recorder.Body.String())
	}
	source.ManualOrderNo = ""
	orders.req = nil
	recorder = httptest.NewRecorder()
	server.createManualPayable(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"businessType":"LOGISTICS","sourceBusinessId":"99"}`)))
	if recorder.Code != 400 || orders.req != nil {
		t.Fatal("normal contract used as historical source")
	}
}
func TestHistoricalLogisticsOptionsAreFinanceProjection(t *testing.T) {
	server := &Server{Orders: &historyPayableOrders{}, Shipping: historyPayableShipping{source: &shippingv1.ContractShippingHandoff{Id: 99, ManualOrderNo: "LO-HISTORY", ScheduleId: 12, Status: "SCHEDULED", FinalCurrency: "USD", SignedContractKey: "PRIVATE-KEY"}}}
	recorder := httptest.NewRecorder()
	server.listHistoricalLogisticsPayableOptions(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	body := recorder.Body.String()
	if recorder.Code != 200 || !strings.Contains(body, "LO-HISTORY") || strings.Contains(body, "PRIVATE-KEY") || strings.Contains(body, `"id":"2"`) {
		t.Fatal(body)
	}
}
