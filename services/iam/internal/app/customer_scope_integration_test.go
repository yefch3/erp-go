package app

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

func TestCustomerScopeTenantRoleIsolation(t *testing.T) {
	dsn := os.Getenv("CUSTOMER_SCOPE_TEST_DSN")
	if dsn == "" {
		t.Skip("CUSTOMER_SCOPE_TEST_DSN not set")
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
	// Session-local fixtures shadow no business tables and vanish on disconnect.
	_, err = pool.Exec(ctx, `CREATE TEMP TABLE employees(tenant_id bigint,id bigint,status text);
        CREATE TEMP TABLE roles(tenant_id bigint,id bigint,code text,status text);
        CREATE TEMP TABLE employee_roles(tenant_id bigint,employee_id bigint,role_id bigint);
        INSERT INTO employees VALUES(1,1,'ACTIVE'),(2,1,'ACTIVE'),(1,99,'ACTIVE');
        INSERT INTO roles VALUES(1,1,'SALES','ACTIVE'),(2,1,'SUPER_ADMIN','ACTIVE'),(1,2,'SUPER_ADMIN','ACTIVE');
        INSERT INTO employee_roles VALUES(1,1,1),(2,1,1),(1,99,2);`)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{pool: pool}
	for _, tt := range []struct {
		tenant, actor int64
		all           bool
	}{{1, 1, false}, {2, 1, true}, {1, 99, true}, {2, 99, false}, {3, 1, false}} {
		got, err := svc.VisibleEmployees(ctx, tt.tenant, tt.actor, "customer")
		if err != nil || got.All != tt.all {
			t.Fatalf("tenant=%d actor=%d: %+v %v", tt.tenant, tt.actor, got, err)
		}
		if !got.All && (len(got.EmployeeIDs) != 1 || got.EmployeeIDs[0] != tt.actor) {
			t.Fatal("ordinary customer scope is not SELF")
		}
	}
	if _, err = pool.Exec(ctx, "UPDATE roles SET status='INACTIVE' WHERE tenant_id=2"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.VisibleEmployees(ctx, 2, 1, "customer")
	if err != nil || got.All {
		t.Fatal("inactive admin role retained access")
	}
	if _, err = pool.Exec(ctx, "UPDATE employees SET status='INACTIVE' WHERE tenant_id=1 AND id=99"); err != nil {
		t.Fatal(err)
	}
	got, err = svc.VisibleEmployees(ctx, 1, 99, "customer")
	if err != nil || got.All {
		t.Fatal("inactive employee retained access")
	}
}
