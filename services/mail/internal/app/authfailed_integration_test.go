package app

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 服务器拒绝过凭据的信箱：后台一天只试一次登录，员工重新填凭据的那一刻恢复。
//
// 2026-09-23 生产上五个凭据坏掉的箱，状态检查每 7~11 分钟登录一次、被拒一次，
// 一天两千多次；163 已经回了 "login frequency limited"。

// statusHost 只会答 STATUS。err 非空时每次都拒绝。
type statusHost struct {
	Mailbox
	err   error
	calls atomic.Int32
}

func (h *statusHost) FolderStatus(context.Context, MailAccount, string) (FolderStatus, error) {
	h.calls.Add(1)
	if h.err != nil {
		return FolderStatus{}, h.err
	}
	// UIDNext=1、没有未读：「没有新信」，不会被提上来全量同步。
	return FolderStatus{UIDValidity: 7, UIDNext: 1}, nil
}

type rejectingWaiter struct{ calls atomic.Int32 }

func (w *rejectingWaiter) WaitForNews(context.Context, MailAccount, string, time.Duration) (bool, error) {
	w.calls.Add(1)
	return false, NewCredentialRejected(errors.New("LOGIN Login error or password error"))
}

type authFixture struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	svc    *Service
	tenant int64
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &authFixture{ctx: ctx, pool: pool, tenant: time.Now().UnixNano()}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", f.tenant)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", f.tenant)
		pool.Close()
	})
	box, _ := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	f.svc = New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return f
}

