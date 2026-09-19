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

// 员工的国家（2026-09-18）：存两位大写码，或者空；别的形状拒掉。
//
// 这一列是拿来分组的（员工邮箱监管那棵树按它分），"us"、"US"、"USA" 要是
// 都能存进去，树上就是三个国家。所以进门就收拾成一种写法，收拾不了的不收。
func TestEmployeeCountryIsStoredAsTwoUpperLettersOrNothing(t *testing.T) {
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
	s := New(pool, "test-secret", time.Hour,
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	depts, err := s.q.ListDepartments(ctx, 1)
	if err != nil || len(depts) == 0 {
		t.Skip("这个库里没有部门，跳过")
	}
	stamp := time.Now().UnixNano()
	code := fmt.Sprintf("CTY%d", stamp%1_000_000_000)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM employees WHERE code = $1`, code)
	})

	// 小写、带空格的照样收，存的是收拾过的。
	emp, err := s.CreateEmployee(ctx, 1, CreateEmployeeInput{
		Code: code, Name: "国家测试", DepartmentID: depts[0].ID, CountryCode: " us ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if emp.CountryCode != "US" {
		t.Fatalf("存进去的国家码是 %q，要 US", emp.CountryCode)
	}

	// 三个字母不是国家码：拒，且说清是国家那一栏。
	_, err = s.UpdateEmployee(ctx, 1, UpdateEmployeeInput{
		ID: emp.ID, Code: code, Name: "国家测试", DepartmentID: depts[0].ID,
		ExpectedVersion: emp.Version, CountryCode: "USA",
	})
	if got := apierr.CodeFromError(err); got != "IAM_EMP_COUNTRY_INVALID" {
		t.Fatalf("USA 应该被拒成 IAM_EMP_COUNTRY_INVALID，实际 %q（%v）", got, err)
	}

	// 清空是允许的：没填 = 树上进「未分配国家」。
	after, err := s.UpdateEmployee(ctx, 1, UpdateEmployeeInput{
		ID: emp.ID, Code: code, Name: "国家测试", DepartmentID: depts[0].ID,
		ExpectedVersion: emp.Version, CountryCode: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if after.CountryCode != "" {
		t.Fatalf("清空后还是 %q", after.CountryCode)
	}
}
