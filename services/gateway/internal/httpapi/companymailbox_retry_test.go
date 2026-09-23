package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type companyMailboxStub struct {
	mailv1.EmailServiceClient
	got *mailv1.GetCompanyMailboxRequest
}

func (s *companyMailboxStub) GetCompanyMailbox(_ context.Context, r *mailv1.GetCompanyMailboxRequest, _ ...grpc.CallOption) (*mailv1.GetCompanyMailboxResponse, error) {
	s.got = r
	return &mailv1.GetCompanyMailboxResponse{Mailbox: &mailv1.CompanyMailbox{
		AccountId: 5, Email: "boss@263.net", NeedsReauth: true, IsActive: true,
	}}, nil
}

// 员工打开邮箱页开锁：让 mail 先拿存着的密码试一次（RetryLogin）。还被拒就照旧 409。
// 管理员在员工详情页查看：不试——那不是这个邮箱主人在场的时候。
func TestOnlyUnlockingAsksMailToRetryTheCompanyMailboxLogin(t *testing.T) {
	stub := &companyMailboxStub{}
	s := &Server{Emails: stub}
	ctx := grpcx.WithOperator(context.Background(), grpcx.Operator{TenantID: 4, EmployeeID: 11})

	w := httptest.NewRecorder()
	s.unlockCompanyMailbox(w, httptest.NewRequest("POST", "/api/mailbox/unlock-company", nil).WithContext(ctx))
	if stub.got == nil || !stub.got.GetRetryLogin() {
		t.Fatal("开锁应该让 mail 先试一次登录")
	}
	if w.Code != 409 {
		t.Fatalf("试过还被拒，应该照旧 409，实际 %d", w.Code)
	}

	stub.got = nil
	route := chi.NewRouteContext()
	route.URLParams.Add("id", "12")
	r := httptest.NewRequest("GET", "/api/employees/12/company-mailbox", nil)
	w = httptest.NewRecorder()
	s.getCompanyMailbox(w, r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route)))
	if stub.got == nil || stub.got.GetRetryLogin() {
		t.Fatal("管理员查看不该触发登录")
	}
}
