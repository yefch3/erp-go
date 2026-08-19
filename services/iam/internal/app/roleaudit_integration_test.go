package app

import (
	"testing"
)

// 保存角色权限必须真的落库。
//
// 这个 bug 只有数据库能证明：代码本身没有任何问题，坏的是 directory_change_logs
// 的 CHECK 约束不认识 'ROLE'。任何把仓储 mock 掉的测试都会通过，而生产上每一次
// 保存权限都是 500。所以这里连真实数据库。
//
// 运行：IAM_TEST_DSN=postgres://erp_iam:...@127.0.0.1:5433/erp_iam go test ./services/iam/...
func TestGrantRolePermissionsPersistsAndAudits(t *testing.T) {
	pool, ctx := loginTestPool(t)
	tenantID := seedCompany(t, ctx, pool, "角色审计公司", "roleaudit@example.com", "pw-for-test-only")
	svc := loginService(pool)

	var roleID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO roles (tenant_id, code, name) VALUES ($1, 'TESTER', '测试角色') RETURNING id`,
		tenantID).Scan(&roleID); err != nil {
		t.Fatalf("播种角色: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM role_permissions WHERE tenant_id = $1`, tenantID); err != nil {
			t.Errorf("清理角色权限: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM directory_change_logs WHERE tenant_id = $1`, tenantID); err != nil {
			t.Errorf("清理变更日志: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM roles WHERE tenant_id = $1`, tenantID); err != nil {
			t.Errorf("清理角色: %v", err)
		}
	})

	// 取两个真实存在的权限编码；写死编码会让这个测试随权限表的增删而碎掉。
	rows, err := pool.Query(ctx, `SELECT code FROM permissions ORDER BY code LIMIT 2`)
	if err != nil {
		t.Fatalf("读取权限编码: %v", err)
	}
	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatal(err)
		}
		codes = append(codes, code)
	}
	rows.Close()
	if len(codes) < 2 {
		t.Skip("权限表里不足两条记录，跳过")
	}

	var operatorID int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM employees WHERE tenant_id = $1 LIMIT 1`, tenantID).Scan(&operatorID); err != nil {
		t.Fatalf("读取操作人: %v", err)
	}

	if err := svc.GrantRolePermissions(ctx, tenantID, roleID, codes, operatorID); err != nil {
		t.Fatalf("保存角色权限失败（生产上这一步是 500）: %v", err)
	}

	// 权限真的进去了 —— 这是 bug 最坏的部分：审计一挡，整个事务回滚，
	// 用户以为只是报错，其实什么也没保存。
	var granted int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM role_permissions WHERE tenant_id = $1 AND role_id = $2`,
		tenantID, roleID).Scan(&granted); err != nil {
		t.Fatal(err)
	}
	if granted != len(codes) {
		t.Errorf("角色权限条数 = %d，期望 %d", granted, len(codes))
	}

	// 审计也真的进去了。
	var audits int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM directory_change_logs
		 WHERE tenant_id = $1 AND entity_type = 'ROLE' AND entity_id = $2 AND action = 'SET_PERMISSIONS'`,
		tenantID, roleID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Errorf("ROLE/SET_PERMISSIONS 审计行数 = %d，期望 1", audits)
	}
}

// 数据范围走的是同一张表、同一个 'ROLE' 取值，坏法一模一样，只是还没有人点到它。
func TestSetRoleDataScopePersistsAndAudits(t *testing.T) {
	pool, ctx := loginTestPool(t)
	tenantID := seedCompany(t, ctx, pool, "数据范围公司", "scopeaudit@example.com", "pw-for-test-only")

	var roleID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO roles (tenant_id, code, name) VALUES ($1, 'SCOPER', '范围角色') RETURNING id`,
		tenantID).Scan(&roleID); err != nil {
		t.Fatalf("播种角色: %v", err)
	}
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM role_data_scopes WHERE tenant_id = $1`,
			`DELETE FROM directory_change_logs WHERE tenant_id = $1`,
			`DELETE FROM roles WHERE tenant_id = $1`,
		} {
			if _, err := pool.Exec(ctx, stmt, tenantID); err != nil {
				t.Errorf("清理 %q: %v", stmt, err)
			}
		}
	})

	// 直接验证约束本身：服务层的入参会随需求变，而这行插入表达的正是当初被
	// 挡住的那件事。
	if _, err := pool.Exec(ctx,
		`INSERT INTO directory_change_logs (tenant_id, entity_type, entity_id, action, operator_id)
		 VALUES ($1, 'ROLE', $2, 'SET_DATA_SCOPE', 0)`,
		tenantID, roleID); err != nil {
		t.Fatalf("写入 ROLE 类型的变更日志失败: %v", err)
	}
}
