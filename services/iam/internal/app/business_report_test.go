package app

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

func TestBusinessReportPermissionRequiresActiveBossInTenant(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("IAM_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
CREATE TEMP TABLE permissions(id bigint,code text);
CREATE TEMP TABLE employees(tenant_id bigint,id bigint,status text);
CREATE TEMP TABLE roles(tenant_id bigint,id bigint,code text,status text);
CREATE TEMP TABLE employee_roles(tenant_id bigint,employee_id bigint,role_id bigint);
CREATE TEMP TABLE role_permissions(tenant_id bigint,role_id bigint,permission_id bigint);
INSERT INTO permissions VALUES (1,'report:business:read');
INSERT INTO employees VALUES (1,7,'ACTIVE'),(1,8,'ACTIVE'),(1,9,'ACTIVE'),(2,7,'ACTIVE'),(1,10,'INACTIVE');
INSERT INTO roles VALUES (1,1,'BOSS','ACTIVE'),(1,2,'SALES','ACTIVE'),(1,3,'SUPER_ADMIN','ACTIVE'),(1,4,'BOSS','INACTIVE'),(2,1,'SALES','ACTIVE');
INSERT INTO employee_roles VALUES (1,7,1),(1,8,2),(1,9,3),(2,7,1),(1,10,1),(1,8,4);
INSERT INTO role_permissions VALUES (1,1,1),(1,2,1),(1,3,1),(1,4,1),(2,1,1);`)
	if err != nil {
		t.Fatal(err)
	}
	query := store.New(tx)
	for _, actor := range []struct {
		tenant, id int64
		allowed    bool
	}{
		{1, 7, true}, {1, 8, false}, {1, 9, false}, {2, 7, false}, {1, 10, false},
	} {
		allowed, err := query.EmployeeHasPermission(ctx, store.EmployeeHasPermissionParams{TenantID: actor.tenant, EmployeeID: actor.id, Code: businessReportPermission})
		if err != nil || allowed != actor.allowed {
			t.Fatalf("access for %+v: %v, %v", actor, allowed, err)
		}
		codes, err := query.ListEmployeePermissionCodes(ctx, store.ListEmployeePermissionCodesParams{TenantID: actor.tenant, EmployeeID: actor.id})
		if err != nil || slices.Contains(codes, businessReportPermission) != actor.allowed {
			t.Fatalf("menu permission for %+v: %v, %v", actor, codes, err)
		}
	}
}
