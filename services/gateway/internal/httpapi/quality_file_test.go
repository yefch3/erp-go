package httpapi

import (
	"context"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"google.golang.org/grpc"
)

type qualityFileOrderClient struct {
	prv1.PurchaseOrderServiceClient
	request *prv1.ReadQualityInspectionFileRequest
}

func (c *qualityFileOrderClient) ReadQualityInspectionFile(_ context.Context, req *prv1.ReadQualityInspectionFileRequest, _ ...grpc.CallOption) (*prv1.ReadQualityInspectionFileResponse, error) {
	c.request = req
	return &prv1.ReadQualityInspectionFileResponse{FileName: "质检报告.txt", ContentType: "text/plain", Content: []byte("report")}, nil
}

func TestQualityFileDownloadPaths(t *testing.T) {
	orders := &qualityFileOrderClient{}
	s := &Server{Orders: orders}
	r := chi.NewRouter()
	r.Get("/api/quality/tasks/{id}/files/{fileID}", s.readQualityTaskFile)
	r.Get("/api/purchase-orders/{id}/quality-inspections/{taskID}/files/{fileID}", s.readOrderQualityFile)
	for _, tc := range []struct {
		path string
		poID int64
	}{
		{"/api/quality/tasks/42/files/17", 0},
		{"/api/purchase-orders/53/quality-inspections/42/files/17", 53},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if req := orders.request; req.GetId() != 42 || req.GetFileId() != 17 || req.GetPoId() != tc.poID {
			t.Fatalf("%s passed wrong identifiers: %v", tc.path, req)
		}
		if w.Code != http.StatusOK || w.Body.String() != "report" || w.Header().Get("Content-Type") != "text/plain" {
			t.Fatalf("%s: status %d, response %q", tc.path, w.Code, w.Body.String())
		}
		kind, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
		if err != nil || kind != "attachment" || params["filename"] != "质检报告.txt" {
			t.Fatalf("%s has invalid download headers: %v", tc.path, w.Header())
		}
	}
	orders.request = nil
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/purchase-orders/-1/quality-inspections/42/files/17", nil))
	if w.Code != http.StatusNotFound || orders.request != nil {
		t.Fatalf("invalid order must never fall through to task lookup: %d, %v", w.Code, orders.request)
	}
}
