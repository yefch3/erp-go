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

// 收信登录被服务器拒绝过的信箱：后台不再拿这份凭据去登录。只剩两个口子——
// 员工本人打开邮箱页时的那次收信，和员工重新填凭据——成功了就恢复。
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

type loginState struct {
	banner   bool // auth_failed：横幅上「重新登录」
	rejected bool // login_rejected_at 有值：后台放慢重试
	checked  bool // status_checked_at 有值
}

func (f *authFixture) state(t *testing.T, id int64) loginState {
	t.Helper()
	var st loginState
	if err := f.pool.QueryRow(f.ctx, `SELECT auth_failed, login_rejected_at IS NOT NULL, status_checked_at IS NOT NULL
		FROM mail_accounts WHERE tenant_id=$1 AND id=$2`, f.tenant, id).Scan(&st.banner, &st.rejected, &st.checked); err != nil {
		t.Fatal(err)
	}
	return st
}

func (f *authFixture) cfg() SyncConfig {
	return SyncConfig{TenantID: f.tenant, Folder: "INBOX"}.withDefaults()
}

func (f *authFixture) due(t *testing.T) map[int64]bool {
	t.Helper()
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

// 后台哪一档都不挑收信登录被拒的箱：不问轻状态（不管拒了多久、多久没问过）、
// 不进全量同步、不被常开连接守着。
func TestTheBackgroundNeverLogsIntoARejectedMailbox(t *testing.T) {
	f := newAuthFixture(t)
	normal := f.bind(t, 9301, "a@263.net", "")
	beingRead := f.bind(t, 9302, "b@263.net", "last_read_at=now()")
	rejected := map[int64]string{
		f.bind(t, 9303, "c@263.net", "auth_failed=true, login_rejected_at=now()-interval '1 hour', status_checked_at=now()-interval '31 minutes'"): "刚被拒、半小时没问过",
		f.bind(t, 9304, "d@263.net", "auth_failed=true, login_rejected_at=now()-interval '3 days', status_checked_at=now()-interval '25 hours'"):   "拒了三天、一天没问过",
		f.bind(t, 9305, "e@263.net", "auth_failed=true, login_rejected_at=now()-interval '3 days'"):                                                "从没问过",
	}
	rejectedButRead := f.bind(t, 9306, "f@263.net", "auth_failed=true, login_rejected_at=now()-interval '1 hour', last_read_at=now()")

	due := f.due(t)
	if !due[normal] {
		t.Error("正常、没人看、从没问过的箱应该被问")
	}
	for id, why := range rejected {
		if due[id] {
			t.Errorf("收信登录被拒的箱不该被问轻状态（%s）", why)
		}
	}
	if due[rejectedButRead] {
		t.Error("有人在看也一样不问")
	}

	active := f.active(t)
	if !active[beingRead] {
		t.Error("有人在看的正常箱应该在全量那一档")
	}
	if active[rejectedButRead] {
		t.Error("收信登录被拒的箱不该全量同步")
	}
	being, err := f.svc.q.MailboxIsBeingRead(f.ctx, store.MailboxIsBeingReadParams{
		TenantID: f.tenant, ID: rejectedButRead, ActiveSeconds: int32(f.cfg().ActiveWindow.Seconds()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if being {
		t.Error("收信登录被拒的箱不该被常开连接守着")
	}
}

// pageOpenHost 够一次完整收信用：收件箱照 reject 答，其余文件夹一律「没有」，
// 那几段收信就跳过了。
type pageOpenHost struct {
	Mailbox
	reject  atomic.Bool
	fetches atomic.Int32
}

func (h *pageOpenHost) Fetch(context.Context, MailAccount, string, uint32, uint32) (FetchResult, error) {
	h.fetches.Add(1)
	if h.reject.Load() {
		return FetchResult{}, NewCredentialRejected(errors.New("LOGIN Login error or password error"))
	}
	return FetchResult{UIDValidity: 7}, nil
}
func (h *pageOpenHost) FetchBelow(context.Context, MailAccount, string, uint32, uint32) (FetchResult, error) {
	return FetchResult{UIDValidity: 7}, nil
}
func (h *pageOpenHost) SentFolder(context.Context, MailAccount) (string, error) {
	return "", errors.New("没有")
}
func (h *pageOpenHost) JunkFolder(context.Context, MailAccount) (string, error) {
	return "", errors.New("没有")
}
func (h *pageOpenHost) TrashFolder(context.Context, MailAccount) (string, error) {
	return "", errors.New("没有")
}
func (h *pageOpenHost) ArchiveFolder(context.Context, MailAccount) (string, error) {
	return "", errors.New("没有")
}
func (h *pageOpenHost) ListFolders(context.Context, MailAccount) ([]HostFolder, error) {
	return nil, errors.New("没有")
}
func (h *pageOpenHost) SearchFlagged(context.Context, MailAccount, string) ([]uint32, error) {
	return nil, nil
}
func (h *pageOpenHost) FetchFlags(context.Context, MailAccount, string, []uint32) (map[uint32]MessageFlags, error) {
	return map[uint32]MessageFlags{}, nil
}
func (h *pageOpenHost) RecentMessageIDs(context.Context, MailAccount, string, uint32) (map[string]bool, error) {
	return map[string]bool{}, nil
}

// openPage 等于员工打开邮箱页（syncOnOpen → /mailbox/sync），并等后台那半截
// 收完，免得清理测试数据时它还在写。
func (f *authFixture) openPage(t *testing.T, id int64) {
	t.Helper()
	cfg := f.cfg()
	_, _, _ = f.svc.SyncMailboxInteractive(f.ctx, cfg, id)
	deadline := time.Now().Add(5 * time.Second)
	for f.svc.fleet(cfg.Concurrency).running() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

// 后台不试了，那么「服务商把登录太频繁误报成密码错」这种误判靠什么恢复：员工
// 打开邮箱页时的那次收信。人在场，看得到横幅；这次登上去了就当场恢复正常。
func TestOpeningTheMailboxPageRetriesAndRecovers(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9381, "p@263.net", "auth_failed=true, login_rejected_at=now()-interval '1 hour', last_read_at=now()")
	host := &pageOpenHost{}
	host.reject.Store(true)
	f.svc.UseMailbox(host)

	// 还是被拒：试了一次，照旧记着，后台照旧不碰。
	f.openPage(t, id)
	if n := host.fetches.Load(); n != 1 {
		t.Fatalf("打开页面应该试一次，实际 %d 次", n)
	}
	if st := f.state(t, id); !st.banner || !st.rejected {
		t.Fatalf("还是被拒：横幅和被拒标记都该留着；实际 %+v", st)
	}

	// 服务商那边好了：这次登上去了。
	host.reject.Store(false)
	f.openPage(t, id)
	if st := f.state(t, id); st.banner || st.rejected {
		t.Fatalf("登上去了，横幅和被拒标记都应该清掉；实际 %+v", st)
	}
	if !f.active(t)[id] {
		t.Fatal("恢复之后，有人在看的箱应该回到全量同步那一档")
	}
}

// 员工在 ERP 里标已读、删信，要写回邮件服务器。凭据坏着的时候先不写：拿坏
// 凭据去写只会被拒，二十次之后这条操作被放弃，员工那次操作就丢了。凭据修好
// 之后照常写回。
func TestWriteBacksWaitForTheCredentialsToBeFixed(t *testing.T) {
	f := newAuthFixture(t)
	t.Cleanup(func() { _, _ = f.pool.Exec(f.ctx, "DELETE FROM mail_flag_ops WHERE tenant_id=$1", f.tenant) })
	id := f.bind(t, 9391, "q@263.net", "auth_failed=true, login_rejected_at=now()-interval '1 hour'")
	if err := f.svc.q.EnqueueFlagOp(f.ctx, store.EnqueueFlagOpParams{
		TenantID: f.tenant, AccountID: id, EmployeeID: 9391,
		Folder: "INBOX", ImapUid: 5, Flag: `\Seen`, Op: opAdd, MessageID: "w@mid",
	}); err != nil {
		t.Fatal(err)
	}
	claim := func() int {
		t.Helper()
		rows, err := f.svc.q.ClaimFlagOps(f.ctx, store.ClaimFlagOpsParams{TenantID: f.tenant, RowLimit: 10})
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	if n := claim(); n != 0 {
		t.Fatalf("凭据坏着的时候不该取出写回操作，取出了 %d 条", n)
	}
	var attempts int32
	if err := f.pool.QueryRow(f.ctx, "SELECT attempts FROM mail_flag_ops WHERE tenant_id=$1", f.tenant).Scan(&attempts); err != nil {
		t.Fatalf("操作应该还在队列里：%v", err)
	}
	if attempts != 0 {
		t.Fatalf("没去写就不该记失败次数，实际 %d", attempts)
	}

	// 员工重新填了授权码。
	if _, err := f.svc.VerifyMailSecret(f.ctx, f.tenant, 9391, BindRequest{
		Email: "q@263.net", Provider: "p263", Secret: "new-pw",
	}); err != nil {
		t.Fatal(err)
	}
	if n := claim(); n != 1 {
		t.Fatalf("凭据修好之后应该照常写回，取出了 %d 条", n)
	}
}

// 发信失败只管横幅，不能让一个箱停止收信。发信那条路连超时、断线、对方限流都
// 记成 authProblem（adapter/provider/smtp.go）；如果后台按横幅那一位排班，发
// 一封信时网络抖一下，这个箱就一整天不自动收信（2026-09-23 审查发现）。
func TestASendFailureDoesNotStopReceiving(t *testing.T) {
	f := newAuthFixture(t)
	idle := f.bind(t, 9351, "s1@263.net", "")
	read := f.bind(t, 9352, "s2@263.net", "last_read_at=now()")
	for _, id := range []int64{idle, read} {
		// 和 smtp.go 一样的调用。
		f.svc.RecordFailure(f.ctx, f.tenant, id, "smtp auth: EOF", true)
	}
	if st := f.state(t, idle); !st.banner || st.rejected {
		t.Fatalf("发信失败：横幅照亮，但不算收信被拒；实际 %+v", st)
	}
	if !f.due(t)[idle] {
		t.Error("发信失败的箱照常问轻状态")
	}
	if !f.active(t)[read] {
		t.Error("发信失败的箱有人在看就照常全量同步")
	}
}

// 被拒的起始时间不随后来的失败往后挪：记的是「从什么时候开始被拒」，排查时
// 一眼看得出断了多久。
func TestARejectionKeepsItsStartTime(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9361, "r@263.net", "auth_failed=true, login_rejected_at=now()-interval '3 days'")
	f.svc.recordLoginRejected(f.ctx, f.tenant, id, NewCredentialRejected(errors.New("LOGIN Login error")))
	var old bool
	if err := f.pool.QueryRow(f.ctx, "SELECT login_rejected_at < now()-interval '2 days' FROM mail_accounts WHERE tenant_id=$1 AND id=$2",
		f.tenant, id).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if !old {
		t.Fatal("再次被拒不该把起始时间挪到现在")
	}
}

// 被拒的箱后来碰上一次网络错误（有人打开页面时那次收信超时了）：横幅换成网络
// 的说法没关系，但后台不能因此把它放回全量同步——凭据还是那份坏的。
func TestALaterNetworkErrorDoesNotPutARejectedMailboxBack(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9371, "n@263.net", "auth_failed=true, login_rejected_at=now()-interval '1 hour', last_read_at=now()")
	f.svc.RecordFailure(f.ctx, f.tenant, id, "dial tcp: i/o timeout", false)
	if f.active(t)[id] {
		t.Fatal("一次网络错误不该让收信被拒的箱回到全量同步")
	}
}

// 状态检查被拒：记到账号上（设置页出「重新登录」），之后后台再也不挑它。
func TestAStatusCheckRejectionStopsTheBackground(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9311, "x@263.net", "")
	host := &statusHost{err: NewCredentialRejected(errors.New("LOGIN Login error or password error"))}
	f.svc.UseMailbox(host)

	f.svc.checkMailboxStatus(f.ctx, f.cfg(), id)
	if st := f.state(t, id); !st.banner || !st.rejected || !st.checked {
		t.Fatalf("被拒之后应该记下横幅、被拒起始时间和检查时间，实际 %+v", st)
	}
	if f.due(t)[id] {
		t.Fatal("被拒的箱不该在下一轮又被挑出来")
	}
	// 过了一天也一样：后台不再去试。
	if _, err := f.pool.Exec(f.ctx, "UPDATE mail_accounts SET status_checked_at=now()-interval '25 hours' WHERE tenant_id=$1 AND id=$2", f.tenant, id); err != nil {
		t.Fatal(err)
	}
	if f.due(t)[id] {
		t.Fatal("过了一天后台也不该再去试")
	}
}

// 连不上不是凭据问题：不该让一个只是网络抖了的箱掉进「一天一次」。
func TestANetworkFailureDoesNotMarkTheCredentialsBad(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9321, "y@263.net", "")
	f.svc.UseMailbox(&statusHost{err: errors.New("dial tcp: i/o timeout")})

	f.svc.checkMailboxStatus(f.ctx, f.cfg(), id)
	if st := f.state(t, id); st.banner || st.rejected || !st.checked {
		t.Fatalf("网络失败：不该标成凭据被拒，但要记检查时间；实际 %+v", st)
	}
}

// 员工重新填了授权码：那一刻就恢复，并排到下一轮最前面。不清的话，后台
// 一直跳过它，重新绑了也收不到信。
func TestNewCredentialsClearTheRejectionAtOnce(t *testing.T) {
	f := newAuthFixture(t)
	id := f.bind(t, 9331, "z@263.net", "auth_failed=true, login_rejected_at=now(), status_checked_at=now()")

	if _, err := f.svc.VerifyMailSecret(f.ctx, f.tenant, 9331, BindRequest{
		Email: "z@263.net", Provider: "p263", Secret: "new-pw",
	}); err != nil {
		t.Fatal(err)
	}
	if st := f.state(t, id); st.banner || st.rejected || st.checked {
		t.Fatalf("重新填了授权码应该清掉横幅和被拒时间、并排到最前，实际 %+v", st)
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
	if st := f.state(t, id); !st.banner || !st.rejected {
		t.Errorf("被拒这件事应该记到账号上，管理器才不会把它再挑出来；实际 %+v", st)
	}
}
