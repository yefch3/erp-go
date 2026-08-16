package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// The whole contract: reachable with no session, answers 200, names its
// version. The deploy script curls this before declaring a swap healthy.
func TestHealthzAnswersWithoutASession(t *testing.T) {
	t.Setenv("ERP_SHA", "abc123")
	s := &Server{}
	req := httptest.NewRequest("GET", "/api/healthz", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("healthz = %d, want 200", rec.Code)
	}
	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("healthz body is not JSON: %v", err)
	}
	if !body.OK || body.Version != "abc123" {
		t.Errorf("healthz = %+v, want ok with the pinned version", body)
	}
}
