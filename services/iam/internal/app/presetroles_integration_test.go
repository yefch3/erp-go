package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 新公司开张就该有第一家公司开箱即有的四个预置角色。
//
// 这组测试里最重要的是**对表测试**：把新公司每个预置角色的权限码和数据范围，
// 与迁移播给 1 号公司的同名角色逐条对比——presetroles.go 那张 Go 表和十几个
// 迁移的一致性不靠注释请求人同改，靠这条测试当场变红。
//
// 对比必须在**专用的一次性迁移库**上做（IAM_MIGRATION_TEST_DSN），不能用
// 共享测试库。第一版用了共享库，CI 当场揭穿：共享库里哪个测试先建公司谁就
// 拿到 1 号，清理时把迁移播给 1 号的角色一起删了——基准被污染，本地却因为
// 服务引导过 1 号而一直是绿的。一次性库里 1 号就是迁移的纯产物，没有别人。
func TestPresetRolesMatchWhatMigrationsGaveTheFirstTenant(t *testing.T) {
	dsn := os.Getenv("IAM_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set IAM_MIGRATION_TEST_DSN to a disposable PostgreSQL database")
	}
	ctx := context.Background()

	// 把一次性库刷到最新——它平时没人碰，第一次跑要从零建起。
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(sqlDB, "../../db/migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// 序列拨远：这个库里 1 号必须永远是迁移播种的那家，新建的公司不许撞上。
	// 取现有最大号 +100 而不是固定值，本地反复跑不回拨序列。
	if _, err := sqlDB.Exec(
		`SELECT setval('tenants_id_seq', (SELECT COALESCE(MAX(id),0)+100 FROM tenants))`); err != nil {
		t.Fatal(err)
	}

	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	svc := New(pool, "test-secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	stamp := time.Now().UnixNano()
	domain := fmt.Sprintf("preset%d.example", stamp)
	if err := svc.EnsureExtraTenant(ctx, SeedTenant{
		CompanyName: fmt.Sprintf("预置角色-%d", stamp), MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("P-%d!", stamp),
	}); err != nil {
		t.Fatal(err)
	}
	var tenantID int64
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTenant(t, pool, tenantID) })

	permsOf := func(tid int64, code string) []string {
		rows, err := pool.Query(ctx, `SELECT p.code
			FROM roles r
			JOIN role_permissions rp ON rp.role_id = r.id AND rp.tenant_id = r.tenant_id
			JOIN permissions p ON p.id = rp.permission_id
			WHERE r.tenant_id = $1 AND r.code = $2 ORDER BY p.code`, tid, code)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
		return out
	}
	scopesOf := func(tid int64, code string) []string {
		rows, err := pool.Query(ctx, `SELECT s.module || '=' || s.scope_type
			FROM roles r
			JOIN role_data_scopes s ON s.role_id = r.id AND s.tenant_id = r.tenant_id
			WHERE r.tenant_id = $1 AND r.code = $2`, tid, code)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
		sort.Strings(out)
		return out
	}

	for _, pr := range presetRoles {
		wantPerms, gotPerms := permsOf(1, pr.Code), permsOf(tenantID, pr.Code)
		if len(wantPerms) == 0 {
			t.Fatalf("测试库的第一家公司没有 %s 角色——迁移变了？", pr.Code)
		}
		if fmt.Sprint(wantPerms) != fmt.Sprint(gotPerms) {
			t.Fatalf("%s 的权限和迁移给第一家的对不上：\n迁移: %v\n播种: %v\n"+
				"（多半是有人加了权限迁移而 presetroles.go 没跟上，或反之）",
				pr.Code, wantPerms, gotPerms)
		}
		wantScopes, gotScopes := scopesOf(1, pr.Code), scopesOf(tenantID, pr.Code)
		if fmt.Sprint(wantScopes) != fmt.Sprint(gotScopes) {
			t.Fatalf("%s 的数据范围和迁移给第一家的对不上：\n迁移: %v\n播种: %v",
				pr.Code, wantScopes, gotScopes)
		}
	}

	// 播出来的是空角色：谁担任采购经理是这家公司自己的决定。
	var members int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM employee_roles er
		JOIN roles r ON r.id = er.role_id AND r.tenant_id = er.tenant_id
		WHERE er.tenant_id = $1 AND r.code <> 'SUPER_ADMIN'`, tenantID).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 0 {
		t.Fatalf("预置角色不该自带成员，实际 %d 个", members)
	}
}

// 启动扫描给此改动之前开出的公司补齐；人建过或改过的角色一个字都不碰。
func TestEnsurePresetRolesHealsOldTenantsWithoutTouchingEditedOnes(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("set IAM_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	svc := New(pool, "test-secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	stamp := time.Now().UnixNano()
	domain := fmt.Sprintf("heal%d.example", stamp)
	if err := svc.EnsureExtraTenant(ctx, SeedTenant{
		CompanyName: fmt.Sprintf("补种-%d", stamp), MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("H-%d!", stamp),
	}); err != nil {
		t.Fatal(err)
	}
	var tenantID int64
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTenant(t, pool, tenantID) })

	// 模拟「此改动之前开出的公司」：把播出来的预置角色删掉（只有测试能删，
	// 线上没有删角色的入口），只动 BUYER 留下三个，检验补的是缺的那一个。
	if _, err := pool.Exec(ctx, `DELETE FROM role_data_scopes WHERE tenant_id=$1
		AND role_id IN (SELECT id FROM roles WHERE tenant_id=$1 AND code='BUYER')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM role_permissions WHERE tenant_id=$1
		AND role_id IN (SELECT id FROM roles WHERE tenant_id=$1 AND code='BUYER')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM roles WHERE tenant_id=$1 AND code='BUYER'`, tenantID); err != nil {
		t.Fatal(err)
	}
	// 同时把 FINANCE 的权限改小，扮演「人改过的角色」。
	if _, err := pool.Exec(ctx, `DELETE FROM role_permissions WHERE tenant_id=$1
		AND role_id IN (SELECT id FROM roles WHERE tenant_id=$1 AND code='FINANCE')`, tenantID); err != nil {
		t.Fatal(err)
	}

	if err := svc.EnsurePresetRoles(ctx); err != nil {
		t.Fatal(err)
	}

	var buyerPerms, financePerms int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions rp
		JOIN roles r ON r.id = rp.role_id AND r.tenant_id = rp.tenant_id
		WHERE rp.tenant_id=$1 AND r.code='BUYER'`, tenantID).Scan(&buyerPerms); err != nil {
		t.Fatal(err)
	}
	if buyerPerms == 0 {
		t.Fatal("缺失的 BUYER 没被补回来")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions rp
		JOIN roles r ON r.id = rp.role_id AND r.tenant_id = rp.tenant_id
		WHERE rp.tenant_id=$1 AND r.code='FINANCE'`, tenantID).Scan(&financePerms); err != nil {
		t.Fatal(err)
	}
	if financePerms != 0 {
		t.Fatalf("人改过的 FINANCE 被补种还原了（%d 条权限）——默认值只填空，不还原", financePerms)
	}
}

// cleanupTenant 按外键顺序清掉一家测试公司。
func cleanupTenant(t *testing.T, pool *pgxpool.Pool, tenantID int64) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		"DELETE FROM employee_invitations WHERE tenant_id=$1",
		"DELETE FROM role_data_scopes WHERE tenant_id=$1",
		"DELETE FROM employee_roles WHERE tenant_id=$1",
		"DELETE FROM role_permissions WHERE tenant_id=$1",
		"DELETE FROM roles WHERE tenant_id=$1",
		"DELETE FROM users WHERE tenant_id=$1",
		"DELETE FROM employees WHERE tenant_id=$1",
		"DELETE FROM departments WHERE tenant_id=$1",
		"DELETE FROM tenant_domains WHERE tenant_id=$1",
		"DELETE FROM tenants WHERE id=$1",
	} {
		if _, err := pool.Exec(ctx, stmt, tenantID); err != nil {
			t.Errorf("cleanup %q: %v", stmt, err)
		}
	}
}
