package httpapi

import (
	"net/http"
	"strings"

	"github.com/sgao19/erp-go/pkg/grpcx"
)

// Permission answers are remembered for ten seconds by the iam connection
// itself (grpcx.AnswerCache, wired in cmd/main.go): the route gates, the
// checks inside handlers and /api/me/permissions all ask through it, as do
// the other services on their own iam connections.
//
// What only the gateway can add is the other half: it relays every role and
// employee change, so it can forget a company's answers the moment such a
// change succeeds instead of letting them run out their ten seconds. That is
// this file. The other services have no such hook and live with the TTL.

// changesPermissions says whether a successful write to this path can change
// what somebody is allowed to do or see: who is active, who holds which role,
// what a role grants, how departments nest (data scopes follow the tree),
// whether a company is switched on.
//
// Fail-safe by prefix — a new route under these paths clears the cache until
// somebody decides it need not — minus the writes known to leave permissions
// alone. Those matter: avatar-urls is a read sent as POST that anyone with
// iam:employee:read may call, and every call would empty the tenant's cache.
func changesPermissions(path string) bool {
	if strings.HasPrefix(path, "/api/employees") {
		for _, suffix := range nonPermissionEmployeeWrites {
			if strings.HasSuffix(path, suffix) {
				return false
			}
		}
		return true
	}
	return strings.HasPrefix(path, "/api/roles") || strings.HasPrefix(path, "/api/departments") ||
		strings.HasPrefix(path, "/api/platform/tenants")
}

// nonPermissionEmployeeWrites are /api/employees writes that change neither
// who is active nor who holds which role.
var nonPermissionEmployeeWrites = []string{
	"/avatar-urls", "/avatar", "/avatar/presign", "/invite", "/invite-batch",
	"/reset-link", "/password", "/company-mailbox", "/manager", "/revoke-sessions",
}

// forgetPermissionsOnChange clears cached answers the moment a write that can
// change them succeeds — before its response goes out, so the browser that
// made the change cannot outrun it.
func (s *Server) forgetPermissionsOnChange(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Answers == nil || !mutating(r.Method) || !changesPermissions(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		forget := func() {
			if strings.HasPrefix(r.URL.Path, "/api/platform/") {
				s.Answers.ForgetAll()
			} else if op, ok := grpcx.OperatorFromContext(r.Context()); ok {
				s.Answers.ForgetTenant(op.TenantID)
			}
		}
		tap := &successTap{ResponseWriter: w, onSuccess: forget}
		next.ServeHTTP(tap, r)
		if !tap.decided {
			// Returned without writing: net/http answers 200 for it.
			tap.decided = true
			forget()
		}
	})
}

// successTap runs onSuccess once, when the response turns out to be a
// success, just before the status line is written.
type successTap struct {
	http.ResponseWriter
	onSuccess func()
	decided   bool
}

func (t *successTap) WriteHeader(code int) {
	if !t.decided {
		t.decided = true
		if code < http.StatusBadRequest {
			t.onSuccess()
		}
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *successTap) Write(b []byte) (int, error) {
	if !t.decided {
		t.WriteHeader(http.StatusOK)
	}
	return t.ResponseWriter.Write(b)
}
