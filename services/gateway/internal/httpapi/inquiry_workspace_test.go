package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestD1RetiredInquiryHasNoDependencies(t *testing.T) {
	// With no mail, sourcing or export clients, retirement must return before
	// attempting any external work, including legacy RFQ email dispatch.
	s := &Server{}
	w := httptest.NewRecorder()
	s.retiredInquiryAction(w, httptest.NewRequest(http.MethodPost, "/api/factory-rfqs/1/send", strings.NewReader(`{}`)))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "INQUIRY_WORKFLOW_REPLACED") {
		t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
	}
}
