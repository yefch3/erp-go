package app

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

func TestSupplierReadScopeUsesActiveTenantRoles(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("IAM_TEST_DSN not set")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE employees(tenant_id bigint,id bigint,status text);
		CREATE TEMP TABLE roles(tenant_id bigint,id bigint,code text,status text);
		CREATE TEMP TABLE employee_roles(tenant_id bigint,employee_id bigint,role_id bigint);
		CREATE TEMP TABLE role_data_scopes(tenant_id bigint,role_id bigint,module text,scope_type text,custom_dept_ids bigint[]);
		CREATE TEMP TABLE permissions(id bigint,code text);
		CREATE TEMP TABLE role_permissions(tenant_id bigint,role_id bigint,permission_id bigint);
		INSERT INTO employees VALUES (1,1,'ACTIVE'),(1,2,'ACTIVE'),(1,3,'ACTIVE'),(1,4,'ACTIVE'),(2,1,'ACTIVE');
		INSERT INTO roles VALUES
			(1,1,'SUPPLIER_READER','ACTIVE'),(1,2,'COMPANY_SCOPE','ACTIVE'),(1,3,'SUPER_ADMIN','ACTIVE'),
			(2,1,'SUPPLIER_READER','ACTIVE'),(2,2,'COMPANY_SCOPE','ACTIVE');
		INSERT INTO employee_roles VALUES (1,1,1),(1,1,2),(1,2,1),(1,3,2),(1,4,3),(2,1,1),(2,1,2);
		INSERT INTO role_data_scopes VALUES (1,1,'supplier_read','SELF','{}'),(1,2,'supplier_read','ALL','{}');
		INSERT INTO permissions VALUES (1,'masterdata:supplier:read');
		INSERT INTO role_permissions VALUES (1,1,1),(2,1,1);`)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{pool: pool, q: store.New(pool)}
	check := func(tenant, employee int64, module string, all bool) {
		t.Helper()
		want := "SELF"
		if all {
			want = "ALL"
		}
		got, err := svc.VisibleEmployees(ctx, tenant, employee, module)
		if err != nil || got.All != all || got.ScopeType != want {
			t.Fatalf("tenant=%d employee=%d module=%s: got %+v, %v, want %s", tenant, employee, module, got, err, want)
		}
	}
	check(1, 1, supplierReadScope, true)
	check(1, 1, "supplier", false)
	check(1, 2, supplierReadScope, false)
	check(1, 3, supplierReadScope, false)
	check(1, 4, supplierReadScope, true)
	check(2, 1, supplierReadScope, false)

	if _, err := pool.Exec(ctx, "UPDATE roles SET status='INACTIVE' WHERE tenant_id=1 AND id=2"); err != nil {
		t.Fatal(err)
	}
	check(1, 1, supplierReadScope, false)
}
