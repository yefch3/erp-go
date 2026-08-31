package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 一个人绑两个信箱之后，两个都得是活的。
//
// 这条钉的是 00043 放开 UNIQUE (tenant_id, employee_id) 之后必须成立的性质。
// 它们今天全都成立，但每一条在改回"按人查"的那一刻都会**静默地**不成立——
// 那正是这一整批改动存在的理由：
//
//	· 两个信箱各有各的同步游标（mail_sync_state 按 account_id 分），
//	  不会一个推进、另一个永远停在 0
//	· 两个信箱各有各的凭据和服务器，跨服务商（263 + Gmail）能并存
//	· 收到的信按 account_id 分开，不会混
//	· 默认信箱只有一个，而且是人选的那个——不是 id 最小的那个
func TestTwoMailboxesForOnePersonBothStayAlive(t *testing.T) {
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

	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	employeeID := tenantID%100000 + 810001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_sync_state WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 同一个人，两个服务商。第一个绑的自动成为默认。
	if err := svc.SaveMailAccount(ctx, tenantID, employeeID, "me@sunrise.com", "", "code-263"); err != nil {
		t.Fatalf("绑第一个信箱：%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts
		SET smtp_host='smtp.263.net', imap_host='imap.263.net'
		WHERE tenant_id=$1 AND email='me@sunrise.com'`, tenantID); err != nil {
		t.Fatal(err)
	}
	// 第二个走 UpsertMailAccountShell 那条路——冲突键是地址，所以这是新增
	// 一行，不是把第一个改掉。
	second, err := svc.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
		TenantID: tenantID, EmployeeID: employeeID, Email: "me@gmail.com",
	})
	if err != nil {
		t.Fatalf("绑第二个信箱：%v", err)
	}
	// 密文的 AAD 绑定行 id，所以只能拿到 id 之后再封——和生产代码
	// UpsertMailAccountShell → SetMailAccountSecret 同一个顺序。
	blob, err := box.Seal([]byte("code-gmail"), AccountAAD(tenantID, second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE mail_accounts
		SET smtp_host='smtp.gmail.com', imap_host='imap.gmail.com',
		    secret_enc=$3, key_version=$4
		WHERE tenant_id=$1 AND id=$2`, tenantID, second, blob, box.Version()); err != nil {
		t.Fatal(err)
	}

	boxes, err := svc.q.ListMailAccountsForEmployee(ctx, store.ListMailAccountsForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 2 {
		t.Fatalf("应该有两个信箱，实际 %d 个——UNIQUE (tenant_id, employee_id) 还在？", len(boxes))
	}

	// 默认只有一个，而且是先绑的那个。**加一个信箱不该悄悄换掉发件人。**
	var defaults int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mail_accounts
		WHERE tenant_id=$1 AND employee_id=$2 AND is_default`, tenantID, employeeID).Scan(&defaults); err != nil {
		t.Fatal(err)
	}
	if defaults != 1 {
		t.Fatalf("默认信箱应该正好一个，实际 %d 个", defaults)
	}
	firstID, err := svc.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == second {
		t.Error("加第二个信箱把默认发件人换掉了——写信的发件人不该因为绑了个新箱就变")
	}

	// 各自的凭据和服务器互不串。密文的 AAD 绑定行 id，解对了就证明取对了行。
	a, err := svc.ForAccount(ctx, tenantID, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "me@sunrise.com" || a.Secret != "code-263" {
		t.Errorf("第一个信箱拿到 %s / %q", a.Email, a.Secret)
	}
	b, err := svc.ForAccount(ctx, tenantID, second)
	if err != nil {
		t.Fatal(err)
	}
	if b.Email != "me@gmail.com" || b.IMAPHost != "imap.gmail.com" || b.Secret != "code-gmail" {
		t.Errorf("第二个信箱拿到 %s / %s / %q——跨服务商并存不成立",
			b.Email, b.IMAPHost, b.Secret)
	}

	// 同步游标各走各的。这一条是「第二个箱静默收不到信」的直接反面：
	// 按人查的那一版里，被挑中的箱推进，另一个永远停在 0。
	for _, c := range []struct {
		id  int64
		uid int64
	}{{firstID, 11}, {second, 22}} {
		if _, err := pool.Exec(ctx, `INSERT INTO mail_sync_state
			(tenant_id, account_id, folder, last_uid) VALUES ($1,$2,'INBOX',$3)`,
			tenantID, c.id, c.uid); err != nil {
			t.Fatal(err)
		}
	}
	var uidA, uidB int64
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT last_uid FROM mail_sync_state WHERE tenant_id=$1 AND account_id=$2 AND folder='INBOX'),
		(SELECT last_uid FROM mail_sync_state WHERE tenant_id=$1 AND account_id=$3 AND folder='INBOX')`,
		tenantID, firstID, second).Scan(&uidA, &uidB); err != nil {
		t.Fatal(err)
	}
	if uidA != 11 || uidB != 22 {
		t.Fatalf("两个信箱的同步游标应该各是各的，拿到 %d / %d", uidA, uidB)
	}

	// 改默认：清旧设新是一条语句，中途不会出现零个或两个默认。
	if err := svc.SetDefaultMailbox(ctx, tenantID, employeeID, second); err != nil {
		t.Fatal(err)
	}
	nowDefault, err := svc.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if nowDefault != second {
		t.Errorf("改完默认应该是 %d，拿到 %d", second, nowDefault)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM mail_accounts
		WHERE tenant_id=$1 AND employee_id=$2 AND is_default`, tenantID, employeeID).Scan(&defaults); err != nil {
		t.Fatal(err)
	}
	if defaults != 1 {
		t.Fatalf("改完默认之后仍该正好一个，实际 %d 个", defaults)
	}
}

// 别人已经绑了的地址，第二个人绑不上——UNIQUE (tenant_id, email) 是 00043
// 有意留下的那一条，而撞上时要说人话，不是抛 23505。
func TestOneMailboxCannotBelongToTwoPeople(t *testing.T) {
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
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	empA, empB := tenantID%100000+820001, tenantID%100000+820002
	if err := svc.SaveMailAccount(ctx, tenantID, empA, "shared@sunrise.com", "", "x"); err != nil {
		t.Fatal(err)
	}
	err = svc.SaveMailAccount(ctx, tenantID, empB, "shared@sunrise.com", "", "y")
	if err == nil {
		t.Fatal("两个人绑同一个地址竟然成功了——那封信该算谁的？")
	}
	if !strings.Contains(err.Error(), "已经被") {
		t.Errorf("撞上时要说人话，拿到：%v", err)
	}
}
