package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// receivableScopeStub 扮演 IAM：每个员工只看见测试指定的那些人。
type receivableScopeStub struct{ views map[int64]Visibility }

func (s *receivableScopeStub) VisibleEmployees(_ context.Context, employeeID int64, _ string) (Visibility, error) {
	if v, ok := s.views[employeeID]; ok {
		return v, nil
	}
	return Visibility{EmployeeIDs: []int64{employeeID}, ScopeType: "SELF"}, nil
}

// TestReceivableDueList 钉住 E1 催收清单的全部判断：
// 排序（该收的日子越早越靠前、没配账期的垫底）、逾期天数的正负、
// 收完的不再出现、按人围栏、以及两个筛子。
func TestReceivableDueList(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set; skipping DB-backed receivable test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, tbl := range []string{"receipt_allocations", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
	}()

	const salesA, salesB, admin = 51, 52, 53
	// mkContract 造一张生效合同：due 传空串表示「客户没配账期」。
	mkContract := func(no string, sales int64, total string, due string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO contracts
			(tenant_id, contract_no, customer_id, customer_name, status, sales_employee_id, sales_employee,
			 effective_at, receivable_due_date, created_by, updated_by)
			VALUES ($1,$2,9,'客户','EFFECTIVE',$3,'销售', now() - interval '10 days', nullif($4,'')::date, 1, 1)
			RETURNING id`, tenantID, no, sales, due).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var versionID int64
		if err := pool.QueryRow(ctx, `INSERT INTO contract_versions
			(tenant_id, contract_id, version_no, status, currency, total_amount, created_by,
			 fx_rate, fx_rate_at, fx_source)
			VALUES ($1,$2,1,'APPROVED','USD',$3::numeric,1, 1, now(), 'TEST') RETURNING id`, tenantID, id, total).Scan(&versionID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE contracts SET current_version_id=$2 WHERE id=$1`, id, versionID); err != nil {
			t.Fatal(err)
		}
		return id
	}
	today := dbToday(ctx, t, pool)
	overdue := today.AddDate(0, 0, -30).Format("2006-01-02") // 逾期 30 天
	soon := today.AddDate(0, 0, 5).Format("2006-01-02")      // 还有 5 天

	overdueID := mkContract("CT-RECV-OVERDUE", salesA, "50000", overdue)
	mkContract("CT-RECV-SOON", salesA, "30000", soon)
	mkContract("CT-RECV-UNSET", salesA, "20000", "") // 客户没配账期
	mkContract("CT-RECV-OTHER", salesB, "10000", overdue)
	paidID := mkContract("CT-RECV-PAID", salesA, "10000", overdue)

	// 收完的那张：**收满了也照样留在待核销页上**。
	//
	// 这条断言在需求变更后翻了面。老模型里「收完」是算出来的，收满即自动
	// 从清单上消失；新模型下这一页问的是「财务确认过没有」，所以收满但
	// 没人点确认的合同必须还在，等人来点。撤掉的那句条件是 `未收 > 0.01`。
	//
	// transaction_id 直接给一个数，不去建银行流水行：F2 之后那一行在采购
	// 的库里，出口这边只存这个引用（跨库，所以已经不是外键了）。
	if _, err := pool.Exec(ctx, `INSERT INTO receipt_allocations (tenant_id,transaction_id,contract_id,contract_no,amount,currency) VALUES ($1,$2,$3,'CT-RECV-PAID',10000,'USD')`, tenantID, tenantID+1, paidID); err != nil {
		t.Fatal(err)
	}

	stub := &receivableScopeStub{views: map[int64]Visibility{
		salesA: {EmployeeIDs: []int64{salesA}, ScopeType: "SELF"},
		admin:  {All: true, ScopeType: "ALL"},
	}}
	svc := New(pool, Deps{Scopes: stub})
	opA := Operator{ID: salesA, Name: "A"}
	opAdmin := Operator{ID: admin, Name: "Admin"}

	// SELF：看见自己的四张——**包含已经收满的那张**（别人的仍然不算）。
	// 逾期的排最前、没配账期的垫底。
	rows, total, err := svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, opA)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(rows) != 4 {
		t.Fatalf("SELF 应看到四张（收满的也在，等人确认；别人的不算），实际 %d 张: %+v", total, rows)
	}
	// 按合同号取行，不按下标——两张合同的到期日相同时谁在前由 id 决定，
	// 用下标断言会在无关的改动下随机翻车。
	byNo := map[string]ReceivableRow{}
	order := make([]string, 0, len(rows))
	for _, r := range rows {
		byNo[r.ContractNo] = r
		order = append(order, r.ContractNo)
	}
	paid, ok := byNo["CT-RECV-PAID"]
	if !ok {
		t.Fatalf("收满但没人确认完成的合同必须留在待核销页"+
			"——「不一定数字一样就可以核销完了」，实际 %v", order)
	}
	if paid.OpenAmount != "0.00" {
		t.Fatalf("收满的那张未收应为 0.00，实际 %s", paid.OpenAmount)
	}
	// 排序：两张逾期的在最前（彼此顺序由 id 定，不断言），没配账期的垫底。
	if order[len(order)-1] != "CT-RECV-UNSET" {
		t.Fatalf("没配账期的该垫底，实际顺序 %v", order)
	}
	if order[0] != "CT-RECV-OVERDUE" && order[0] != "CT-RECV-PAID" {
		t.Fatalf("逾期的该排最前，实际顺序 %v", order)
	}
	// 逾期天数：正数是已逾期，负数是还剩几天。
	if byNo["CT-RECV-OVERDUE"].OverdueDays != 30 {
		t.Fatalf("逾期天数应为 30，实际 %d", byNo["CT-RECV-OVERDUE"].OverdueDays)
	}
	if byNo["CT-RECV-SOON"].OverdueDays != -5 {
		t.Fatalf("未到期的应为 -5（还有五天），实际 %d", byNo["CT-RECV-SOON"].OverdueDays)
	}
	if unset := byNo["CT-RECV-UNSET"]; !unset.DueUnset || unset.DueDate != "" {
		t.Fatalf("没配账期的行应当 DueUnset 且日期为空：%+v", unset)
	}
	// 未收金额是算出来的，不是存的。
	if od := byNo["CT-RECV-OVERDUE"]; od.OpenAmount != "50000.00" || od.ReceivedAmount != "0" {
		t.Fatalf("未收金额算错：%+v", od)
	}

	// ALL：连别人的那张一起看见（收满的那张同样在里面）。
	if _, total, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, opAdmin); err != nil || total != 5 {
		t.Fatalf("ALL 应看到五张，实际 %d，err=%v", total, err)
	}

	// 只看逾期：没到期的和没配账期的都不该出现。
	//
	// 「逾期」看的是**日子**，不是钱——收满的那张到期日也在过去，所以它
	// 也在里面。这一档是给财务按时间收窄队列用的，不是「还欠钱的」的同义词。
	rows, _, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{OverdueOnly: true}, 1, 50, opA)
	if err != nil {
		t.Fatal(err)
	}
	overdueNos := map[string]bool{}
	for _, r := range rows {
		overdueNos[r.ContractNo] = true
	}
	if len(rows) != 2 || !overdueNos["CT-RECV-OVERDUE"] || !overdueNos["CT-RECV-PAID"] {
		t.Fatalf("只看逾期时应剩两张（过了日子的都算，含收满未确认的）：%+v", rows)
	}

	// 只看未配账期：催的是配置，不是钱。
	rows, _, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{UnsetOnly: true}, 1, 50, opA)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ContractNo != "CT-RECV-UNSET" {
		t.Fatalf("只看未配账期时应只剩一张：%+v", rows)
	}

	// 补算：给没有到期日的合同按客户账期补上，且只补空的。
	// 先确认补算不会覆盖已有的日子——逾期那张的日期必须原样不动。
	n, err := svc.BackfillReceivableDue(ctx, tenantID, 9, 90)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("补算应只碰那一张没有到期日的，实际改了 %d 行", n)
	}
	var stillOverdue string
	if err := pool.QueryRow(ctx, `SELECT receivable_due_date::text FROM contracts WHERE id=$1`, overdueID).Scan(&stillOverdue); err != nil {
		t.Fatal(err)
	}
	if stillOverdue != overdue {
		t.Fatalf("补算不该改动已有的到期日：%s ≠ %s", stillOverdue, overdue)
	}
	// 补算过后，「未配账期」这一类就空了。
	rows, _, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{UnsetOnly: true}, 1, 50, opA)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("补算之后不该还有未配账期的行：%+v", rows)
	}
}

