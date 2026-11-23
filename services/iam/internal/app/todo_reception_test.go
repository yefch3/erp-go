package app

import (
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestTodoReceptionRoleDefaults(t *testing.T) {
	for _, tc := range []struct {
		role       string
		only       bool
		categories []string
	}{
		{"SALES", false, []string{"SALES_INQUIRY"}},
		{"SALES_MANAGER", false, []string{"SALES_INQUIRY"}},
		{"BUYER", false, []string{"FACTORY_INQUIRY", "EXECUTION_PROCUREMENT", "QUALITY"}},
		{"PROCUREMENT_MANAGER", false, []string{"FACTORY_INQUIRY", "EXECUTION_PROCUREMENT", "QUALITY"}},
		{"LOGISTICS", false, []string{"LOGISTICS_INQUIRY"}},
		{"SHIPPING_MANAGER", false, []string{"LOGISTICS_INQUIRY"}},
		{"QUALITY_INSPECTOR", false, []string{"QUALITY"}},
		{"BOSS", true, nil},
		{"SUPER_ADMIN", true, nil},
		{"CUSTOM_MANAGER", false, nil},
	} {
		t.Run(tc.role, func(t *testing.T) {
			got := defaultTodoReception(tc.role)
			if got.ApprovalsOnly != tc.only || !slices.Equal(got.Categories, tc.categories) {
				t.Fatalf("unexpected setting: %+v", got)
			}
		})
	}
}

func TestTodoReceptionSettingsAndPrecedence(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("IAM_TEST_DSN not set")
	}
	ctx := t.Context()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM employees WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM roles WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM departments WHERE tenant_id=$1`, tenant)
	}()
	var employee, buyer, custom, department int64
	if err := pool.QueryRow(ctx, `INSERT INTO departments (tenant_id,code,name) VALUES ($1,'T','Test') RETURNING id`, tenant).Scan(&department); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO employees (tenant_id,code,name,department_id) VALUES ($1,'T','Tester',$2) RETURNING id`, tenant, department).Scan(&employee); err != nil {
		t.Fatal(err)
	}
	for code, id := range map[string]*int64{"BUYER": &buyer, "CUSTOM_MANAGER": &custom} {
		if err := pool.QueryRow(ctx, `INSERT INTO roles (tenant_id,code,name) VALUES ($1,$2,$2) RETURNING id`, tenant, code).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO employee_roles (tenant_id,employee_id,role_id) VALUES ($1,$2,$3),($1,$2,$4)`, tenant, employee, buyer, custom); err != nil {
		t.Fatal(err)
	}
	svc := New(pool, "secret", time.Hour, slog.Default())
	check := func(only, personal bool, categories ...string) {
		t.Helper()
		setting, err := svc.GetEmployeeTodoReception(ctx, tenant, employee)
		if err != nil || setting.ApprovalsOnly != only || setting.EmployeeOverride != personal || !slices.Equal(setting.Categories, categories) {
			t.Fatalf("reception=%+v err=%v, want approvalOnly=%v employeeOverride=%v categories=%v", setting, err, only, personal, categories)
		}
	}
	check(false, false, "FACTORY_INQUIRY", "EXECUTION_PROCUREMENT", "QUALITY")
	if _, err := svc.SetRoleTodoReception(ctx, tenant, custom, TodoReception{Categories: []string{"LOGISTICS_INQUIRY"}}); err != nil {
		t.Fatal(err)
	}
	check(false, false, "FACTORY_INQUIRY", "LOGISTICS_INQUIRY", "EXECUTION_PROCUREMENT", "QUALITY")
	if _, err := svc.SetEmployeeTodoReception(ctx, tenant, employee, true); err != nil {
		t.Fatal(err)
	}
	check(true, true)
	if _, err := svc.SetEmployeeTodoReception(ctx, tenant, employee, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRoleTodoReception(ctx, tenant, custom, TodoReception{ApprovalsOnly: true}); err != nil {
		t.Fatal(err)
	}
	check(true, false)
	if _, err := pool.Exec(ctx, `UPDATE roles SET status='INACTIVE' WHERE tenant_id=$1 AND id=$2`, tenant, custom); err != nil {
		t.Fatal(err)
	}
	check(false, false, "FACTORY_INQUIRY", "EXECUTION_PROCUREMENT", "QUALITY")
	if _, err := svc.SetRoleTodoReception(ctx, tenant+1, custom, TodoReception{}); apierr.CodeFromError(err) != "IAM_ROLE_NOT_FOUND" {
		t.Fatalf("cross-tenant role write: %v", err)
	}
	if _, err := svc.SetEmployeeTodoReception(ctx, tenant+1, employee, true); apierr.CodeFromError(err) != "IAM_EMPLOYEE_NOT_FOUND" {
		t.Fatalf("cross-tenant employee write: %v", err)
	}
}
