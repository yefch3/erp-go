package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 钉住引导程序给超管铺数据范围这件事。
//
// 它曾经漏掉：引导只授权限（能用哪些功能），一个数据范围（能看谁的单据）
// 都没配，而解析器对未配置的模块兜底 SELF。症状是管理员打开合同、收款、
// 应收清单全是空的，权限页上却一切正常——最难自己想明白的那类问题。
//
// 这个测试的价值不在今天，在下次有人加模块的时候：忘了补
// superAdminScopeModules，这里就红。
func TestBootstrapGivesSuperAdminEveryScope(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("set IAM_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	domain := "bootstrap-scope-test.invalid"
	admin := "admin@" + domain
	// 引导只在库里一个用户都没有时才动作，所以这个测试不能跑在共享库上
	// ——除非我们只验证它写出来的东西。这里改为直接调用并在事后清理：
	// EnsureAdmin 自建租户，清理按它返回的租户走。
	var tenantID int64
	defer func() {
		if tenantID == 0 {
			return
		}
		for _, stmt := range []string{
			`DELETE FROM role_data_scopes WHERE tenant_id = $1`,
			`DELETE FROM role_permissions WHERE tenant_id = $1`,
			`DELETE FROM employee_roles WHERE tenant_id = $1`,
			`DELETE FROM roles WHERE tenant_id = $1`,
			`DELETE FROM users WHERE tenant_id = $1`,
			`DELETE FROM employees WHERE tenant_id = $1`,
			`DELETE FROM departments WHERE tenant_id = $1`,
			`DELETE FROM tenant_domains WHERE tenant_id = $1`,
			`DELETE FROM tenants WHERE id = $1`,
		} {
			_, _ = pool.Exec(ctx, stmt, tenantID)
		}
	}()

	svc := New(pool, "test-secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// HasAnyUser 是按租户问的，所以用一个必然空的租户号触发引导。
	probeTenant := time.Now().UnixNano()
	if err := svc.EnsureAdmin(ctx, probeTenant, SeedTenant{
		CompanyName: "范围引导测试", MailDomains: domain,
		AdminEmail: admin, InitialPassword: "Bootstrap-Scope-2026!",
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT tenant_id FROM employees WHERE email = $1`, admin).Scan(&tenantID); err != nil {
		t.Fatalf("引导没有建出管理员: %v", err)
	}

	// 列表里的每一个模块都该拿到 ALL。
	for _, module := range superAdminScopeModules {
		var scope string
		err := pool.QueryRow(ctx, `
			SELECT s.scope_type FROM role_data_scopes s
			JOIN roles r ON r.id = s.role_id
			WHERE s.tenant_id = $1 AND r.code = 'SUPER_ADMIN' AND s.module = $2`,
			tenantID, module).Scan(&scope)
		if err != nil {
			t.Fatalf("模块 %s 的超管范围没配上——引导又漏了: %v", module, err)
		}
		if scope != "ALL" {
			t.Fatalf("模块 %s 的超管范围应为 ALL，实际 %s", module, scope)
		}
	}

	// mail 是有意留空的：邮件正文最私密，超管要看别人的邮箱应当是一个
	// 显式的、留下记录的决定。这一条同时防止「顺手把 mail 也加上」。
	var mailScopes int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM role_data_scopes s
		JOIN roles r ON r.id = s.role_id
		WHERE s.tenant_id = $1 AND r.code = 'SUPER_ADMIN' AND s.module = 'mail'`,
		tenantID).Scan(&mailScopes); err != nil {
		t.Fatal(err)
	}
	if mailScopes != 0 {
		t.Fatal("mail 不该由引导程序给超管铺开——让人看别人的邮箱要显式决定")
	}
}
