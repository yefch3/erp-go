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

func TestEnsureSuperAdminAccessRepairsPermissionsAndScopes(t *testing.T) {
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
	domain := fmt.Sprintf("super-admin-repair-%d.invalid", stamp)
	if err := svc.EnsureExtraTenant(ctx, SeedTenant{
		CompanyName: "超管修复测试", MailDomains: domain,
		AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("Repair-%d!", stamp),
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
		"SELECT id FROM roles WHERE tenant_id=$1 AND code='SUPER_ADMIN'", tenantID).Scan(&roleID); err != nil {
		t.Fatal(err)
	}

	// Reproduce both production symptoms: a later permission is absent and a
	// business scope was narrowed even though the role still says 超级管理员.
	if _, err := pool.Exec(ctx, `DELETE FROM role_permissions
		WHERE tenant_id=$1 AND role_id=$2
		  AND permission_id=(SELECT id FROM permissions WHERE code='quality:task:read')`, tenantID, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE role_data_scopes SET scope_type='SELF'
		WHERE tenant_id=$1 AND role_id=$2 AND module='quality'`, tenantID, roleID); err != nil {
		t.Fatal(err)
	}

	if err := svc.EnsureSuperAdminAccess(ctx); err != nil {
		t.Fatal(err)
	}
	var gotPermissions, allPermissions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM role_permissions
		WHERE tenant_id=$1 AND role_id=$2`, tenantID, roleID).Scan(&gotPermissions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM permissions`).Scan(&allPermissions); err != nil {
		t.Fatal(err)
	}
	if gotPermissions != allPermissions {
		t.Fatalf("SUPER_ADMIN permissions = %d, want all %d", gotPermissions, allPermissions)
	}
	var qualityScope string
	if err := pool.QueryRow(ctx, `SELECT scope_type FROM role_data_scopes
		WHERE tenant_id=$1 AND role_id=$2 AND module='quality'`, tenantID, roleID).Scan(&qualityScope); err != nil {
		t.Fatal(err)
	}
	if qualityScope != "ALL" {
		t.Fatalf("SUPER_ADMIN quality scope = %q, want ALL", qualityScope)
	}

	if err := svc.GrantRolePermissions(ctx, tenantID, roleID, []string{}); err == nil {
		t.Fatal("SUPER_ADMIN permissions must not be editable")
	}
}
