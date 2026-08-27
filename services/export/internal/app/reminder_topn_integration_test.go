package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 提醒收件箱刷新一次，看到的清单不能换一批。
//
// 提醒是 sweeper 一趟批量插的，created_at 必然打平；收件箱是 top-N
//（LIMIT 50），不是分页。排序没有唯一列收口时，「取哪 50 条」Postgres 不作
// 保证——刷新一次可能换一批，「我刚才看到那条催款提醒，现在找不到了」。
//
// 和分页那个测试（approval 的 paging_integration_test）同一个病，另一种症状。
func TestReminderInboxIsStableAcrossRefreshes(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	const employee = 4243
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receivable_reminders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "CT-INBOX-1", "USD", "10000")

	// 60 条**创建时间完全相同**的未读提醒——sweeper 一趟插出来就是这样。
	// 唯一键靠 period_no 区分（逾期第 1..60 轮），排序键上全打平。
	const n = 60
	for i := 1; i <= n; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO receivable_reminders
			(tenant_id, contract_id, contract_no, recipient_employee_id,
			 reminder_type, period_no, due_date, open_amount, currency, title, created_at)
			VALUES ($1,$2,'CT-INBOX-1',$3,'OVERDUE',$4,'2026-08-01',1000,'USD',
			        '催款', timestamptz '2026-08-27 09:00:00+08')`,
			tenantID, contractID, employee, i); err != nil {
			t.Fatal(err)
		}
	}

	svc := New(pool, Deps{})
	list := func() map[int64]bool {
		rows, _, err := svc.ReceivableInbox(ctx, tenantID, employee, true, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 50 {
			t.Fatalf("top-50 应该给 50 条，实际 %d", len(rows))
		}
		got := make(map[int64]bool, len(rows))
		for _, r := range rows {
			got[r.ID] = true
		}
		return got
	}

	first := list()
	// 在两次刷新之间动几条行（只碰不改）：UPDATE 会把行挪到堆末尾，打平的行
	// 顺序就跟着变。生产上对应的是「有人标了一条已读 / sweeper 补了一轮」——
	// 收件箱本来就是一边被读一边被写的。
	for _, pn := range []int{5, 25, 45} {
		if _, err := pool.Exec(ctx, `UPDATE receivable_reminders SET title = title
			WHERE tenant_id=$1 AND period_no=$2`, tenantID, pn); err != nil {
			t.Fatal(err)
		}
	}
	second := list()

	for id := range first {
		if !second[id] {
			t.Errorf("提醒 %d 第一次刷新看得见、第二次没了——清单在换", id)
		}
	}
	for id := range second {
		if !first[id] {
			t.Errorf("提醒 %d 第二次才冒出来——清单在换", id)
		}
	}
}
