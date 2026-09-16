package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 主邮箱（00067）：公司的邮箱，管理员替员工分配，员工不能改密码、不能解绑；
// 一个人同时只有一个，可以换。这组用例钉住这几条规矩，以及「员工自己绑的
// 那条路一个字都没变」。
//
// 服务里没接邮件通道（Deps 不给 Mailbox），所以走的是「邮件通道未启用，已
// 保存账号」那条——这里验的是归属、权限和留痕，不是密码对不对。

type companyMailboxFixture struct {
	svc      *Service
	pool     *pgxpool.Pool
	tenantID int64
}

func newCompanyMailboxFixture(t *testing.T) (*companyMailboxFixture, context.Context) {
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
	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
		pool.Close()
	})
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return &companyMailboxFixture{svc: svc, pool: pool, tenantID: tenantID}, ctx
}

type accountState struct {
	employee  int64
	kind      string
	isDefault bool
	isActive  bool
	hasSecret bool
	unbound   bool
}

func (f *companyMailboxFixture) account(t *testing.T, id int64) accountState {
	t.Helper()
	var st accountState
	if err := f.pool.QueryRow(context.Background(), `
		SELECT employee_id, kind, is_default, is_active, length(secret_enc) > 0, unbound_at IS NOT NULL
		FROM mail_accounts WHERE tenant_id=$1 AND id=$2`, f.tenantID, id).
		Scan(&st.employee, &st.kind, &st.isDefault, &st.isActive, &st.hasSecret, &st.unbound); err != nil {
		t.Fatal(err)
	}
	return st
}

// 留痕：某个动作是谁替谁做的。
func (f *companyMailboxFixture) audit(t *testing.T, action string) (employee, actor int64, n int) {
	t.Helper()
	if err := f.pool.QueryRow(context.Background(), `
		SELECT coalesce(max(employee_id),0), coalesce(max(actor_id),0), count(*)
		FROM mail_binding_log WHERE tenant_id=$1 AND action=$2`, f.tenantID, action).
		Scan(&employee, &actor, &n); err != nil {
		t.Fatal(err)
	}
	return employee, actor, n
}

