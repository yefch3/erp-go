package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http/httptest"
	"testing"
)

type documentFileClient struct {
	mdv1.CustomerServiceClient
	kind   string
	denied bool
}

func (c documentFileClient) GetCustomerDocumentFile(ctx context.Context, r *mdv1.GetCustomerDocumentFileRequest, opts ...grpc.CallOption) (*mdv1.GetCustomerDocumentFileResponse, error) {
	if c.denied {
		return nil, status.Error(codes.NotFound, "not accessible")
	}
	return &mdv1.GetCustomerDocumentFileResponse{FileName: "certificate.pdf", ContentType: c.kind, Content: []byte("sample")}, nil
}
func TestCustomerDocumentDownloadProtection(t *testing.T) {
	for _, tc := range []struct {
		kind        string
		denied      bool
		status      int
		disposition string
	}{{"application/pdf", false, 200, "inline"}, {"text/html", false, 200, "attachment"}, {"application/pdf", true, 404, ""}} {
		s := &Server{Customers: documentFileClient{kind: tc.kind, denied: tc.denied}}
		router := chi.NewRouter()
		router.Get("/customers/{id}/documents/{revisionId}/file", s.getCustomerDocumentFile)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/customers/1/documents/2/file?preview=1", nil))
		if w.Code != tc.status {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if !tc.denied {
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("file could be cached after access revocation")
			}
			if got := w.Header().Get("Content-Disposition"); len(got) < len(tc.disposition) || got[:len(tc.disposition)] != tc.disposition {
				t.Fatal("unsafe preview disposition", got)
			}
		}
	}
}
