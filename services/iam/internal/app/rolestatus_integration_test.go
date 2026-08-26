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

// 停用角色必须**真的**收回权限。
//
// status 列一直在，但没有任何查询读它——停用只是把角色从列表里藏起来，持有
// 人的权限、数据范围一个不少。一个说「停用」却什么都没拿走的按钮，比没有按钮
// 更坏：管理员以为权限收回了，就不再去看。这组测试钉住四条查询都尊重角色状态。
func TestDeactivatingARoleActuallyRevokes(t *testing.T) {
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
	domain := fmt.Sprintf("rolestat%d.example", stamp)
	if err := svc.EnsureExtraTenant(ctx, SeedTenant{
		CompanyName: fmt.Sprintf("角色停用-%d", stamp), MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("R-%d!aB", stamp),
	}); err != nil {
		t.Fatal(err)
	}
	var tenantID int64
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTenant(t, pool, tenantID) })

	// 一个普通员工，只持有预置的「采购专员」角色。
	var deptID, buyerRoleID, empID int64
	if err := pool.QueryRow(ctx,
		"SELECT id FROM departments WHERE tenant_id=$1 AND code='HQ'", tenantID).Scan(&deptID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT id FROM roles WHERE tenant_id=$1 AND code='BUYER'", tenantID).Scan(&buyerRoleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO employees (tenant_id, code, name, department_id, email)
		VALUES ($1,'E1','小采',$2,$3) RETURNING id`,
		tenantID, deptID, fmt.Sprintf("buyer%d@%s", stamp, domain)).Scan(&empID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO employee_roles (tenant_id, employee_id, role_id) VALUES ($1,$2,$3)",
		tenantID, empID, buyerRoleID); err != nil {
		t.Fatal(err)
	}

	const probe = "procurement:sourcing:read" // 采购专员持有的一项
	mustAllow := func(want bool, when string) {
		t.Helper()
		ok, err := svc.CheckPermission(ctx, tenantID, empID, probe)
		if err != nil {
			t.Fatal(err)
		}
		if ok != want {
			t.Fatalf("%s：权限判定应为 %v，实际 %v", when, want, ok)
		}
		codes, err := svc.ListEmployeePermissions(ctx, tenantID, empID)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, c := range codes {
			if c == probe {
				has = true
			}
		}
		if has != want {
			t.Fatalf("%s：菜单用的权限清单应为 %v，实际 %v——两处口径必须一致，"+
				"否则菜单亮着、点进去 403", when, want, has)
		}
	}

	mustAllow(true, "停用前")

	// 有人持有时拒绝停用，并把人数说出来。
	err = svc.SetRoleStatus(ctx, tenantID, buyerRoleID, "INACTIVE")
	if err == nil {
		t.Fatal("角色还有人持有就被停用了——那些人会在下一次点击时无声地撞上没有权限")
	}
	if !strings.Contains(err.Error(), "1 位") {
		t.Fatalf("拒绝时要说出还有几个人持有，人才知道下一步做什么：%v", err)
	}
	mustAllow(true, "拒绝停用之后")

	// 改派之后再停用：这次成功，且权限当场消失。
	if _, err := pool.Exec(ctx,
		"DELETE FROM employee_roles WHERE tenant_id=$1 AND employee_id=$2", tenantID, empID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRoleStatus(ctx, tenantID, buyerRoleID, "INACTIVE"); err != nil {
		t.Fatalf("没人持有了，停用该成功：%v", err)
	}
	// 把人再放回停用的角色里：权限**不该**因此回来。
	if _, err := pool.Exec(ctx,
		"INSERT INTO employee_roles (tenant_id, employee_id, role_id) VALUES ($1,$2,$3)",
		tenantID, empID, buyerRoleID); err != nil {
		t.Fatal(err)
	}
	mustAllow(false, "停用之后")

	// 停用的角色不再供出成员——审批流指着它时会明确报「没有可用审批人」，
	// 而不是把任务派给一个公司已经废弃的角色。
	members, err := svc.ListRoleMembers(ctx, tenantID, buyerRoleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 0 {
		t.Fatalf("停用的角色仍然供出了 %d 个成员", len(members))
	}

	// 启用回来：权限恢复。停用是可撤销的决定，不是删除。
	if err := svc.SetRoleStatus(ctx, tenantID, buyerRoleID, "ACTIVE"); err != nil {
		t.Fatal(err)
	}
	mustAllow(true, "重新启用之后")
}

// 超管角色停不得：停掉之后没有人能把它启用回来。
func TestSuperAdminRoleCannotBeDeactivated(t *testing.T) {
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
	domain := fmt.Sprintf("salock%d.example", stamp)
	if err := svc.EnsureExtraTenant(ctx, SeedTenant{
		CompanyName: fmt.Sprintf("超管锁-%d", stamp), MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("S-%d!aB", stamp),
	}); err != nil {
		t.Fatal(err)
	}
	var tenantID, saID int64
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTenant(t, pool, tenantID) })
	if err := pool.QueryRow(ctx,
		"SELECT id FROM roles WHERE tenant_id=$1 AND code='SUPER_ADMIN'", tenantID).Scan(&saID); err != nil {
		t.Fatal(err)
	}
	// 先把持有人摘掉，排除「有人持有」那道闸，确保拦住它的是超管这条规则本身。
	if _, err := pool.Exec(ctx,
		"DELETE FROM employee_roles WHERE tenant_id=$1 AND role_id=$2", tenantID, saID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRoleStatus(ctx, tenantID, saID, "INACTIVE"); err == nil {
		t.Fatal("超管角色被停用了——没有人能把它启用回来，钥匙反锁进屋")
	}
}

// 停用的角色不会被开机补种复活：那是有人做过的决定。
func TestDeactivatedPresetRoleIsNotResurrectedBySeeding(t *testing.T) {
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
	domain := fmt.Sprintf("noresurrect%d.example", stamp)
	if err := svc.EnsureExtraTenant(ctx, SeedTenant{
		CompanyName: fmt.Sprintf("不复活-%d", stamp), MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("N-%d!aB", stamp),
	}); err != nil {
		t.Fatal(err)
	}
	var tenantID, roleID int64
	if err := pool.QueryRow(ctx,
		"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTenant(t, pool, tenantID) })
	if err := pool.QueryRow(ctx,
		"SELECT id FROM roles WHERE tenant_id=$1 AND code='LOGISTICS'", tenantID).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"DELETE FROM employee_roles WHERE tenant_id=$1 AND role_id=$2", tenantID, roleID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRoleStatus(ctx, tenantID, roleID, "INACTIVE"); err != nil {
		t.Fatal(err)
	}

	if err := svc.EnsurePresetRoles(ctx); err != nil {
		t.Fatal(err)
	}

	var status string
	if err := pool.QueryRow(ctx,
		"SELECT status FROM roles WHERE tenant_id=$1 AND code='LOGISTICS'", tenantID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "INACTIVE" {
		t.Fatalf("停用的预置角色被开机补种复活了（现在是 %s）——那是有人做过的决定", status)
	}
}
