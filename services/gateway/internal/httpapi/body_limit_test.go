package httpapi

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestReadBodyLimitChecksFinalChunk(t *testing.T) {
	t.Parallel()

	s := &Server{}
	req := httptest.NewRequest("POST", "/api/stock-imports/preview", bytes.NewReader(make([]byte, 4097)))
	rec := httptest.NewRecorder()

	if _, ok := s.readBodyLimit(rec, req, 4096); ok {
		t.Fatal("readBodyLimit accepted a body over the configured limit")
	}
	if rec.Code != 413 {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}
