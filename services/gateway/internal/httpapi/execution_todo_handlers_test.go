package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type executionContractsStub struct{ exv1.ContractServiceClient }

type executionReceiptStub struct{ exv1.ReceiptServiceClient }

func (executionReceiptStub) ListReceivableDue(_ context.Context, r *exv1.ListReceivableDueRequest, _ ...grpc.CallOption) (*exv1.ListReceivableDueResponse, error) {
	if r.GetClosedOnly() {
		return &exv1.ListReceivableDueResponse{Items: []*exv1.ReceivableDue{{ContractId: 4, ContractNo: "RECONCILED-STILL-NEEDS-RELEASE", ExecutionConditionStatus: "WAITING", OpenAmount: "0"}}, Meta: &commonv1.PageMeta{Total: 1}}, nil
	}
	if r.GetPage().GetPage() == 1 {
		return &exv1.ListReceivableDueResponse{Items: []*exv1.ReceivableDue{{ContractId: 1, ExecutionConditionStatus: "READY"}}, Meta: &commonv1.PageMeta{Total: 201}}, nil
	}
	return &exv1.ListReceivableDueResponse{Items: []*exv1.ReceivableDue{{ContractId: 2, ContractNo: "FINANCE-PENDING", ExecutionConditionStatus: "WAITING"}, {ContractId: 3, ExecutionConditionStatus: "NOT_APPLICABLE"}}, Meta: &commonv1.PageMeta{Total: 201}}, nil
}
func (executionContractsStub) CheckContractExecution(_ context.Context, r *exv1.CheckContractExecutionRequest, _ ...grpc.CallOption) (*exv1.CheckContractExecutionResponse, error) {
	if r.Id == 12 {
		return nil, status.Error(codes.NotFound, "contract missing")
	}
	return &exv1.CheckContractExecutionResponse{Allowed: r.Id == 10}, nil
}

type executionRequirementsStub struct{ prv1.RequirementServiceClient }

func (executionRequirementsStub) ListRequirements(context.Context, *prv1.ListRequirementsRequest, ...grpc.CallOption) (*prv1.ListRequirementsResponse, error) {
	return &prv1.ListRequirementsResponse{Requirements: []*prv1.Requirement{{Id: 1, ContractId: 10, Status: "WAITING_REQUOTE"}, {Id: 2, ContractId: 11, Status: "WAITING_REQUOTE"}, {Id: 3, ContractId: 10, OwnerId: 99, Status: "WAITING_REQUOTE"}, {Id: 4, ContractId: 12, Status: "WAITING_REQUOTE"}}, Meta: &commonv1.PageMeta{Total: 4}}, nil
}

type executionShippingStub struct {
	shippingv1.ShippingServiceClient
}

func (executionShippingStub) ListContractShippingHandoffs(context.Context, *shippingv1.ListContractShippingHandoffsRequest, ...grpc.CallOption) (*shippingv1.ListContractShippingHandoffsResponse, error) {
	return &shippingv1.ListContractShippingHandoffsResponse{Handoffs: []*shippingv1.ContractShippingHandoff{{Id: 1, ContractId: 10, Status: "WAITING_REQUOTE"}, {Id: 2, ContractId: 11, Status: "WAITING_REQUOTE"}}}, nil
}
func TestExecutionTodosUseActionPermissionsAndLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		permissions                    map[string]bool
		finance, procurement, shipping int
	}{
		{"read access is not an assignment", map[string]bool{"export:receipt:read": true, "procurement:requirement:read": true, "shipping:schedule:read": true}, 0, 0, 0},
		{"finance sees signed unreleased contracts across pages", map[string]bool{"export:receipt:write": true, "shipping:schedule:read": true}, 2, 0, 0},
		{"buyer excludes closed contracts and preserves sales-owned requirements", map[string]bool{"procurement:order:write": true}, 0, 2, 0},
		{"logistics excludes non executable contracts", map[string]bool{"shipping:schedule:write": true}, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Access: homeAccessStub{allowed: tc.permissions}, Contracts: executionContractsStub{}, Receipts: executionReceiptStub{}, Requirements: executionRequirementsStub{}, Shipping: executionShippingStub{}}
			req := httptest.NewRequest("GET", "/api/home/execution-todos", nil)
			req = req.WithContext(grpcx.WithOperator(req.Context(), grpcx.Operator{TenantID: 1, EmployeeID: 9}))
			rec := httptest.NewRecorder()
			s.listExecutionTodos(rec, req)
			if rec.Code != 200 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var response struct {
				Data map[string][]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]int{"finance": tc.finance, "procurement": tc.procurement, "shipping": tc.shipping} {
				if len(response.Data[key]) != want {
					t.Errorf("%s count=%d want=%d body=%s", key, len(response.Data[key]), want, rec.Body.String())
				}
			}
		})
	}
}
