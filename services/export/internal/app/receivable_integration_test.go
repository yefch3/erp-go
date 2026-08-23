package app

import (
	"context"
	"os"
	"testing"
	"time"

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
		for _, tbl := range []string{"receipt_allocations", "bank_transactions", "bank_accounts", "contract_versions", "contracts"} {
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
	today := time.Now().UTC()
	overdue := today.AddDate(0, 0, -30).Format("2006-01-02") // 逾期 30 天
	soon := today.AddDate(0, 0, 5).Format("2006-01-02")      // 还有 5 天

	overdueID := mkContract("CT-RECV-OVERDUE", salesA, "50000", overdue)
	mkContract("CT-RECV-SOON", salesA, "30000", soon)
	mkContract("CT-RECV-UNSET", salesA, "20000", "") // 客户没配账期
	mkContract("CT-RECV-OTHER", salesB, "10000", overdue)
	paidID := mkContract("CT-RECV-PAID", salesA, "10000", overdue)

	// 收完的那张：造一笔银行进账并全额核销，它就该从催收清单上消失。
	var acctID, txnID int64
	if err := pool.QueryRow(ctx, `INSERT INTO bank_accounts (tenant_id,account_no,account_name,currency) VALUES ($1,'ACC-1','我方账户','USD') RETURNING id`, tenantID).Scan(&acctID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO bank_transactions (tenant_id,account_id,bank_ref,direction,amount,currency,value_date) VALUES ($1,$2,'REF-1','CREDIT',10000,'USD',current_date) RETURNING id`, tenantID, acctID).Scan(&txnID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO receipt_allocations (tenant_id,transaction_id,contract_id,contract_no,amount,currency) VALUES ($1,$2,$3,'CT-RECV-PAID',10000,'USD')`, tenantID, txnID, paidID); err != nil {
		t.Fatal(err)
	}

	stub := &receivableScopeStub{views: map[int64]Visibility{
		salesA: {EmployeeIDs: []int64{salesA}, ScopeType: "SELF"},
		admin:  {All: true, ScopeType: "ALL"},
	}}
	svc := New(pool, Deps{Scopes: stub})
	opA := Operator{ID: salesA, Name: "A"}
	opAdmin := Operator{ID: admin, Name: "Admin"}

	// SELF：只看见自己的三张（收完的那张不算），且逾期的排最前、
	// 没配账期的垫底。
	rows, total, err := svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, opA)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("SELF 应看到三张（收完的不算、别人的不算），实际 %d 张: %+v", total, rows)
	}
	if rows[0].ContractNo != "CT-RECV-OVERDUE" || rows[1].ContractNo != "CT-RECV-SOON" || rows[2].ContractNo != "CT-RECV-UNSET" {
		t.Fatalf("排序错了：逾期的该最前、没配账期的该垫底，实际 %s / %s / %s",
			rows[0].ContractNo, rows[1].ContractNo, rows[2].ContractNo)
	}
	// 逾期天数：正数是已逾期，负数是还剩几天。
	if rows[0].OverdueDays != 30 {
		t.Fatalf("逾期天数应为 30，实际 %d", rows[0].OverdueDays)
	}
	if rows[1].OverdueDays != -5 {
		t.Fatalf("未到期的应为 -5（还有五天），实际 %d", rows[1].OverdueDays)
	}
	if !rows[2].DueUnset || rows[2].DueDate != "" {
		t.Fatalf("没配账期的行应当 DueUnset 且日期为空：%+v", rows[2])
	}
	// 未收金额是算出来的，不是存的。
	if rows[0].OpenAmount != "50000.00" || rows[0].ReceivedAmount != "0" {
		t.Fatalf("未收金额算错：%+v", rows[0])
	}

	// ALL：连别人的那张一起看见（仍不含收完的）。
	if _, total, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{}, 1, 50, opAdmin); err != nil || total != 4 {
		t.Fatalf("ALL 应看到四张，实际 %d，err=%v", total, err)
	}

	// 只看逾期：没到期的和没配账期的都不该出现。
	rows, _, err = svc.ListReceivableDue(ctx, tenantID, ReceivableFilter{OverdueOnly: true}, 1, 50, opA)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ContractNo != "CT-RECV-OVERDUE" {
		t.Fatalf("只看逾期时应只剩一张：%+v", rows)
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
		for _, tbl := range []string{"receivable_reminders", "receipt_allocations", "bank_transactions", "bank_accounts", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
	}()

	const sales = 61
	mk := func(no string, total string, dueOffsetDays int) {
		var id int64
		due := time.Now().UTC().AddDate(0, 0, dueOffsetDays).Format("2006-01-02")
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
