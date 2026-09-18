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

// 新建员工撞上唯一约束时，**说的必须是真正重复的那一项**。
//
// employees 上有两条唯一约束：(tenant_id, code) 和跨租户的 lower(email)。
// 从前这里只看「是不是 23505」，一律说成「工号已存在」——于是填了一个别人
// 用过的邮箱，人看到的是「工号已存在」，而那个工号谁都没用过，怎么换都还是
// 这句话。2026-09-18 报上来的就是这个。
//
// 这条用例走的是真库：约束名只有数据库知道，编一个 pgconn.PgError 出来测
// 等于测我自己抄得对不对。
func TestCreateEmployeeNamesTheFieldThatWasActuallyDuplicated(t *testing.T) {
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
	email := fmt.Sprintf("dup%d@example.test", stamp)
	code := fmt.Sprintf("DUP%d", stamp%1_000_000_000)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM employees WHERE email LIKE $1`,
			fmt.Sprintf("dup%d%%", stamp))
	})

	first, err := s.CreateEmployee(ctx, 1, CreateEmployeeInput{
		Code: code, Name: "重复测试甲", DepartmentID: depts[0].ID, Email: email, OperatorID: 1,
	})
	if err != nil {
		t.Fatalf("第一个都建不出来：%v", err)
	}
	if first.Code != code {
		t.Fatalf("建出来的工号不对：%q", first.Code)
	}

	// 工号一样、邮箱不一样 —— 这才是「工号已存在」。
	_, err = s.CreateEmployee(ctx, 1, CreateEmployeeInput{
		Code: code, Name: "重复测试乙", DepartmentID: depts[0].ID,
		Email: fmt.Sprintf("dup%d-b@example.test", stamp), OperatorID: 1,
	})
	if got := apierr.CodeFromError(err); got != "IAM_EMP_CODE_TAKEN" {
		t.Errorf("工号重复该说工号：%s（%v）", got, err)
	}

	// 邮箱一样、工号不一样 —— 这一条从前也被说成「工号已存在」，就是这次要修的。
	_, err = s.CreateEmployee(ctx, 1, CreateEmployeeInput{
		Code: code + "X", Name: "重复测试丙", DepartmentID: depts[0].ID, Email: email, OperatorID: 1,
	})
	if got := apierr.CodeFromError(err); got != "IAM_EMP_EMAIL_TAKEN" {
		t.Errorf("邮箱重复该说邮箱，不能说成工号：%s（%v）", got, err)
	}
}