// bind 绑一个箱，再按 sets 直接改那一行（SQL 片段，$2 是账号 id）。
func (f *authFixture) bind(t *testing.T, employee int64, email, sets string) int64 {
	t.Helper()
	res, err := f.svc.VerifyMailSecret(f.ctx, f.tenant, employee, BindRequest{
		Email: email, Provider: "p263", Secret: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sets != "" {
		if _, err := f.pool.Exec(f.ctx, "UPDATE mail_accounts SET "+sets+" WHERE tenant_id=$1 AND id=$2",
			f.tenant, res.AccountID); err != nil {
			t.Fatal(err)
		}
	}
	return res.AccountID
}

func (f *authFixture) authFailed(t *testing.T, id int64) (failed bool, checked bool) {
	t.Helper()
	if err := f.pool.QueryRow(f.ctx,
		"SELECT auth_failed, status_checked_at IS NOT NULL FROM mail_accounts WHERE tenant_id=$1 AND id=$2",
		f.tenant, id).Scan(&failed, &checked); err != nil {
		t.Fatal(err)
	}
	return failed, checked
}

func (f *authFixture) cfg() SyncConfig {
	return SyncConfig{TenantID: f.tenant, Folder: "INBOX"}.withDefaults()
}

func (f *authFixture) due(t *testing.T) map[int64]bool {
	t.Helper()
	// 参数走和生产同一个函数：钉住 Go 这一侧真的把「一天」传进去了。
	rows, err := f.svc.q.ListMailboxesDueForStatus(f.ctx, dueForStatusParams(f.cfg()))
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]bool{}
	for _, r := range rows {
		out[r.ID] = true
	}
	return out
}

func (f *authFixture) active(t *testing.T) map[int64]bool {
	t.Helper()
	cfg := f.cfg()
	rows, err := f.svc.q.ListActiveMailAccounts(f.ctx, store.ListActiveMailAccountsParams{
		TenantID: cfg.TenantID, ActiveSeconds: int32(cfg.ActiveWindow.Seconds()),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]bool{}
	for _, r := range rows {
		out[r.ID] = true
	}
	return out
}

// 谁在哪一档：被拒的箱不进全量那一档、不被常开连接守着，轻状态那一档一天一次。
func TestRejectedMailboxesAreTriedOncePerDay(t *testing.T) {
	f := newAuthFixture(t)
	normal := f.bind(t, 9301, "a@263.net", "")
	justFailed := f.bind(t, 9302, "b@263.net", "auth_failed=true, status_checked_at=now()-interval '1 hour'")
	failedLongAgo := f.bind(t, 9303, "c@263.net", "auth_failed=true, status_checked_at=now()-interval '25 hours'")
	failedButRead := f.bind(t, 9304, "d@263.net", "auth_failed=true, last_read_at=now(), status_checked_at=now()-interval '1 hour'")
	beingRead := f.bind(t, 9305, "e@263.net", "last_read_at=now()")

	due := f.due(t)
	if !due[normal] {
		t.Error("正常、没人看、从没问过的箱应该被问")
	}
	if due[justFailed] {
		t.Error("一小时前刚被拒的箱不该再问——一天一次")
	}
	if !due[failedLongAgo] {
		t.Error("被拒已经超过一天的箱应该试一次：服务商有时把一时忙报成密码错")
	}
	if due[failedButRead] {
		t.Error("有人在看也一样一天一次")
	}

	active := f.active(t)
	if !active[beingRead] {
		t.Error("有人在看的正常箱应该在全量那一档")
	}
	if active[failedButRead] {
		t.Error("凭据被拒的箱不该每两分钟全量同步一次")
	}

	being, err := f.svc.q.MailboxIsBeingRead(f.ctx, store.MailboxIsBeingReadParams{
		TenantID: f.tenant, ID: failedButRead, ActiveSeconds: int32(f.cfg().ActiveWindow.Seconds()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if being {
		t.Error("凭据被拒的箱不该被常开连接守着")
	}
}

// 状态检查被拒：记到账号上（设置页出「重新登录」、后台据此一天一次），
// 一天后那次试探成功就恢复正常。
func TestAStatusCheckRejectionIsRecordedAndHealsItself(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9311, "x@263.net", "")
	host := &statusHost{err: NewCredentialRejected(errors.New("LOGIN Login error or password error"))}
	f.svc.UseMailbox(host)

	f.svc.checkMailboxStatus(f.ctx, f.cfg(), id)
	if failed, checked := f.authFailed(t, id); !failed || !checked {
		t.Fatalf("被拒之后应该记下 auth_failed 和检查时间，实际 auth_failed=%v checked=%v", failed, checked)
	}
	if f.due(t)[id] {
		t.Fatal("刚被拒的箱不该在下一轮又被挑出来")
	}

	// 一天以后，服务商那次原来是误报：这次登上去了。
	if _, err := f.pool.Exec(f.ctx, "UPDATE mail_accounts SET status_checked_at=now()-interval '25 hours' WHERE tenant_id=$1 AND id=$2", f.tenant, id); err != nil {
		t.Fatal(err)
	}
	host.err = nil
	if !f.due(t)[id] {
		t.Fatal("满一天应该再试一次")
	}
	f.svc.checkMailboxStatus(f.ctx, f.cfg(), id)
	if failed, _ := f.authFailed(t, id); failed {
		t.Fatal("登上去了，auth_failed 应该清掉，它才能回到正常那一档")
	}
}

// 连不上不是凭据问题：不该让一个只是网络抖了的箱掉进「一天一次」。
func TestANetworkFailureDoesNotMarkTheCredentialsBad(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9321, "y@263.net", "")
	f.svc.UseMailbox(&statusHost{err: errors.New("dial tcp: i/o timeout")})

	f.svc.checkMailboxStatus(f.ctx, f.cfg(), id)
	if failed, checked := f.authFailed(t, id); failed || !checked {
		t.Fatalf("网络失败：不该标成凭据被拒，但要记检查时间；实际 auth_failed=%v checked=%v", failed, checked)
	}
}

// 员工重新填了授权码：那一刻就恢复，并排到下一轮最前面。不清的话，后台
// 一直跳过它，重新绑了也收不到信。
func TestNewCredentialsClearTheRejectionAtOnce(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9331, "z@263.net", "auth_failed=true, status_checked_at=now()")

	if _, err := f.svc.VerifyMailSecret(f.ctx, f.tenant, 9331, BindRequest{
		Email: "z@263.net", Provider: "p263", Secret: "new-pw",
	}); err != nil {
		t.Fatal(err)
	}
	if failed, checked := f.authFailed(t, id); failed || checked {
		t.Fatalf("重新填了授权码应该清掉 auth_failed 并排到最前，实际 auth_failed=%v checked=%v", failed, checked)
	}
	if !f.due(t)[id] {
		t.Fatal("重新绑好的箱应该在下一轮就被问到")
	}
}

// 常开连接登录被拒：停下来，并记到账号上——从前只是退避重连，有人开着页面
// 就最长十分钟一次，永远重连、永远被拒。
func TestAnIdleWatchStopsWhenTheCredentialsAreRejected(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9341, "w@263.net", "last_read_at=now()")
	waiter := &rejectingWaiter{}

	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	go func() {
		f.svc.watchMailbox(ctx, f.cfg(), waiter, id)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("登录被拒之后常开连接应该停下，但它还在重连（已试 %d 次）", waiter.calls.Load())
	}
	if n := waiter.calls.Load(); n != 1 {
		t.Errorf("应该只试一次，实际 %d 次", n)
	}
	if failed, _ := f.authFailed(t, id); !failed {
		t.Error("被拒这件事应该记到账号上，管理器才不会把它再挑出来")
	}
}
