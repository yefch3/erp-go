package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"google.golang.org/grpc"
)

type entryDefaultsClient struct {
	exv1.ContractServiceClient
	action string
}

func (c *entryDefaultsClient) ContractWorkflow(_ context.Context, req *exv1.ContractWorkflowRequest, _ ...grpc.CallOption) (*exv1.ContractWorkflowResponse, error) {
	c.action = req.Action
	return &exv1.ContractWorkflowResponse{DataJson: `{"sellerName":"","sellerAddress":""}`}, nil
}

func TestContractEntryDefaultsUsesReadOnlyWorkflowAction(t *testing.T) {
	client := &entryDefaultsClient{}
	s := &Server{Contracts: client}
	w := httptest.NewRecorder()
	s.contractWorkflow(w, httptest.NewRequest(http.MethodGet, "/api/contracts/entry-defaults", nil))
	if w.Code != http.StatusOK || client.action != "entry_defaults" || !strings.Contains(w.Body.String(), `"sellerName":""`) {
		t.Fatalf("status=%d action=%q body=%s", w.Code, client.action, w.Body.String())
	}
}
