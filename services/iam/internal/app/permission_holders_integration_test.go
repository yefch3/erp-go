package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 「挑一位做得了这件事的同事」用的名单，必须和逐人判定是同一个口径。
//
// ListPermissionHolders 给业务服务出下拉选项（比如给采购单指定采购员），
// CheckPermission 在那位同事真的去操作时把关。两条查询各写了一遍「谁算持有」
// ——在职、角色启用、邮箱那两项人人都有——只要有一条走偏，页面上就是「下拉里
// 有他，选了却说他没权限」，或者反过来「他明明能做，却选不到」。
// 这里把一家公司里每一种人都摆出来，逐人对两条查询的答案。
//
// 名单只比把关多一条：**得登得上来**（有账号、账号没被停用）。把关用不着问
// 这个——能走到把关那一步的人已经登录了；名单是替别人挑人，挑到一个登不上来
// 的，交过去的东西就没人接得住，交的人自己多半也看不到它了。
func TestPermissionHoldersMatchCheckPermissionPersonByPerson(t *testing.T) {
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
	newTenant := func(tag string) int64 {
		t.Helper()
		domain := fmt.Sprintf("holders%s%d.example", tag, stamp)
		if err := svc.EnsureExtraTenant(ctx, SeedTenant{
			CompanyName: fmt.Sprintf("权限名单-%s-%d", tag, stamp), MailDomains: domain,
			AdminEmail: "admin@" + domain, InitialPassword: fmt.Sprintf("H-%d!aB", stamp),
		}); err != nil {
			t.Fatal(err)
		}
		var tenantID int64
		if err := pool.QueryRow(ctx,
			"SELECT tenant_id FROM tenant_domains WHERE domain=$1", domain).Scan(&tenantID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cleanupTenant(t, pool, tenantID) })
		return tenantID
	}
	tenantID := newTenant("a")
	otherTenant := newTenant("b")

	roleID := func(tenant int64, code string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx,
			"SELECT id FROM roles WHERE tenant_id=$1 AND code=$2", tenant, code).Scan(&id); err != nil {
			t.Fatalf("role %s: %v", code, err)
		}
		return id
	}
	var deptID int64
	var deptName string
	if err := pool.QueryRow(ctx,
		"SELECT id, name FROM departments WHERE tenant_id=$1 AND code='HQ'", tenantID).Scan(&deptID, &deptName); err != nil {
		t.Fatal(err)
	}
	seq := 0
	// account：这个人的登录账号——"ACTIVE" 能登录，"DISABLED" 被停用，"" 没开过账号。
	employee := func(tenant int64, name, status, account string, dept int64, roles ...int64) int64 {
		t.Helper()
		seq++
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO employees (tenant_id, code, name, department_id, email, status)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			tenant, fmt.Sprintf("H%d", seq), name, dept, fmt.Sprintf("h%d-%d@holders.example", seq, stamp), status).Scan(&id); err != nil {
			t.Fatalf("employee %s: %v", name, err)
		}
		for _, role := range roles {
			if _, err := pool.Exec(ctx,
				"INSERT INTO employee_roles (tenant_id, employee_id, role_id) VALUES ($1,$2,$3)", tenant, id, role); err != nil {
				t.Fatalf("role for %s: %v", name, err)
			}
		}
		if account != "" {
			// 密码串是个占位：这条测试不登录，只看账号在不在、停没停用。
			if _, err := pool.Exec(ctx, `INSERT INTO users (tenant_id, employee_id, username, password_hash, status) VALUES ($1,$2,$3,'not-a-hash',$4)`,
				tenant, id, fmt.Sprintf("h%d-%d", seq, stamp), account); err != nil {
				t.Fatalf("account for %s: %v", name, err)
			}
		}
		return id
	}
	buyerRole, salesRole := roleID(tenantID, "BUYER"), roleID(tenantID, "SALES")

	buyer := employee(tenantID, "在职采购", "ACTIVE", "ACTIVE", deptID, buyerRole)
	leftBuyer := employee(tenantID, "离职采购", "INACTIVE", "ACTIVE", deptID, buyerRole)
	noRole := employee(tenantID, "没有角色", "ACTIVE", "ACTIVE", deptID)
	sales := employee(tenantID, "在职销售", "ACTIVE", "ACTIVE", deptID, salesRole)
	// 持有权限、但登不上来的两种人：没开过账号的，账号被停用的。
	noAccount := employee(tenantID, "没开账号的采购", "ACTIVE", "", deptID, buyerRole)
	disabledAccount := employee(tenantID, "账号停用的采购", "ACTIVE", "DISABLED", deptID, buyerRole)
	// 另一家公司里同样身份的人：名单里绝不能出现。
	var otherDept int64
	if err := pool.QueryRow(ctx,
		"SELECT id FROM departments WHERE tenant_id=$1 AND code='HQ'", otherTenant).Scan(&otherDept); err != nil {
		t.Fatal(err)
	}
	foreignBuyer := employee(otherTenant, "别家采购", "ACTIVE", "ACTIVE", otherDept, roleID(otherTenant, "BUYER"))

	const code = "procurement:order:write"

	holders := func(permission string) map[int64]string {
		t.Helper()
		rows, err := svc.ListPermissionHolders(ctx, tenantID, permission)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]string{}
		for _, r := range rows {
			if _, dup := out[r.EmployeeID]; dup {
				t.Fatalf("%d（%s）在名单里出现了两次——持有两个都带这项权限的角色不该让人重复", r.EmployeeID, r.Name)
			}
			out[r.EmployeeID] = r.DepartmentName
		}
		return out
	}
	// 逐人对口径：这家公司的每一个员工，名单里有没有他 == 逐人判定放行、并且他登得上来。
	mustAgree := func(permission, when string) map[int64]string {
		t.Helper()
		listed := holders(permission)
		rows, err := pool.Query(ctx, `SELECT e.id, e.name,
			EXISTS (SELECT 1 FROM users u WHERE u.tenant_id=e.tenant_id AND u.employee_id=e.id AND u.status='ACTIVE')
			FROM employees e WHERE e.tenant_id=$1`, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		type person struct {
			id        int64
			name      string
			canSignIn bool
		}
		var everyone []person
		for rows.Next() {
			var p person
			if err := rows.Scan(&p.id, &p.name, &p.canSignIn); err != nil {
				t.Fatal(err)
			}
			everyone = append(everyone, p)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		for _, p := range everyone {
			allowed, err := svc.CheckPermission(ctx, tenantID, p.id, permission)
			if err != nil {
				t.Fatal(err)
			}
			if _, in := listed[p.id]; in != (allowed && p.canSignIn) {
				t.Fatalf("%s：%s（%d）名单里=%v，逐人判定=%v，登得上来=%v——两条查询的口径走偏了", when, p.name, p.id, in, allowed, p.canSignIn)
			}
		}
		for id := range listed {
			known := false
			for _, p := range everyone {
				known = known || p.id == id
			}
			if !known {
				t.Fatalf("%s：名单里有不属于这家公司的人 %d", when, id)
			}
		}
		return listed
	}

	listed := mustAgree(code, "初始")
	if _, ok := listed[buyer]; !ok {
		t.Fatal("名单漏了该有的人（在职、持有采购专员）")
	}
	for id, why := range map[int64]string{
		leftBuyer: "已离职", noRole: "没有角色", sales: "销售没有这项权限", foreignBuyer: "别家公司的人",
		noAccount: "没开过账号，登不上来", disabledAccount: "账号被停用，登不上来",
	} {
		if _, ok := listed[id]; ok {
			t.Fatalf("名单里出现了不该有的人（%s）", why)
		}
	}
	// 这两位「持有权限」是真的——把关那一头照样放行——只是名单不该替别人挑到他们。
	for _, id := range []int64{noAccount, disabledAccount} {
		if allowed, err := svc.CheckPermission(ctx, tenantID, id, code); err != nil || !allowed {
			t.Fatalf("这条测试的前提不成立：%d 应当持有 %s（%v %v）", id, code, allowed, err)
		}
	}
	// 账号开通（或恢复）之后，当场进名单。
	if _, err := pool.Exec(ctx, "UPDATE users SET status='ACTIVE' WHERE tenant_id=$1 AND employee_id=$2", tenantID, disabledAccount); err != nil {
		t.Fatal(err)
	}
	if _, ok := mustAgree(code, "账号恢复后")[disabledAccount]; !ok {
		t.Fatal("账号恢复之后，持有人没有进名单")
	}
	// 部门名是给下拉里区分重名用的。
	if listed[buyer] != deptName || deptName == "" {
		t.Fatalf("部门名不对：%q（应为 %q）", listed[buyer], deptName)
	}

	// 角色停用：权限当场收回，名单也当场没有他。
	if _, err := pool.Exec(ctx, "UPDATE roles SET status='INACTIVE' WHERE tenant_id=$1 AND id=$2", tenantID, buyerRole); err != nil {
		t.Fatal(err)
	}
	listed = mustAgree(code, "采购专员角色停用后")
	if _, ok := listed[buyer]; ok {
		t.Fatal("角色停用之后，持有人还在名单里")
	}
	if _, err := pool.Exec(ctx, "UPDATE roles SET status='ACTIVE' WHERE tenant_id=$1 AND id=$2", tenantID, buyerRole); err != nil {
		t.Fatal(err)
	}

	// 同时持有两个都带这项权限的角色：只出现一次（上面 holders 里查了重复）。
	if _, err := pool.Exec(ctx, `INSERT INTO role_permissions (tenant_id, role_id, permission_id)
		SELECT $1, $2, id FROM permissions WHERE code=$3 ON CONFLICT DO NOTHING`, tenantID, salesRole, code); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO employee_roles (tenant_id, employee_id, role_id) VALUES ($1,$2,$3)", tenantID, buyer, salesRole); err != nil {
		t.Fatal(err)
	}
	listed = mustAgree(code, "销售角色也带上这项权限后")
	if _, ok := listed[sales]; !ok {
		t.Fatal("角色新带上这项权限之后，持有人没有进名单")
	}

	// 邮箱那两项人人都有（EmployeeHasPermission 里的特例）：名单也得是全体在职、登得上来的人。
	listed = mustAgree("mail:email:read", "邮箱基线权限")
	if _, ok := listed[noRole]; !ok {
		t.Fatal("邮箱权限是在职员工人人都有的，没有角色的人也该在名单里")
	}
	if _, ok := listed[noAccount]; ok {
		t.Fatal("没开过账号的人不该出现在任何名单里")
	}
	if _, ok := listed[leftBuyer]; ok {
		t.Fatal("离职的人不该出现在任何名单里")
	}

	// 一项没人持有的权限：空名单，不是错误。
	if rows, err := svc.ListPermissionHolders(ctx, tenantID, "no:such:permission"); err != nil || len(rows) != 0 {
		t.Fatalf("没人持有的权限应给空名单：%v %v", rows, err)
	}
	// 没给权限编码：明确拒绝，而不是悄悄给空名单——那样调用方会以为「没有人可选」。
	if _, err := svc.ListPermissionHolders(ctx, tenantID, "  "); apierr.CodeFromError(err) != "IAM_PERMISSION_CODE_REQUIRED" {
		t.Fatalf("空的权限编码应被拒绝：%v", err)
	}
}
