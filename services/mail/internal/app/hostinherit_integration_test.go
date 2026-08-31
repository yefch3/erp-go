package app

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 新绑的信箱，绑完就能收发。
//
// 这条测试是补一个已经发到线上的洞。00042 把收发服务器从「一家公司一份」
// （mail_hosts）搬到了信箱行上，但插入新信箱那一句 UpsertMailAccountShell
// 没跟着改——新行的 smtp_host 是空串，而 ForAccount 见到空串就返回
// ErrMailHostNotConfigured。
//
// 症状：**新员工绑完邮箱，发信和 IMAP 同步全停**，一直停到管理员碰巧再去
// 「邮件主机设置」里点一次保存（那一下会触发 SyncAccountHostsFromTenant 把
// 租户配置刷到所有行）。两件事之间没有任何提示把它们联系起来，而绑定那一步
// 是成功的——人只会觉得"绑了但收不到信"。
//
// 之所以之前没人发现：00042 的回填给**已有的**行补齐了主机，而库里当时的
// 行全是已有的；多信箱的集成测试又是手工 UPDATE 补的主机
// （multimailbox_integration_test.go:57），恰好把这条路绕开了。
func TestNewlyBoundMailboxInheritsTheCompanyHost(t *testing.T) {
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
	employeeID := tenantID%100000 + 990001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_hosts WHERE tenant_id=$1", tenantID)
	}()

	// 公司已经配好了收发服务器——管理员在「邮件主机设置」里填过。
	if _, err := pool.Exec(ctx, `INSERT INTO mail_hosts
		(tenant_id, domain, smtp_host, smtp_port, smtp_security,
		 imap_host, imap_port, imap_security, hourly_quota, daily_quota)
		VALUES ($1,'sunrise.com','smtp.263.net',465,'SSL','imap.263.net',993,'SSL',80,400)`,
		tenantID); err != nil {
		t.Fatal(err)
	}

	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := svc.SaveMailAccount(ctx, tenantID, employeeID, "newbie@sunrise.com", "", "code"); err != nil {
		t.Fatal(err)
	}
	id, err := svc.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	acct, err := svc.ForAccount(ctx, tenantID, id)
	if errors.Is(err, ErrMailHostNotConfigured) {
		t.Fatal("新绑的信箱拿不到收发服务器——发信和同步全停，而公司明明配好了。" +
			"插入新信箱那一句没有把 mail_hosts 的模板种进去")
	}
	if err != nil {
		t.Fatal(err)
	}
	if acct.Host != "smtp.263.net" || acct.IMAPHost != "imap.263.net" {
		t.Errorf("收发服务器是 %s / %s，应该继承公司配置", acct.Host, acct.IMAPHost)
	}
	// 配额也一起继承。它和主机同一批搬过来（00042），漏了的话新信箱按
	// 100/500 发，而公司在服务商那边的额度可能只有 80/400——超出去是整个
	// 域名被限流，受影响的不止这一个人。
	var hourly, daily int32
	if err := pool.QueryRow(ctx, `SELECT hourly_quota, daily_quota FROM mail_accounts
		WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&hourly, &daily); err != nil {
		t.Fatal(err)
	}
	if hourly != 80 || daily != 400 {
		t.Errorf("配额是 %d/%d，应该继承公司配置的 80/400", hourly, daily)
	}

	// 第二个信箱走同一句 SQL（Google 回调也走它），一样要种上。
	second, err := svc.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
		TenantID: tenantID, EmployeeID: employeeID, Email: "newbie@gmail.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	var smtpHost string
	if err := pool.QueryRow(ctx, `SELECT smtp_host FROM mail_accounts
		WHERE tenant_id=$1 AND id=$2`, tenantID, second).Scan(&smtpHost); err != nil {
		t.Fatal(err)
	}
	if smtpHost == "" {
		t.Error("第二个信箱的收发服务器是空的——Google 回调也走这一句，" +
			"绑完一样是个不能收发的箱")
	}
}

// 没配过 mail_hosts 的公司，绑定本身要能成功。
//
// 种模板用的是 LEFT JOIN 而不是 JOIN，就是为了这个：改成 JOIN 的话，
// 一家新开的公司连插都插不进去，而报出来的会是「绑定失败」这种查不到原因
// 的话——真正该说的是「还没配收发服务器」，那句 ForAccount 已经会说了。
func TestBindingWorksBeforeAnybodyConfiguredTheCompanyHost(t *testing.T) {
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
	employeeID := tenantID%100000 + 991001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	svc := New(pool, Deps{Secrets: box}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// mail_hosts 里一行都没有。
	if err := svc.SaveMailAccount(ctx, tenantID, employeeID, "first@newco.com", "", "code"); err != nil {
		t.Fatalf("公司还没配收发服务器，绑定就应该失败？%v", err)
	}
	id, err := svc.defaultAccountIDFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ForAccount(ctx, tenantID, id); !errors.Is(err, ErrMailHostNotConfigured) {
		t.Errorf("这个信箱确实还不能收发，该说的是「没配收发服务器」，拿到：%v", err)
	}
}
