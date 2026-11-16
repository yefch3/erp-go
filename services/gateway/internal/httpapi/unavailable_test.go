package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnavailableServiceReturnsRetryableError(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).writeGRPCError(w, status.Error(codes.Unavailable, "name resolver error: produced zero addresses"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Success || body.Code != "SERVICE_UNAVAILABLE" || !strings.Contains(body.Message, "重试") || strings.Contains(body.Message, "resolver") {
		t.Fatalf("unexpected unavailable response: %s", w.Body.String())
	}
}
