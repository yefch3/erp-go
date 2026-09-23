package grpcin

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/app"
)

// 只会验证登录的假服务器。
type loginHost struct {
	app.Mailbox
	reject atomic.Bool
	calls  atomic.Int32
}

func (h *loginHost) VerifyLogin(context.Context, app.MailAccount) error {
	h.calls.Add(1)
	if h.reject.Load() {
		return app.NewCredentialRejected(errors.New("LOGIN Login error or password error"))
	}
	return nil
}

// 主邮箱记着「登录被拒」时，员工开锁那一下先拿存着的密码试一次：还被拒就照旧
// 挡着，登上了就放行。后台不再替被拒的箱去试，而主邮箱在被拒时连锁都开不了、
// 员工又改不了它的密码——没有这一次，服务商一次误报就会让它停到管理员出手。
func TestUnlockingACompanyMailboxRetriesItsLoginOnce(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	const owner, admin = int64(9401), int64(9402)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	box, _ := app.NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	svc := app.New(pool, app.Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	res, err := svc.VerifyMailSecret(ctx, tenantID, owner, app.BindRequest{
		Email: "boss@263.net", Provider: "p263", Secret: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts SET kind='COMPANY', auth_failed=true,
		login_rejected_at=now()-interval '1 day', last_error='LOGIN Login error' WHERE tenant_id=$1 AND id=$2`,
		tenantID, res.AccountID); err != nil {
		t.Fatal(err)
	}
	host := &loginHost{}
	svc.UseMailbox(host)
	h := New(svc)
	as := func(employee int64) context.Context {
		return grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenantID, EmployeeID: employee})
	}
	ask := func(c context.Context, req *mailv1.GetCompanyMailboxRequest) *mailv1.CompanyMailbox {
		t.Helper()
		resp, err := h.GetCompanyMailbox(c, req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.GetMailbox() == nil {
			t.Fatal("应该查到这个主邮箱")
		}
		return resp.GetMailbox()
	}

	// 管理员在员工详情页看：不触发登录。
	host.reject.Store(true)
	if m := ask(as(admin), &mailv1.GetCompanyMailboxRequest{EmployeeId: owner, RetryLogin: true}); !m.GetNeedsReauth() {
		t.Fatal("管理员看到的应该还是「需要重新设置密码」")
	}
	// 本人只是看，不开锁：也不触发。
	ask(as(owner), &mailv1.GetCompanyMailboxRequest{})
	if n := host.calls.Load(); n != 0 {
		t.Fatalf("只有本人开锁才该去登录，实际试了 %d 次", n)
	}

	// 本人开锁，服务器还是拒绝：试一次，照旧挡着。
	if m := ask(as(owner), &mailv1.GetCompanyMailboxRequest{RetryLogin: true}); !m.GetNeedsReauth() {
		t.Fatal("还被拒，应该照旧挡着")
	}
	if n := host.calls.Load(); n != 1 {
		t.Fatalf("开锁应该试一次，实际 %d 次", n)
	}

	// 服务商那边好了：这次开锁就放行，而且后台重新接手。
	host.reject.Store(false)
	if m := ask(as(owner), &mailv1.GetCompanyMailboxRequest{RetryLogin: true}); m.GetNeedsReauth() {
		t.Fatal("登上了，应该放行")
	}
	var rejected bool
	if err := pool.QueryRow(ctx, "SELECT login_rejected_at IS NOT NULL FROM mail_accounts WHERE tenant_id=$1 AND id=$2",
		tenantID, res.AccountID).Scan(&rejected); err != nil {
		t.Fatal(err)
	}
	if rejected {
		t.Fatal("登上了，被拒标记应该清掉，后台才会重新收这个箱的信")
	}
}
