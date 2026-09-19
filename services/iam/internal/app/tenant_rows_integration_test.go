package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 角色的权限行、范围行、员工挂角色的行，公司号必须和角色一致——由数据库
// 兜底（迁移 00081 的复合外键），不靠谁记得写 tenant_id。
//
// 2026-09-19 查出来的事故：迁移 00055 往 role_permissions 插行时列清单里没写
// tenant_id，数据库按默认值填成 1，三家公司的销售因此少了两条权限，而且从
// 角色页面上看不出来。这条测试钉的是"再这么写，库会拒"。
func TestRoleRowsCannotBeFiledUnderAnotherTenant(t *testing.T) {
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

	stamp := time.Now().UnixNano()
	code := fmt.Sprintf("ZZ_TENANT_ROWS_%d", stamp%1_000_000_000)
	var roleID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO roles (tenant_id, code, name) VALUES (1, $1, '公司号测试') RETURNING id`, code,
	).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM roles WHERE id = $1`, roleID)
	})
	var permID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM permissions ORDER BY id LIMIT 1`).Scan(&permID); err != nil {
		t.Fatal(err)
	}

	// 角色在公司 1，行却记到公司 2：三张表都得拒，而且是外键那一类的拒。
	refused := func(name, sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Errorf("%s：公司号和角色对不上的行应该被外键拒掉，实际 %v", name, err)
		}
	}
	refused("role_permissions",
		`INSERT INTO role_permissions (tenant_id, role_id, permission_id) VALUES (2, $1, $2)`, roleID, permID)
	refused("role_data_scopes",
		`INSERT INTO role_data_scopes (tenant_id, role_id, module, scope_type) VALUES (2, $1, 'export', 'ALL')`, roleID)
	var empID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM employees WHERE tenant_id = 1 ORDER BY id LIMIT 1`).Scan(&empID); err != nil {
		t.Skip("这个库里公司 1 没有员工，跳过挂角色那一项")
	}
	refused("employee_roles",
		`INSERT INTO employee_roles (tenant_id, employee_id, role_id) VALUES (2, $1, $2)`, empID, roleID)

	// 00055 那种写法——列清单里没有 tenant_id——对公司 1 的角色恰好能写进去
	// （默认值就是 1），所以守卫在 scripts/check-iam-seeds.sh；这里只证明
	// 对的写法照常能写。
	if _, err := pool.Exec(ctx,
		`INSERT INTO role_permissions (tenant_id, role_id, permission_id) VALUES (1, $1, $2)`, roleID, permID,
	); err != nil {
		t.Fatalf("公司号对得上的行应该能写：%v", err)
	}
}
