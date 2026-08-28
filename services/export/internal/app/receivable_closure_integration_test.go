package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 收款结清。钉的是五条：
//
//	· 结清的合同从催收清单上消失；closed_only 视图能找到它（带类别和差额）
//	· 结清的合同**提醒也不再发**——这是整个机制存在的第一理由：
//	  没有它，一笔退款会让销售每 7 天收一封「应收逾期」，永不停止
//	· 同一张合同不许结清两次；撤销要理由
//	· 撤销后合同回到清单，提醒恢复
//	· 类别只认那四种
func TestReceivableClosureSilencesTheChase(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed closure test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM receivable_reminders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_receivable_closures WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_versions WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id=$1`, tenantID)
	}()

	contractID := seedContract(ctx, t, pool, tenantID, "CT-CLOSE-1", "USD", "10000")
	// 让它逾期：到期日在 10 天前，未收 10000——不结清的话，这正是每 7 天
	// 响一次的那种合同。
	if _, err := pool.Exec(ctx,
		`UPDATE contracts SET receivable_due_date = current_date - 10 WHERE id=$1`, contractID); err != nil {
		t.Fatal(err)
	}

	svc := New(pool, Deps{})
	op := Operator{ID: 5, Name: "Finance"}
	all := Operator{ID: 5, Name: "Finance"} // seedContract 的数据范围走 scope_all

	// 结清前：在清单上，扫一趟提醒会发出来。
	rows, _, err := svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, all)
	if err != nil {
		t.Fatal(err)
	}
	if !hasContract(rows, contractID) {
		t.Fatal("逾期未收的合同不在催收清单上——前提就不成立")
	}
	if _, err := svc.SweepReceivableReminders(ctx); err != nil {
		t.Fatal(err)
	}
	if n := reminderCount(ctx, t, pool, tenantID, contractID); n != 1 {
		t.Fatalf("结清前扫一趟应发 1 条提醒，实际 %d", n)
	}

	// 胡写的类别拒绝。
	if err := svc.CloseReceivable(ctx, tenantID, contractID, "WHATEVER", "", op); err == nil ||
		!strings.Contains(err.Error(), "EX_RCLOSE_CATEGORY_INVALID") {
		t.Fatalf("胡写的类别应该被拒绝，实际 %v", err)
	}

	// 结清。
	if err := svc.CloseReceivable(ctx, tenantID, contractID, "CANCELLED", "合同取消，钱退了", op); err != nil {
		t.Fatal(err)
	}
	if err := svc.CloseReceivable(ctx, tenantID, contractID, "OTHER", "", op); err == nil ||
		!strings.Contains(err.Error(), "EX_RCLOSE_TWICE") {
		t.Fatalf("结清两次应该被拒绝，实际 %v", err)
	}

	// 从催收清单上消失；closed_only 视图能找到，带着类别和快照差额。
	rows, _, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, all)
	if err != nil {
		t.Fatal(err)
	}
	if hasContract(rows, contractID) {
		t.Fatal("结清的合同还在催收清单上")
	}
	closed, _, err := svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{ClosedOnly: true}, 1, 50, all)
	if err != nil {
		t.Fatal(err)
	}
	if !hasContract(closed, contractID) {
		t.Fatal("closed_only 视图找不到结清的合同——撤销就无处可点了")
	}
	for _, r := range closed {
		if r.ContractID == contractID && r.ClosedCategory != "CANCELLED" {
			t.Fatalf("结清类别没带出来：%q", r.ClosedCategory)
		}
	}

	// **提醒静音。** 把已发的清掉再扫：结清的合同一条都不该再发。
	if _, err := pool.Exec(ctx,
		`DELETE FROM receivable_reminders WHERE tenant_id=$1`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SweepReceivableReminders(ctx); err != nil {
		t.Fatal(err)
	}
	if n := reminderCount(ctx, t, pool, tenantID, contractID); n != 0 {
		t.Fatalf("结清的合同又发了 %d 条提醒——这正是要根除的那个每周一封", n)
	}

	// 撤销：没理由不行；有理由之后合同回到清单，提醒恢复。
	if err := svc.ReopenReceivable(ctx, tenantID, contractID, "", op); err == nil {
		t.Fatal("没有理由的撤销被放行了")
	}
	if err := svc.ReopenReceivable(ctx, tenantID, contractID, "钱还是要追", op); err != nil {
		t.Fatal(err)
	}
	rows, _, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, all)
	if err != nil {
		t.Fatal(err)
	}
	if !hasContract(rows, contractID) {
		t.Fatal("撤销结清后合同没有回到催收清单")
	}
	if _, err := svc.SweepReceivableReminders(ctx); err != nil {
		t.Fatal(err)
	}
	if n := reminderCount(ctx, t, pool, tenantID, contractID); n != 1 {
		t.Fatalf("撤销后扫一趟应恢复提醒，实际 %d 条", n)
	}
}

func hasContract(rows []ReceivableRow, id int64) bool {
	for _, r := range rows {
		if r.ContractID == id {
			return true
		}
	}
	return false
}

func reminderCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tenantID, contractID int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM receivable_reminders WHERE tenant_id=$1 AND contract_id=$2`,
		tenantID, contractID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
