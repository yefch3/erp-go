package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 平台开户（E7）的整条链：名单守门 → 开公司 → 邀请 → 激活 → 登录 → 停用挡登录。
//
// 与配置路径的分歧点是身份证明：这条路上管理员的验证章由**激活点击**挣来，
// 开出来的公司**没有域名**（域名检查防的是员工邀请手滑，不是开户），密码由
// 本人在激活页设置。测试跑真库，把每一环都走到登录为止——「开出来了」的
// 定义是能登进去，不是行存在。
func TestPlatformOnboardingEndToEnd(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("set IAM_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, "test-secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	stamp := time.Now().UnixNano()
	var cleanupTenants []int64
	defer func() {
		for _, id := range cleanupTenants {
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
				if _, err := pool.Exec(ctx, stmt, id); err != nil {
					t.Errorf("cleanup %q: %v", stmt, err)
				}
			}
		}
	}()

	// 操作员：用配置路径开一家「平台自己的公司」，把它的管理员写进名单。
	opDomain := fmt.Sprintf("op%d.example", stamp)
	opSeed := SeedTenant{
		CompanyName: fmt.Sprintf("平台方-%d", stamp), MailDomains: opDomain,
		AdminEmail: "admin@" + opDomain, InitialPassword: fmt.Sprintf("Op-%d!", stamp),
	}
	if err := svc.EnsureExtraTenant(ctx, opSeed); err != nil {
		t.Fatal(err)
	}
	var opTenant, opEmployee int64
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", opDomain).Scan(&opTenant); err != nil {
		t.Fatal(err)
	}
	cleanupTenants = append(cleanupTenants, opTenant)
	if err := pool.QueryRow(ctx,
		"SELECT id FROM employees WHERE tenant_id=$1 AND code='ADMIN'", opTenant).Scan(&opEmployee); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO platform_operators (employee_id, note) VALUES ($1,'test')", opEmployee); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM platform_operators WHERE employee_id=$1", opEmployee)
	}()

	// ---- 名单守门：不在名单上的人，第一步就被拒 ----
	if _, _, err := svc.PlatformCreateTenant(ctx, opEmployee+999999, "冒名", "a@b.example"); err == nil {
		t.Fatal("不在名单上的人开出了公司——平台守卫是空的")
	}

	// ---- 开公司：用公共邮箱地址（263 场景），公司应无域名 ----
	adminAddr := fmt.Sprintf("cust%d@263.net", stamp)
	ten, inv, err := svc.PlatformCreateTenant(ctx, opEmployee, "客户公司甲", adminAddr)
	if err != nil {
		t.Fatalf("开户失败：%v", err)
	}
	cleanupTenants = append(cleanupTenants, ten.ID)
	var domainCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM tenant_domains WHERE tenant_id=$1", ten.ID).Scan(&domainCount); err != nil {
		t.Fatal(err)
	}
	if domainCount != 0 {
		t.Fatalf("平台开的公司不该认领任何域名，实际认领了 %d 个——公共邮箱的别家公司会被挡住", domainCount)
	}
	if inv.Token == "" || inv.Email != adminAddr {
		t.Fatalf("邀请没铸对：%+v", inv)
	}

	// ---- 激活前登不进（没有密码可言），激活后登得进 ----
	if _, err := svc.Login(ctx, adminAddr, "anything"); err == nil {
		t.Fatal("还没激活就能登录——密码从哪来的？")
	}
	pw := fmt.Sprintf("Cust-%d!pass", stamp)
	if _, err := svc.ActivateAccount(ctx, inv.Token, pw); err != nil {
		t.Fatalf("激活失败：%v", err)
	}
	got, err := svc.Login(ctx, adminAddr, pw)
	if err != nil {
		t.Fatalf("激活后登不进去，开户等于没开：%v", err)
	}
	if got.Employee.TenantID != ten.ID {
		t.Fatalf("管理员落错了公司：%d != %d", got.Employee.TenantID, ten.ID)
	}

	// ---- 同一个地址开第二家：全系统身份唯一，必须拒绝且说人话 ----
	if _, _, err := svc.PlatformCreateTenant(ctx, opEmployee, "客户公司乙", adminAddr); err == nil {
		t.Fatal("同一个邮箱开出了第二家公司")
	} else if !strings.Contains(err.Error(), "已属于") {
		t.Fatalf("撞地址是开户页最常见的用户错误，要说人话，实际：%v", err)
	}

	// ---- 重发邀请：已激活的管理员该被拒（那是改密码，不是邀请） ----
	if _, err := svc.PlatformReinvite(ctx, opEmployee, ten.ID); err == nil {
		t.Fatal("给已激活的管理员重发了邀请——那是披着邀请外衣的改密码")
	}

	// ---- 停用挡登录，恢复放行 ----
	if err := svc.PlatformSetTenantStatus(ctx, opEmployee, opTenant, ten.ID, "SUSPENDED"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, adminAddr, pw); err == nil {
		t.Fatal("公司停用了还能登录")
	}
	if err := svc.PlatformSetTenantStatus(ctx, opEmployee, opTenant, ten.ID, "ACTIVE"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, adminAddr, pw); err != nil {
		t.Fatalf("恢复后该能登录：%v", err)
	}

	// ---- 操作员不能停用自己所在的公司（钥匙别反锁进屋） ----
	if err := svc.PlatformSetTenantStatus(ctx, opEmployee, opTenant, opTenant, "SUSPENDED"); err == nil {
		t.Fatal("操作员把自己所在的公司停用了——解锁入口正是被锁的页面")
	}
}

// 有域名的公司，员工邀请照旧守门；没域名的公司不设这道门。
//
// 这是 E7 的设计修正落到代码后的形状：门只在「有网可防」时立起来。
func TestInviteDomainGateOnlyWhenDomainsExist(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("set IAM_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, "test-secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	stamp := time.Now().UnixNano()
	domain := fmt.Sprintf("gated%d.example", stamp)
	seed := SeedTenant{
		CompanyName: fmt.Sprintf("有域名的公司-%d", stamp), MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: "G-1!pass",
	}
	if err := svc.EnsureExtraTenant(ctx, seed); err != nil {
		t.Fatal(err)
	}
	var tenantID int64
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	defer func() {
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
				t.Errorf("cleanup: %v", err)
			}
		}
	}()

	// 建一个邮箱在别家域名上的员工：有域名的公司必须拒绝邀请他。
	var deptID int64
	if err := pool.QueryRow(ctx,
		"SELECT id FROM departments WHERE tenant_id=$1 AND code='HQ'", tenantID).Scan(&deptID); err != nil {
		t.Fatal(err)
	}
	var outsider int64
	if err := pool.QueryRow(ctx, `INSERT INTO employees
		(tenant_id, code, name, department_id, email)
		VALUES ($1,'E1','外域员工',$2,$3) RETURNING id`,
		tenantID, deptID, fmt.Sprintf("out%d@qq.com", stamp)).Scan(&outsider); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InviteEmployee(ctx, tenantID, outsider, 0); err == nil {
		t.Fatal("有域名的公司给外域地址发出了邀请——防手滑的网被拆了")
	}
}
