package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 后台循环必须服务每一家有邮箱的公司，不是只服务第一家。
//
// 原来的 withDefaults 把 TenantID 兜底成 1，而 cmd/main.go 从来没传过——于是
// 收信轮询、IDLE 长连接、发信、标记回写、垃圾清理、图片缓存、搜索补齐，**七个
// 后台循环全都只看第一家公司**。第二家的员工把邮箱绑好、授权码填对、页面上一切
// 正常，信却永远不会来。
//
// 而且不报任何错：循环按名单干活，名单里没有就等于不存在。这类「安静地什么都
// 不做」比崩溃难发现得多，所以这条要用真库钉住。
func TestBackgroundLoopsSeeEveryTenantWithAMailbox(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// 两家公司，各一个邮箱。用纳秒当租户号，和别的测试错开。
	base := time.Now().UnixNano()
	tenantA, tenantB := base, base+1
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id IN ($1,$2)", tenantA, tenantB)
	}()

	bind := func(tenantID, employeeID int64, email string, active bool) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO mail_accounts
			(tenant_id, employee_id, email, secret_enc, is_active)
			VALUES ($1,$2,$3,'\x00'::bytea,$4)`,
			tenantID, employeeID, email, active); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 只有 A 有邮箱时，名单里应该有 A。
	bind(tenantA, base+100, "a@example.invalid", true)
	if !hasTenant(svc.tenantsToServe(ctx), tenantA) {
		t.Fatal("绑了邮箱的公司没有出现在名单里")
	}

	// B 绑上邮箱——**不重启进程**，下一轮就该被发现。
	//
	// 这一条是这个测试的重点：名单如果被缓存，新开的公司要等到进程重启才收得到
	// 信，而没有人会把「新客户的邮件不来」联想到「服务没重启」。
	bind(tenantB, base+200, "b@example.invalid", true)
	got := svc.tenantsToServe(ctx)
	if !hasTenant(got, tenantA) || !hasTenant(got, tenantB) {
		t.Fatalf("两家公司都该在名单里，实际 %v", got)
	}

	// 停用的邮箱不该再被轮询：离职的人留着历史，但不该继续占一条连接。
	bind(tenantA, base+300, "left@example.invalid", false)
	// A 仍在（它还有一个活跃邮箱），这里只是确认停用行不会凭空多出一家公司。
	before := len(svc.tenantsToServe(ctx))
	tenantC := base + 2
	bind(tenantC, base+400, "c@example.invalid", false) // 只有停用邮箱
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantC) }()
	if after := svc.tenantsToServe(ctx); len(after) != before || hasTenant(after, tenantC) {
		t.Fatalf("只有停用邮箱的公司不该进名单，实际 %v", after)
	}
}

// 兜底值不能再是 1。
//
// 单独钉一条，因为这正是 bug 的形状：一个看着无害的默认值，让漏传参数从「什么
// 都没发生」变成「安静地服务了错的那家公司」。留 0 是有意的——0 会让漏传变成一次
// 空转，看得见。
func TestSyncConfigDoesNotDefaultToTenantOne(t *testing.T) {
	if got := (SyncConfig{}).withDefaults().TenantID; got != 0 {
		t.Fatalf("SyncConfig 的 TenantID 兜底成了 %d；兜底成任何具体公司都会让漏传变成静默的错", got)
	}
	if got := (WorkerConfig{}).withDefaults().TenantID; got != 0 {
		t.Fatalf("WorkerConfig 的 TenantID 兜底成了 %d，同上", got)
	}
	// 其余兜底值仍然要在——去掉 TenantID 的兜底不该顺手把别的也弄丢。
	c := (SyncConfig{}).withDefaults()
	if c.Folder == "" || c.Interval == 0 || c.BatchSize == 0 || c.Concurrency == 0 || c.HistoryCap == 0 {
		t.Fatalf("其他兜底值被误伤了：%+v", c)
	}
}

func hasTenant(xs []int64, want int64) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
