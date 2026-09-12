package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// 登录名登录（客户要的开户方式：管理员手动填登录名和密码，员工拿来登录，
// 登录名是任意字符串、不必是邮箱、不必绑邮箱）。
//
// 这里种的员工**没有邮箱、也没有 email_verified_at**——那正是从前登不进的
// 那种人。能登进，说明「邮箱必须验证过」那道闸真的拆了。
func TestUsernameLoginNeedsNoMailbox(t *testing.T) {
	pool, ctx := loginTestPool(t)
	tenantID := seedCompany(t, ctx, pool, "用户名公司", fmt.Sprintf("zs%d@usercorp.example", time.Now().UnixNano()), "Seed-Only-Password-1!")
	svc := loginService(pool)

	var employeeID int64
	if err := pool.QueryRow(ctx,
		`SELECT id FROM employees WHERE tenant_id = $1 AND code = 'E001'`, tenantID).Scan(&employeeID); err != nil {
		t.Fatal(err)
	}
	// 没有邮箱、没有验证时间：管理员手动开的账号就长这样。
	if _, err := pool.Exec(ctx,
		`UPDATE employees SET email = '', email_verified_at = NULL WHERE id = $1`, employeeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE employee_id = $1`, employeeID); err != nil {
		t.Fatal(err)
	}
	const pw = "Guangzhou-Export-2026!"
	if _, err := svc.OpenAccount(ctx, tenantID, employeeID, 0, "zhangsan", pw); err != nil {
		t.Fatalf("open account: %v", err)
	}

	res, err := svc.Login(ctx, "zhangsan", pw)
	if err != nil {
		t.Fatalf("a username account without a mailbox could not sign in: %v", err)
	}
	if res.Employee.ID != employeeID {
		t.Fatalf("signed in as employee %d, want %d", res.Employee.ID, employeeID)
	}
	// 管理员打的密码只能开一扇门。
	if !res.MustChangePassword {
		t.Fatal("an administrator-typed password did not demand a change on first login")
	}

	// 不分大小写、两边的空白不算数。
	for _, typed := range []string{"ZhangSan", "  zhangsan  ", "ZHANGSAN"} {
		if _, err := svc.Login(ctx, typed, pw); err != nil {
			t.Errorf("Login(%q) failed: %v", typed, err)
		}
	}

	// 错密码、没这个人：同一句话，不能让登录页变成员工名录。
	if _, err := svc.Login(ctx, "zhangsan", "wrong-"+pw); !errors.Is(err, errBadCredentials) {
		t.Errorf("wrong password: got %v, want bad credentials", err)
	}
	if _, err := svc.Login(ctx, "nobody-here", pw); !errors.Is(err, errBadCredentials) {
		t.Errorf("unknown username: got %v, want bad credentials", err)
	}

	// 员工自己改了密码，强制改密的标记就撤了。
	const pw2 = "Shenzhen-Harbour-77!"
	if err := svc.ChangePassword(ctx, tenantID, employeeID, pw, pw2); err != nil {
		t.Fatalf("change password: %v", err)
	}
	res, err = svc.Login(ctx, "zhangsan", pw2)
	if err != nil {
		t.Fatalf("login after change: %v", err)
	}
	if res.MustChangePassword {
		t.Fatal("still demanding a change after the person chose their own password")
	}
}

// 老账号照旧：seedCompany 种的是邀请开的户，登录名就是邮箱。
func TestEmailLoginStillWorksBesideUsernames(t *testing.T) {
	pool, ctx := loginTestPool(t)
	const pw = "Ningbo-Container-2026!"
	tenantID := seedCompany(t, ctx, pool, "邮箱公司", fmt.Sprintf("wang%d@mailco.example", time.Now().UnixNano()), pw)
	svc := loginService(pool)
	var addr string
	if err := pool.QueryRow(ctx, `SELECT email FROM employees WHERE tenant_id = $1 AND code = 'E001'`, tenantID).Scan(&addr); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, strings.ToUpper(addr), pw); err != nil {
		t.Fatalf("email login broke: %v", err)
	}
	// 登录名是整个字符串。去掉 @ 后面那一截就是另一个名字，登不进。
	if _, err := svc.Login(ctx, "wang", pw); !errors.Is(err, errBadCredentials) {
		t.Errorf("a bare local part signed in: %v", err)
	}
}

// 登录名在整套部署里唯一，不只是公司内唯一：登录页没有「选公司」这一步。
func TestUsernameIsUniqueAcrossTenants(t *testing.T) {
	pool, ctx := loginTestPool(t)
	svc := loginService(pool)
	const pw = "Xiamen-Freight-2026!"
	a := seedCompany(t, ctx, pool, "甲公司", fmt.Sprintf("a%d@jia.example", time.Now().UnixNano()), pw)
	b := seedCompany(t, ctx, pool, "乙公司", fmt.Sprintf("b%d@yi.example", time.Now().UnixNano()), pw)
	emp := func(tenant int64) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM employees WHERE tenant_id = $1 AND code = 'E001'`, tenant).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE employee_id = $1`, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	if _, err := svc.OpenAccount(ctx, a, emp(a), 0, "lisi", pw); err != nil {
		t.Fatal(err)
	}
	_, err := svc.OpenAccount(ctx, b, emp(b), 0, "LiSi", pw)
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Code != "IAM_USERNAME_TAKEN" {
		t.Fatalf("the same username in a second company was accepted: %v", err)
	}
}

// 员工列表的「已激活」认的是有没有登录行，不再看邮箱验证——否则管理员手动
// 开的账号在筛选里哪一档都不在。
func TestAdminOpenedAccountCountsAsActivated(t *testing.T) {
	pool, ctx := loginTestPool(t)
	tenantID := seedCompany(t, ctx, pool, "筛选公司", fmt.Sprintf("ww%d@filtercorp.example", time.Now().UnixNano()), "Seed-Only-Password-1!")
	svc := loginService(pool)
	var employeeID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM employees WHERE tenant_id = $1 AND code = 'E001'`, tenantID).Scan(&employeeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE employees SET email = '', email_verified_at = NULL WHERE id = $1`, employeeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE employee_id = $1`, employeeID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenAccount(ctx, tenantID, employeeID, 0, "wangwu", "Qingdao-Port-2026!"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.q.ListEmployeesFiltered(ctx, store.ListEmployeesFilteredParams{
		TenantID: tenantID, AccountStatus: "ACTIVE", PageSize: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.ID == employeeID {
			found = true
		}
	}
	if !found {
		t.Fatal("an account the administrator opened by hand is not listed as activated")
	}
}

// 登录名可以长得像邮箱而不是任何人的邮箱：系统不关心它像不像。
func TestLoginNameMayLookLikeAnEmail(t *testing.T) {
	pool, ctx := loginTestPool(t)
	tenantID := seedCompany(t, ctx, pool, "像邮箱公司", fmt.Sprintf("seed%d@lookalike.example", time.Now().UnixNano()), "Seed-Only-Password-1!")
	svc := loginService(pool)
	var employeeID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM employees WHERE tenant_id = $1 AND code = 'E001'`, tenantID).Scan(&employeeID); err != nil {
		t.Fatal(err)
	}
	// 员工没有邮箱；管理员偏偏给他定了一个带 @ 的登录名。
	if _, err := pool.Exec(ctx, `UPDATE employees SET email = '', email_verified_at = NULL WHERE id = $1`, employeeID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE employee_id = $1`, employeeID); err != nil {
		t.Fatal(err)
	}
	login := fmt.Sprintf("zhangsan%d@abc.com", time.Now().UnixNano())
	const pw = "Foshan-Ceramics-2026!"
	if _, err := svc.OpenAccount(ctx, tenantID, employeeID, 0, login, pw); err != nil {
		t.Fatalf("an email-looking login name was refused: %v", err)
	}
	if _, err := svc.Login(ctx, login, pw); err != nil {
		t.Fatalf("could not sign in with an email-looking login name: %v", err)
	}
}

// 改邮箱时登录名跟不跟：登录名就是旧邮箱的跟，管理员手动定的不跟。
func TestLoginNameFollowsTheEmailOnlyWhenItWasTheEmail(t *testing.T) {
	pool, ctx := loginTestPool(t)
	svc := loginService(pool)
	const pw = "Dongguan-Toys-2026!"
	stamp := time.Now().UnixNano()

	// 甲：邀请开的户，登录名 = 邮箱。改邮箱之后要拿新邮箱登。
	oldA := fmt.Sprintf("a%d@follow.example", stamp)
	tenantA := seedCompany(t, ctx, pool, "跟着改公司", oldA, pw)
	empA := employeeOf(t, ctx, pool, tenantA)
	newA := fmt.Sprintf("a-new%d@follow.example", stamp)
	if _, err := svc.UpdateEmployee(ctx, tenantA, UpdateEmployeeInput{
		ID: empA.ID, Code: empA.Code, Name: empA.Name, DepartmentID: empA.DepartmentID,
		Email: newA, ExpectedVersion: empA.Version,
	}); err != nil {
		t.Fatalf("update employee: %v", err)
	}
	if _, err := svc.Login(ctx, newA, pw); err != nil {
		t.Errorf("after the email changed, the new address does not sign in: %v", err)
	}
	if _, err := svc.Login(ctx, oldA, pw); !errors.Is(err, errBadCredentials) {
		t.Errorf("the old address still signs in: %v", err)
	}

	// 乙：管理员手动定的登录名，和邮箱无关。改邮箱不动它。
	tenantB := seedCompany(t, ctx, pool, "不跟公司", fmt.Sprintf("b%d@stay.example", stamp), pw)
	empB := employeeOf(t, ctx, pool, tenantB)
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE employee_id = $1`, empB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenAccount(ctx, tenantB, empB.ID, 0, fmt.Sprintf("lisi%d", stamp), pw); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateEmployee(ctx, tenantB, UpdateEmployeeInput{
		ID: empB.ID, Code: empB.Code, Name: empB.Name, DepartmentID: empB.DepartmentID,
		Email: fmt.Sprintf("b-new%d@stay.example", stamp), ExpectedVersion: empB.Version,
	}); err != nil {
		t.Fatalf("update employee: %v", err)
	}
	if _, err := svc.Login(ctx, fmt.Sprintf("lisi%d", stamp), pw); err != nil {
		t.Errorf("a hand-set login name was disturbed by an email change: %v", err)
	}
}

func employeeOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID int64) store.GetEmployeeRow {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM employees WHERE tenant_id = $1 AND code = 'E001'`, tenantID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	emp, err := store.New(pool).GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return emp
}
