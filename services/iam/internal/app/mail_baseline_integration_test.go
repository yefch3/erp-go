package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 邮箱属于每位在职员工自己的工作入口。这个规则不能依赖某个业务角色，
// 否则新角色或暂未分配角色的员工会看到菜单却在打开后得到 403。
func TestActiveEmployeeGetsPersonalMailWithoutRole(t *testing.T) {
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
	domain := fmt.Sprintf("mailbase%d.example", stamp)
	if err := svc.EnsureExtraTenant(ctx, SeedTenant{
		CompanyName: "邮箱基础权限", MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("M-%d!aB", stamp),
	}); err != nil {
		t.Fatal(err)
	}
	var tenantID, deptID, employeeID int64
	if err := pool.QueryRow(ctx, "SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTenant(t, pool, tenantID) })
	if err := pool.QueryRow(ctx, "SELECT id FROM departments WHERE tenant_id=$1 AND code='HQ'", tenantID).Scan(&deptID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO employees (tenant_id,code,name,department_id,email)
		VALUES ($1,'MAIL-ONLY','无业务角色员工',$2,$3) RETURNING id`, tenantID, deptID, "mail-only@"+domain).Scan(&employeeID); err != nil {
		t.Fatal(err)
	}

	for _, code := range []string{"mail:email:read", "mail:email:write"} {
		allowed, err := svc.CheckPermission(ctx, tenantID, employeeID, code)
		if err != nil || !allowed {
			t.Fatalf("active employee baseline %s = %v, %v", code, allowed, err)
		}
	}
	codes, err := svc.ListEmployeePermissions(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"mail:email:read", "mail:email:write"} {
		if !slices.Contains(codes, code) {
			t.Fatalf("login permission list missing %s: %v", code, codes)
		}
	}

	if _, err := pool.Exec(ctx, "UPDATE employees SET status='INACTIVE' WHERE tenant_id=$1 AND id=$2", tenantID, employeeID); err != nil {
		t.Fatal(err)
	}
	allowed, err := svc.CheckPermission(ctx, tenantID, employeeID, "mail:email:read")
	if err != nil || allowed {
		t.Fatalf("inactive employee mail access = %v, %v; want denied", allowed, err)
	}
}
