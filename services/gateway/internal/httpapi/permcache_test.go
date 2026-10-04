package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// liveIAM is iam on the other end of a real gRPC connection, so the test goes
// through the same cached connection the gateway dials in production. Grants
// can change between calls, as they do when an administrator edits a role.
type liveIAM struct {
	iamv1.UnimplementedAccessServiceServer
	mu     sync.Mutex
	grants map[string]bool
	checks int
	lists  int
}

func (l *liveIAM) CheckPermission(_ context.Context, req *iamv1.CheckPermissionRequest) (*iamv1.CheckPermissionResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checks++
	return &iamv1.CheckPermissionResponse{Allowed: l.grants[req.GetPermissionCode()]}, nil
}

func (l *liveIAM) ListEmployeePermissions(_ context.Context, _ *iamv1.ListEmployeePermissionsRequest) (*iamv1.ListEmployeePermissionsResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lists++
	codes := []string{}
	for code, ok := range l.grants {
		if ok {
			codes = append(codes, code)
		}
	}
	return &iamv1.ListEmployeePermissionsResponse{PermissionCodes: codes}, nil
}

func (l *liveIAM) set(code string, granted bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.grants[code] = granted
}

func (l *liveIAM) counts() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.checks, l.lists
}

// cachedGateway is a Server whose iam connection carries the answer cache,
// exactly as cmd/main.go builds it.
func cachedGateway(t *testing.T) (*Server, *liveIAM) {
	t.Helper()
	iam := &liveIAM{grants: map[string]bool{"iam:role:write": true}}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	iamv1.RegisterAccessServiceServer(srv, iam)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	answers, cache := grpcx.CacheAccessAnswers(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	conn, err := grpc.NewClient("passthrough:///iam",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()), answers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &Server{Access: iamv1.NewAccessServiceClient(conn), Answers: cache}, iam
}

func as(tenant, employee int64) context.Context {
	return grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: tenant, EmployeeID: employee})
}

func TestGatewayPermissionChecksAreAnsweredFromTheCache(t *testing.T) {
	s, iam := cachedGateway(t)
	req := httptest.NewRequest(http.MethodGet, "/api/home/reminders", nil).WithContext(as(4, 11))
	for i := 0; i < 3; i++ {
		if ok, err := s.allowed(as(4, 11), "iam:role:write"); err != nil || !ok {
			t.Fatalf("allowed: %v %v", ok, err)
		}
		if ok, err := s.hasPermission(req, "iam:role:write"); err != nil || !ok {
			t.Fatalf("hasPermission: %v %v", ok, err)
		}
		rec := httptest.NewRecorder()
		s.me(rec, httptest.NewRequest(http.MethodGet, "/api/me/permissions", nil).WithContext(as(4, 11)))
		if rec.Code != http.StatusOK {
			t.Fatalf("me: %d", rec.Code)
		}
	}
	if checks, lists := iam.counts(); checks != 1 || lists != 1 {
		t.Fatalf("iam asked %d checks and %d lists for one question each", checks, lists)
	}
}

func TestARoleChangeTakesEffectAtOnce(t *testing.T) {
	s, iam := cachedGateway(t)
	if ok, _ := s.allowed(as(4, 11), "iam:role:write"); !ok {
		t.Fatal("setup: permission missing")
	}
	// The administrator takes the permission away through the gateway.
	h := s.forgetPermissionsOnChange(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		iam.set("iam:role:write", false)
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/roles/7/permissions", nil).WithContext(as(4, 1)))
	if ok, _ := s.allowed(as(4, 11), "iam:role:write"); ok {
		t.Fatal("a removed permission was still answered from the cache")
	}
	rec := httptest.NewRecorder()
	s.me(rec, httptest.NewRequest(http.MethodGet, "/api/me/permissions", nil).WithContext(as(4, 11)))
	if _, lists := iam.counts(); lists != 1 || rec.Code != http.StatusOK {
		t.Fatalf("permission list not re-read after the change: lists=%d", lists)
	}
}

func TestWhichWritesForgetWhichCompany(t *testing.T) {
	cases := []struct {
		name, method, path  string
		status              int
		clearsOwn, clearsT5 bool
	}{
		{"role permissions edited", http.MethodPut, "/api/roles/7/permissions", http.StatusOK, true, false},
		{"roles assigned", http.MethodPost, "/api/employees/11/roles", http.StatusOK, true, false},
		{"employee deactivated", http.MethodDelete, "/api/employees/11", http.StatusOK, true, false},
		{"employee import", http.MethodPost, "/api/employees/import", http.StatusOK, true, false},
		{"department moved", http.MethodPut, "/api/departments/3", http.StatusOK, true, false},
		{"company switched off", http.MethodPost, "/api/platform/tenants/5/status", http.StatusOK, true, true},
		{"refused write", http.MethodPut, "/api/roles/7/permissions", http.StatusForbidden, false, false},
		{"read", http.MethodGet, "/api/roles", http.StatusOK, false, false},
		{"unrelated write", http.MethodPost, "/api/customers", http.StatusOK, false, false},
		{"avatar urls, a read sent as POST", http.MethodPost, "/api/employees/avatar-urls", http.StatusOK, false, false},
		{"password reset by an admin", http.MethodPost, "/api/employees/11/password", http.StatusOK, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, iam := cachedGateway(t)
			_, _ = s.allowed(as(4, 11), "iam:role:write")
			_, _ = s.allowed(as(5, 11), "iam:role:write")
			var clearedBeforeResponse bool
			h := s.forgetPermissionsOnChange(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				before, _ := iam.counts()
				_, _ = s.allowed(as(4, 11), "iam:role:write")
				after, _ := iam.counts()
				clearedBeforeResponse = after > before
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil).WithContext(as(4, 1)))
			before, _ := iam.counts()
			_, _ = s.allowed(as(5, 11), "iam:role:write")
			after, _ := iam.counts()
			if clearedBeforeResponse != tc.clearsOwn || (after > before) != tc.clearsT5 {
				t.Fatalf("own cleared=%v (want %v), tenant 5 cleared=%v (want %v)", clearedBeforeResponse, tc.clearsOwn, after > before, tc.clearsT5)
			}
		})
	}
}

func TestAWriteThatReturnsWithoutWritingStillForgets(t *testing.T) {
	s, iam := cachedGateway(t)
	_, _ = s.allowed(as(4, 11), "iam:role:write")
	h := s.forgetPermissionsOnChange(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/employees/11/roles", nil).WithContext(as(4, 1)))
	_, _ = s.allowed(as(4, 11), "iam:role:write")
	if checks, _ := iam.counts(); checks != 2 {
		t.Fatalf("an implicit 200 did not forget: %d checks", checks)
	}
}

func TestNoCacheMeansNothingToForget(t *testing.T) {
	s := &Server{}
	called := false
	h := s.forgetPermissionsOnChange(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/roles/7/permissions", nil).WithContext(as(4, 1)))
	if !called {
		t.Fatal("the request did not reach its handler")
	}
}

// The answers inside every service may be as old as a revoked session may be
// long-lived; the two promises are one promise and must move together.
func TestAnswerTTLIsTheRevocationBound(t *testing.T) {
	if grpcx.AnswerTTL != revocationRefresh {
		t.Fatalf("access answers live %v but revocations take %v", grpcx.AnswerTTL, revocationRefresh)
	}
}