func TestAdminAssignsACompanyMailboxTheEmployeeCannotTouch(t *testing.T) {
	f, ctx := newCompanyMailboxFixture(t)
	const admin, alice, bob = int64(9001), int64(9002), int64(9003)

	res, err := f.svc.AssignCompanyMailbox(ctx, f.tenantID, admin, alice, BindRequest{
		Email: "Sales01@163.com", Provider: "netease163", Secret: "pw-set-by-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Email != "sales01@163.com" {
		t.Fatalf("地址该规范成小写：%q", res.Email)
	}
	st := f.account(t, res.AccountID)
	if st.employee != alice || st.kind != "COMPANY" || !st.isDefault || !st.isActive || !st.hasSecret {
		t.Fatalf("分配完该是 alice 的、COMPANY、默认、可用、有密码：%+v", st)
	}
	// 留痕：箱是 alice 的，手是 admin 的。
	if emp, actor, n := f.audit(t, "ASSIGN"); n != 1 || emp != alice || actor != admin {
		t.Fatalf("分配该留一条痕，员工 alice、操作人 admin：emp=%d actor=%d n=%d", emp, actor, n)
	}

	// alice 自己看：一个箱，标着 COMPANY。
	boxes, err := f.svc.ListMyMailboxes(ctx, f.tenantID, alice)
	if err != nil || len(boxes) != 1 || boxes[0].Kind != "COMPANY" || !boxes[0].IsDefault {
		t.Fatalf("alice 该看到一个 COMPANY 的默认箱：%v %+v", err, boxes)
	}
	view, ok, err := f.svc.CompanyMailboxOf(ctx, f.tenantID, alice)
	if err != nil || !ok || view.AccountID != res.AccountID || view.Email != "sales01@163.com" {
		t.Fatalf("CompanyMailboxOf 该找到它：%v %v %+v", err, ok, view)
	}
	if _, ok, _ := f.svc.CompanyMailboxOf(ctx, f.tenantID, bob); ok {
		t.Fatal("bob 没有主邮箱")
	}

	// ---- 员工碰不得 ----
	// 解绑：拒，而且箱一根毫毛没动。
	if err := f.svc.UnbindMailbox(ctx, f.tenantID, alice, res.AccountID); apierr.CodeFromError(err) != "MAIL_COMPANY_MAILBOX_MANAGED" {
		t.Fatalf("员工解绑主邮箱该被拒：%v", err)
	}
	if st := f.account(t, res.AccountID); !st.isActive || st.unbound || !st.hasSecret {
		t.Fatalf("被拒之后箱该原样：%+v", st)
	}
	// 改密码：员工重新填一次授权码，哪怕能登上也不许。
	if _, err := f.svc.VerifyMailSecret(ctx, f.tenantID, alice, BindRequest{
		Email: "sales01@163.com", Provider: "netease163", Secret: "my-own-guess",
	}); apierr.CodeFromError(err) != "MAIL_COMPANY_MAILBOX_MANAGED" {
		t.Fatalf("员工改主邮箱密码该被拒：%v", err)
	}
	// 别人也拿不走这个地址——不管是自己绑还是管理员分给他。
	if _, err := f.svc.VerifyMailSecret(ctx, f.tenantID, bob, BindRequest{
		Email: "sales01@163.com", Provider: "netease163", Secret: "x",
	}); err == nil {
		t.Fatal("bob 不该能把 alice 的主邮箱绑到自己名下")
	}
	if _, err := f.svc.AssignCompanyMailbox(ctx, f.tenantID, admin, bob, BindRequest{
		Email: "sales01@163.com", Provider: "netease163", Secret: "x",
	}); apierr.CodeFromError(err) != "MAIL_ADDRESS_TAKEN" {
		t.Fatalf("地址还在 alice 名下，分给 bob 该明说被占了（收回是第二步）：%v", err)
	}
}

// 一个人同时只有一个主邮箱：再分一个别的地址，旧的收回、新的分上、留痕两条。
// 旧箱留历史（unbound_at 有值、行还在），和员工自己解绑一个箱一样。
func TestReassigningReplacesTheEmployeesCompanyMailbox(t *testing.T) {
	f, ctx := newCompanyMailboxFixture(t)
	const admin, alice = int64(9101), int64(9102)

	// alice 先自己绑了一个个人箱——它不该受任何影响。
	personal, err := f.svc.VerifyMailSecret(ctx, f.tenantID, alice, BindRequest{
		Email: "alice@qq.com", Provider: "qq", Secret: "mine",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.svc.AssignCompanyMailbox(ctx, f.tenantID, admin, alice, BindRequest{
		Email: "sales01@163.com", Provider: "netease163", Secret: "pw1",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.svc.AssignCompanyMailbox(ctx, f.tenantID, admin, alice, BindRequest{
		Email: "sales02@163.com", Provider: "netease163", Secret: "pw2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.AccountID == second.AccountID {
		t.Fatal("换地址该是新的一行")
	}
	old, now := f.account(t, first.AccountID), f.account(t, second.AccountID)
	if !old.unbound || old.isActive || old.hasSecret || old.kind != "COMPANY" {
		t.Fatalf("旧主邮箱该被收回（留行、清密码、不再可用）：%+v", old)
	}
	if now.unbound || !now.isActive || !now.isDefault || now.kind != "COMPANY" {
		t.Fatalf("新主邮箱该在用、且是默认：%+v", now)
	}
	if p := f.account(t, personal.AccountID); p.kind != "PERSONAL" || !p.isActive || p.isDefault {
		t.Fatalf("个人箱不该受影响，也不该还是默认：%+v", p)
	}
	view, ok, _ := f.svc.CompanyMailboxOf(ctx, f.tenantID, alice)
	if !ok || view.Email != "sales02@163.com" {
		t.Fatalf("现在拿着的该是 sales02：%v %+v", ok, view)
	}
	if emp, actor, n := f.audit(t, "REPLACE"); n != 1 || emp != alice || actor != admin {
		t.Fatalf("换箱该留一条 REPLACE，员工 alice、操作人 admin：emp=%d actor=%d n=%d", emp, actor, n)
	}
	if _, _, n := f.audit(t, "ASSIGN"); n != 2 {
		t.Fatalf("两次分配该留两条 ASSIGN：%d", n)
	}
	// 同一个地址再分一次不算换：一条 REPLACE 都不多。
	if _, err := f.svc.AssignCompanyMailbox(ctx, f.tenantID, admin, alice, BindRequest{
		Email: "sales02@163.com", Provider: "netease163", Secret: "pw2-rotated",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, n := f.audit(t, "REPLACE"); n != 1 {
		t.Fatalf("同一个地址重填密码不是换箱：REPLACE 仍该是 1 条，得到 %d", n)
	}
}

// 员工自己绑的那条路一个字没变：kind 是 PERSONAL，留痕里操作人就是他自己。
func TestPersonalBindingIsUnchangedAndRecordsTheEmployeeAsActor(t *testing.T) {
	f, ctx := newCompanyMailboxFixture(t)
	const me = int64(9201)
	res, err := f.svc.VerifyMailSecret(ctx, f.tenantID, me, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if st := f.account(t, res.AccountID); st.kind != "PERSONAL" || !st.isDefault {
		t.Fatalf("自己绑的该是 PERSONAL、第一个成为默认：%+v", st)
	}
	if emp, actor, n := f.audit(t, "BIND"); n != 1 || emp != me || actor != me {
		t.Fatalf("自己绑的留痕，员工和操作人都该是他自己：emp=%d actor=%d n=%d", emp, actor, n)
	}
	// 自己的个人箱照样能解。
	if err := f.svc.UnbindMailbox(ctx, f.tenantID, me, res.AccountID); err != nil {
		t.Fatalf("解自己的个人箱该照旧：%v", err)
	}
	if emp, actor, n := f.audit(t, "UNBIND"); n != 1 || emp != me || actor != me {
		t.Fatalf("解绑留痕：emp=%d actor=%d n=%d", emp, actor, n)
	}
}
