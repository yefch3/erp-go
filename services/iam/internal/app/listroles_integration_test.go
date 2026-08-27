package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 角色列表连着每个角色的权限码。
//
// 原来是一个角色查一次（十来个角色就是十来次往返），现在一次问完。角色数量
// 不大，所以这不是性能事故——但**改成批量查最容易做丢的就是分组**：一不小心
// 就把所有权限码堆到第一个角色上，或者干脆漏掉没有任何权限的那些角色。
//
// 钉三条：每个角色拿到的是自己的码、码是排过序的、**没有权限的角色也要出现
// 在结果里**（漏掉它，页面上那个角色的权限会显示成上一次查询的残留）。
func TestListRolesCarriesEachRolesOwnPermissions(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("IAM_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM role_permissions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM roles WHERE tenant_id=$1`, tenantID)
	})

	// 两个有权限的角色，外加一个一条权限都没有的。
	var withPerms, alsoWithPerms, empty int64
	for _, r := range []struct {
		code string
		id   *int64
	}{{"T_A", &withPerms}, {"T_B", &alsoWithPerms}, {"T_EMPTY", &empty}} {
		if err := pool.QueryRow(ctx, `INSERT INTO roles (tenant_id, code, name)
			VALUES ($1,$2,$2) RETURNING id`, tenantID, r.code).Scan(r.id); err != nil {
			t.Fatal(err)
		}
	}

	// 挑几个真实存在的权限，按码排序取，好让期望值是确定的。
	var permIDs []int64
	var permCodes []string
	rows, err := pool.Query(ctx, `SELECT id, code FROM permissions ORDER BY code LIMIT 3`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		var code string
		if err := rows.Scan(&id, &code); err != nil {
			t.Fatal(err)
		}
		permIDs = append(permIDs, id)
		permCodes = append(permCodes, code)
	}
	rows.Close()
	if len(permIDs) < 3 {
		t.Skip("权限表里不足 3 条，跳过")
	}

	// A 拿前两个，B 拿第三个。分组做错的话，这两组会串。
	for _, pid := range permIDs[:2] {
		if _, err := pool.Exec(ctx, `INSERT INTO role_permissions (tenant_id, role_id, permission_id)
			VALUES ($1,$2,$3)`, tenantID, withPerms, pid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_permissions (tenant_id, role_id, permission_id)
		VALUES ($1,$2,$3)`, tenantID, alsoWithPerms, permIDs[2]); err != nil {
		t.Fatal(err)
	}

	svc := New(pool, "secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	roles, codes, err := svc.ListRoles(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("应该有 3 个角色，实际 %d", len(roles))
	}

	got := codes[withPerms]
	if len(got) != 2 || got[0] != permCodes[0] || got[1] != permCodes[1] {
		t.Fatalf("A 角色的权限码不对（分组串了？）：想要 %v，实际 %v", permCodes[:2], got)
	}
	got = codes[alsoWithPerms]
	if len(got) != 1 || got[0] != permCodes[2] {
		t.Fatalf("B 角色的权限码不对：想要 %v，实际 %v", permCodes[2:], got)
	}
	// 没有权限的角色：结果里查不到它是对的（零值就是空切片），
	// 关键是**不能拿到别人的码**。
	if len(codes[empty]) != 0 {
		t.Fatalf("没有权限的角色不该拿到任何码，实际 %v", codes[empty])
	}
}

// 一个角色都没有的时候不该去查权限，也不该报错。
func TestListRolesWithNoRoles(t *testing.T) {
	dsn := os.Getenv("IAM_TEST_DSN")
	if dsn == "" {
		t.Skip("IAM_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	svc := New(pool, "secret", time.Hour, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	roles, codes, err := svc.ListRoles(ctx, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("没有角色时不该报错: %v", err)
	}
	if len(roles) != 0 || len(codes) != 0 {
		t.Fatalf("应该都是空的，实际 roles=%d codes=%d", len(roles), len(codes))
	}
}
