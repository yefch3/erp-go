package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// countingAccess is iam: it answers from a table and counts the questions.
type countingAccess struct {
	iamv1.AccessServiceClient
	allowed map[string]bool
	fail    error
	calls   int
}

func (a *countingAccess) CheckPermission(_ context.Context, req *iamv1.CheckPermissionRequest, _ ...grpc.CallOption) (*iamv1.CheckPermissionResponse, error) {
	a.calls++
	if a.fail != nil {
		return nil, a.fail
	}
	return &iamv1.CheckPermissionResponse{Allowed: a.allowed[req.GetPermissionCode()]}, nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func cachedServer(access *countingAccess) (*Server, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	perms := NewPermissionCache(PermissionCacheTTL)
	perms.now = clock.now
	return &Server{Access: access, Perms: perms}, clock
}

func as(tenant, employee int64) context.Context {
	return grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: tenant, EmployeeID: employee})
}

func TestPermissionAnswersAreReusedUntilTheyExpire(t *testing.T) {
	access := &countingAccess{allowed: map[string]bool{"sales:inquiry:read": true}}
	s, clock := cachedServer(access)
	for i := 0; i < 5; i++ {
		if ok, err := s.allowed(as(4, 11), "sales:inquiry:read"); err != nil || !ok {
			t.Fatalf("allowed: %v %v", ok, err)
		}
		if ok, _ := s.allowed(as(4, 11), "iam:role:write"); ok {
			t.Fatal("a denial turned into a grant")
		}
	}
	if access.calls != 2 {
		t.Fatalf("iam asked %d times for two distinct questions", access.calls)
	}
	// Another person, or the same person in another company, is asked afresh.
	_, _ = s.allowed(as(4, 12), "sales:inquiry:read")
	_, _ = s.allowed(as(5, 11), "sales:inquiry:read")
	if access.calls != 4 {
		t.Fatalf("answers leaked across people or companies: %d calls", access.calls)
	}
	clock.t = clock.t.Add(PermissionCacheTTL)
	access.allowed["sales:inquiry:read"] = false
	if ok, _ := s.allowed(as(4, 11), "sales:inquiry:read"); ok {
		t.Fatal("a revoked permission outlived the TTL")
	}
}

func TestPermissionErrorsAreNotCached(t *testing.T) {
	access := &countingAccess{fail: errors.New("iam down")}
	s, _ := cachedServer(access)
	if _, err := s.allowed(as(4, 11), "sales:inquiry:read"); err == nil {
		t.Fatal("iam failure swallowed")
	}
	access.fail, access.allowed = nil, map[string]bool{"sales:inquiry:read": true}
	if ok, err := s.allowed(as(4, 11), "sales:inquiry:read"); err != nil || !ok {
		t.Fatalf("an error was remembered as an answer: %v %v", ok, err)
	}
}

func TestForgettingAPermissionWinsOverAnAnswerInFlight(t *testing.T) {
	perms := NewPermissionCache(PermissionCacheTTL)
	k := permissionKey{tenant: 4, employee: 11, code: "iam:role:write"}
	_, _, ticket := perms.lookup(k)
	perms.forgetTenant(4) // the role was edited while iam was answering
	perms.store(k, true, ticket)
	if _, ok, _ := perms.lookup(k); ok {
		t.Fatal("an answer from before the change was stored after it")
	}
	_, _, ticket = perms.lookup(k)
	perms.forgetAll()
	perms.store(k, true, ticket)
	if _, ok, _ := perms.lookup(k); ok {
		t.Fatal("an answer from before a platform change was stored after it")
	}
	// Another company's change does not invalidate this one's answers.
	_, _, ticket = perms.lookup(k)
	perms.forgetTenant(5)
	perms.store(k, true, ticket)
	if allowed, ok, _ := perms.lookup(k); !ok || !allowed {
		t.Fatal("an unrelated company's change dropped this answer")
	}
}

func TestPermissionCacheStaysBounded(t *testing.T) {
	perms := NewPermissionCache(PermissionCacheTTL)
	perms.max = 3
	for e := int64(1); e <= 10; e++ {
		k := permissionKey{tenant: 4, employee: e, code: "x"}
		_, _, ticket := perms.lookup(k)
		perms.store(k, true, ticket)
		if len(perms.entries) > perms.max {
			t.Fatalf("cache grew to %d entries", len(perms.entries))
		}
	}
}

