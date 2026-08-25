package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 开第二家公司（EnsureExtraTenant）。
//
// 钉四条：真能开出来且管理员登得进去；重复跑不重复开（幂等键是第一个域名）；
// 换个名字但域名已被认领照样不开——**不会静默改写已有公司**；管理员地址被别家
// 用了要当场拒绝，因为 00023 起地址就是全系统的身份。
func TestEnsureExtraTenantOpensASecondCompanyExactlyOnce(t *testing.T) {
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

	stamp := time.Now().UnixNano()
	domain := fmt.Sprintf("t%d.example", stamp)
	admin := "admin@" + domain
	seed := SeedTenant{
		CompanyName:     fmt.Sprintf("测试公司-%d", stamp),
		MailDomains:     domain,
		AdminEmail:      admin,
		InitialPassword: fmt.Sprintf("Vt-%d-pass", stamp),
	}

	svc := New(pool, "test-secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var tenantID int64
	defer func() {
		if tenantID == 0 {
			return
		}
		// 逆着外键的方向清。种子写过的每张表都要到场——漏一张，下一次跑这个
		// 测试的人收到的是别人的残局。
		for _, stmt := range []string{
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
	}()

	// ---- 开出来，且开得完整：管理员能直接登录 ----
	if err := svc.EnsureExtraTenant(ctx, seed); err != nil {
		t.Fatalf("开第二家公司失败：%v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatalf("域名没有被认领：%v", err)
	}
	got, err := svc.Login(ctx, admin, seed.InitialPassword)
	if err != nil {
		t.Fatalf("新公司的管理员登不进去，开户等于没开：%v", err)
	}
	if got.Employee.TenantID != tenantID {
		t.Fatalf("管理员落进了别家公司：员工档案在 %d，域名归 %d", got.Employee.TenantID, tenantID)
	}

	// ---- 幂等：原样再跑一遍，不能开出第二份 ----
	if err := svc.EnsureExtraTenant(ctx, seed); err != nil {
		t.Fatalf("重复跑该是安静的 no-op：%v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM tenants WHERE name=$1", seed.CompanyName).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("同一份配置开出了 %d 家公司", n)
	}

	// ---- 域名已被认领：换个公司名也不开，更不能改写已有的 ----
	renamed := seed
	renamed.CompanyName = seed.CompanyName + "-改名"
	if err := svc.EnsureExtraTenant(ctx, renamed); err != nil {
		t.Fatalf("域名已认领时该静默跳过：%v", err)
	}
	var name string
	if err := pool.QueryRow(ctx,
		"SELECT name FROM tenants WHERE id=$1", tenantID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != seed.CompanyName {
		t.Fatalf("已有公司被配置改写了：%q", name)
	}

	// ---- 管理员地址被占：必须当场拒绝，不能把别人的身份接进新公司 ----
	stolen := SeedTenant{
		CompanyName:     "冒名公司",
		MailDomains:     fmt.Sprintf("t%d-b.example", stamp),
		AdminEmail:      admin, // 全系统唯一的那个地址
		InitialPassword: "whatever-1",
	}
	// 域名校验要求管理员地址在本公司域名上，所以这里把被占地址的域名也带上。
	stolen.MailDomains = domain + "-b.example," + domain
	if err := svc.EnsureExtraTenant(ctx, stolen); err == nil {
		t.Fatal("管理员地址已属于别家账号，竟然开出来了")
	}
}

// 配置填一半必须把启动拦下来——这正是操作员看着日志的时刻。
func TestEnsureExtraTenantRefusesHalfAConfig(t *testing.T) {
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

	for _, seed := range []SeedTenant{
		{CompanyName: "只有名字"},
		{CompanyName: "缺管理员", MailDomains: "x.example"},
		{CompanyName: "地址不在域名上", MailDomains: "x.example", AdminEmail: "a@elsewhere.example", InitialPassword: "p"},
	} {
		if err := svc.EnsureExtraTenant(ctx, seed); err == nil {
			t.Fatalf("残缺配置 %+v 没有被拒绝", seed)
		}
	}
}