// TestReceivableReminderSweep 钉住提醒的触发时机与幂等：
// 四档各响一次、30 天以外不打扰、逾期每 7 天恰好再响一次、重复扫描
// 不产生第二条、收件箱按人隔离。
func TestReceivableReminderSweep(t *testing.T) {
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
	defer func() {
		for _, tbl := range []string{"receivable_reminders", "receipt_allocations", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
	}()

	const sales = 61
	mk := func(no string, total string, dueOffsetDays int) {
		var id int64
		due := dbToday(ctx, t, pool).AddDate(0, 0, dueOffsetDays).Format("2006-01-02")
		if err := pool.QueryRow(ctx, `INSERT INTO contracts
			(tenant_id, contract_no, customer_id, customer_name, status, sales_employee_id, sales_employee,
			 effective_at, receivable_due_date, created_by, updated_by)
			VALUES ($1,$2,9,'客户','EFFECTIVE',$3,'销售', now()-interval '1 day', $4::date, 1,1) RETURNING id`,
			tenantID, no, sales, due).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var vid int64
		if err := pool.QueryRow(ctx, `INSERT INTO contract_versions
			(tenant_id, contract_id, version_no, status, currency, total_amount, created_by, fx_rate, fx_rate_at, fx_source)
			VALUES ($1,$2,1,'APPROVED','USD',$3::numeric,1,1,now(),'TEST') RETURNING id`, tenantID, id, total).Scan(&vid); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE contracts SET current_version_id=$2 WHERE id=$1`, id, vid); err != nil {
			t.Fatal(err)
		}
	}

	mk("CT-REM-FAR", "10000", 60)  // 60 天后到期——还没进视野
	mk("CT-REM-SOON", "20000", 20) // 20 天后——SOON
	mk("CT-REM-DUE", "30000", 3)   // 3 天后——DUE
	mk("CT-REM-TODAY", "40000", 0) // 今天——DUE
	mk("CT-REM-OD1", "50000", -3)  // 逾期 3 天——OVERDUE 第 1 轮
	mk("CT-REM-OD2", "60000", -10) // 逾期 10 天——OVERDUE 第 2 轮

	svc := New(pool, Deps{})
	if _, err := svc.SweepReceivableReminders(ctx); err != nil {
		t.Fatal(err)
	}

	type slot struct {
		Type     string
		PeriodNo int32
	}
	kind := map[string]slot{}
	rows, err := pool.Query(ctx, `SELECT contract_no, reminder_type, period_no FROM receivable_reminders WHERE tenant_id=$1`, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var no, typ string
		var period int32
		if err := rows.Scan(&no, &typ, &period); err != nil {
			t.Fatal(err)
		}
		kind[no] = slot{typ, period}
	}
	rows.Close()

	if _, ok := kind["CT-REM-FAR"]; ok {
		t.Fatal("60 天后到期的不该打扰——30 天以外不进视野")
	}
	for no, want := range map[string]slot{
		"CT-REM-SOON":  {"SOON", 0},
		"CT-REM-DUE":   {"DUE", 0},
		"CT-REM-TODAY": {"DUE", 0},
		"CT-REM-OD1":   {"OVERDUE", 1}, // 逾期 3 天 → 第一轮
		"CT-REM-OD2":   {"OVERDUE", 2}, // 逾期 10 天 → 第二轮
	} {
		got, ok := kind[no]
		if !ok {
			t.Fatalf("%s 应当有一条提醒", no)
		}
		if got != want {
			t.Fatalf("%s 的档位错了：want %+v got %+v", no, want, got)
		}
	}

	// 幂等：再扫一趟什么都不该多出来。
	again, err := svc.SweepReceivableReminders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("重复扫描不该再发提醒，实际又写了 %d 条", again)
	}

	// 收件箱：未读数对得上，全部已读之后归零。
	inbox, unread, err := svc.ReceivableInbox(ctx, tenantID, sales, true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 5 || unread != 5 {
		t.Fatalf("收件箱应有五条未读，实际 %d 条 / 未读 %d", len(inbox), unread)
	}
	marked, err := svc.MarkReceivableRemindersRead(ctx, tenantID, sales, nil)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 5 {
		t.Fatalf("全部已读应标记五条，实际 %d", marked)
	}
	if _, unread, err = svc.ReceivableInbox(ctx, tenantID, sales, false, 50); err != nil || unread != 0 {
		t.Fatalf("标记之后不该还有未读：%d，err=%v", unread, err)
	}

	// 别人的收件箱是空的——提醒按人隔离。
	if other, _, err := svc.ReceivableInbox(ctx, tenantID, 62, false, 50); err != nil || len(other) != 0 {
		t.Fatalf("别人的收件箱不该有东西：%d 条，err=%v", len(other), err)
	}
}

// dbToday 问数据库「今天是几号」。
//
// **不能用 Go 的 time.Now()。** 这些测试造的是「逾期 30 天」这种相对日期，
// 而断言的是 SQL 用 current_date 算出来的天数——两个时钟只要在不同时区，
// 结果就差一天。以前它们能过，只是因为服务器和 Go 碰巧都在 UTC；业务时区
// 一改成 Asia/Shanghai（见 pkg/pgdb），三个测试当场全红。
//
// 问数据库要今天，两边就永远是同一个「今天」，而这些测试真正想钉的是天数
// 算得对不对，本来就和时区无关。
func dbToday(ctx context.Context, t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var d time.Time
	if err := pool.QueryRow(ctx, `SELECT current_date`).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return d
}
