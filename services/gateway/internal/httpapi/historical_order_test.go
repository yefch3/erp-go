package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"google.golang.org/grpc"
)

type historicalOrderClientStub struct {
	prv1.PurchaseOrderServiceClient
	got *prv1.SaveHistoricalOrderRequest
}

func (c *historicalOrderClientStub) SaveHistoricalOrder(_ context.Context, in *prv1.SaveHistoricalOrderRequest, _ ...grpc.CallOption) (*prv1.SaveHistoricalOrderResponse, error) {
	c.got = in
	return &prv1.SaveHistoricalOrderResponse{Id: 1, Status: "ORDERED"}, nil
}

func TestHistoricalOrderOptionalSupplier(t *testing.T) {
	for _, tc := range []struct {
		name, body, supplierStatus string
		wantOK                     bool
	}{
		{"omitted", `{"confirm":true}`, "", true},
		{"zero", `{"supplierId":"0","confirm":true}`, "", true},
		{"active", `{"supplierId":"8"}`, "ACTIVE", true},
		{"inactive", `{"supplierId":"8"}`, "INACTIVE", false},
		{"negative", `{"supplierId":"-1"}`, "ACTIVE", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orders := &historicalOrderClientStub{}
			s := &Server{Orders: orders}
			if tc.supplierStatus != "" {
				s.Suppliers = activeSupplierClientStub{status: tc.supplierStatus}
			}
			rec := httptest.NewRecorder()
			s.saveHistoricalOrder(rec, httptest.NewRequest(http.MethodPost, "/api/purchase-orders/historical", strings.NewReader(tc.body)))
			if (rec.Code == http.StatusOK) != tc.wantOK || (orders.got != nil) != tc.wantOK {
				t.Fatalf("status=%d response=%s forwarded=%v", rec.Code, rec.Body.String(), orders.got != nil)
			}
		})
	}
}