func TestRoleAndEmployeeWritesClearTheirCompany(t *testing.T) {
	cases := []struct {
		name, method, path string
		status             int
		clearsOwn, clears5 bool
	}{
		{"role permissions edited", http.MethodPut, "/api/roles/7/permissions", http.StatusOK, true, false},
		{"roles assigned", http.MethodPost, "/api/employees/11/roles", http.StatusOK, true, false},
		{"employee deactivated", http.MethodDelete, "/api/employees/11", http.StatusOK, true, false},
		{"company switched off", http.MethodPost, "/api/platform/tenants/5/status", http.StatusOK, true, true},
		{"refused write", http.MethodPut, "/api/roles/7/permissions", http.StatusForbidden, false, false},
		{"read", http.MethodGet, "/api/roles", http.StatusOK, false, false},
		{"unrelated write", http.MethodPost, "/api/customers", http.StatusOK, false, false},
		{"avatar urls, a read sent as POST", http.MethodPost, "/api/employees/avatar-urls", http.StatusOK, false, false},
		{"password reset by an admin", http.MethodPost, "/api/employees/11/password", http.StatusOK, false, false},
		{"employee import", http.MethodPost, "/api/employees/import", http.StatusOK, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Perms: NewPermissionCache(PermissionCacheTTL)}
			own := permissionKey{tenant: 4, employee: 11, code: "iam:role:write"}
			other := permissionKey{tenant: 5, employee: 11, code: "iam:role:write"}
			for _, k := range []permissionKey{own, other} {
				_, _, ticket := s.Perms.lookup(k)
				s.Perms.store(k, true, ticket)
			}
			var clearedBeforeResponse bool
			h := s.forgetPermissionsOnChange(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, cached, _ := s.Perms.lookup(own)
				clearedBeforeResponse = !cached
			}))
			req := httptest.NewRequest(tc.method, tc.path, nil).WithContext(as(4, 1))
			h.ServeHTTP(httptest.NewRecorder(), req)
			_, ownCached, _ := s.Perms.lookup(own)
			_, otherCached, _ := s.Perms.lookup(other)
			if ownCached == tc.clearsOwn || otherCached == tc.clears5 {
				t.Fatalf("own cleared=%v (want %v), tenant 5 cleared=%v (want %v)", !ownCached, tc.clearsOwn, !otherCached, tc.clears5)
			}
			if tc.clearsOwn && !clearedBeforeResponse {
				t.Fatal("cleared only after the response was written")
			}
		})
	}
}

func TestNoCacheMeansAskingEveryTime(t *testing.T) {
	access := &countingAccess{allowed: map[string]bool{"x": true}}
	s := &Server{Access: access}
	for i := 0; i < 3; i++ {
		_, _ = s.allowed(as(4, 11), "x")
	}
	if access.calls != 3 {
		t.Fatalf("calls = %d", access.calls)
	}
}

func TestAWriteThatReturnsWithoutWritingStillClears(t *testing.T) {
	s := &Server{Perms: NewPermissionCache(PermissionCacheTTL)}
	k := permissionKey{tenant: 4, employee: 11, code: "iam:role:write"}
	_, _, ticket := s.Perms.lookup(k)
	s.Perms.store(k, true, ticket)
	h := s.forgetPermissionsOnChange(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/employees/11/roles", nil).WithContext(as(4, 1)))
	if _, cached, _ := s.Perms.lookup(k); cached {
		t.Fatal("an implicit 200 did not clear the cache")
	}
}

func TestHandlerPermissionChecksShareTheCache(t *testing.T) {
	access := &countingAccess{allowed: map[string]bool{"stock:read": true}}
	s, _ := cachedServer(access)
	req := httptest.NewRequest(http.MethodGet, "/api/home/reminders", nil).WithContext(as(4, 11))
	for i := 0; i < 3; i++ {
		if ok, err := s.hasPermission(req, "stock:read"); err != nil || !ok {
			t.Fatalf("hasPermission: %v %v", ok, err)
		}
	}
	if _, _ = s.allowed(as(4, 11), "stock:read"); access.calls != 1 {
		t.Fatalf("handler checks bypassed the cache: %d calls", access.calls)
	}
}
