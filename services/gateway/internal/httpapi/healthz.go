package httpapi

import (
	"net/http"
	"os"
)

// healthz answers the deploy script and, later, the monitor. 200 means "the
// gateway process is up and serving HTTP" — nothing more. It deliberately
// does not probe downstream services: a deploy that swapped the containers
// successfully must read as healthy even while iam is still warming up, and
// a monitoring story that aggregates service health belongs to the services
// themselves, not to a login-free endpoint.
//
// The version is the git SHA the running deployment was pinned to (ERP_SHA
// comes from the production compose overlay), so a deploy can confirm the
// swap actually happened rather than inferring it from silence.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	version := os.Getenv("ERP_SHA")
	if version == "" {
		version = "dev"
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"version":"` + version + `"}`))
}
