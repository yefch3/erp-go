package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 假目录：扮演 IAM，返回「船务部门」那批人。
type stubDirectory struct{ ids []int64 }

func (d stubDirectory) EmployeeIDsByRole(context.Context, string) ([]int64, error) {
	return d.ids, nil
}

// TestBLReminderSweep 钉住提单提醒的触发条件与幂等：
// 开船三天内不催、已有正本不催、到港了不催、草稿不算正本、
// 每 3 天一轮、部门展开成多行、重复扫描零新增。
func TestBLReminderSweep(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("SHIPPING_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, tbl := range []string{"shipping_bl_reminders", "shipping_documents", "shipping_arrival_reminders", "shipping_route_nodes", "shipping_schedules"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id=$1", tenantID)
		}
	}()

	const owner = 71
	mk := func(no string, departedDaysAgo int, status string) int64 {
		var id int64
		etd := time.Now().UTC().AddDate(0, 0, -departedDaysAgo).Format("2006-01-02")
		if err := pool.QueryRow(ctx, `INSERT INTO shipping_schedules
			(tenant_id, schedule_no, vessel_name, voyage_no, port_of_loading, port_of_discharge,
			 etd, eta, original_eta, responsible_employee_id, responsible_name, status, created_by, updated_by)
			VALUES ($1,$2,'MV TEST','V001','宁波','Valparaiso',$3::date,$3::date + 30,$3::date + 30,$4,'船务',$5,1,1)
			RETURNING id`, tenantID, no, etd, owner, status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	mk("SC-BL-FRESH", 1, "SAILED")           // 开船 1 天——三天缓冲内，不催
	late := mk("SC-BL-LATE", 5, "IN_TRANSIT") // 开船 5 天——该催，第 2 轮
	old := mk("SC-BL-OLD", 20, "IN_TRANSIT")  // 开船 20 天——第 7 轮
	done := mk("SC-BL-DONE", 10, "IN_TRANSIT") // 已有正本——不催
	mk("SC-BL-ARRIVED", 10, "ARRIVED")        // 已到港——不催
	draft := mk("SC-BL-DRAFT", 10, "IN_TRANSIT") // 只有草稿——仍该催

	addDoc := func(scheduleID int64, category string) {
		if _, err := pool.Exec(ctx, `INSERT INTO shipping_documents
			(tenant_id, schedule_id, document_group_key, category, version, file_name, file_key, file_size, uploaded_by)
			VALUES ($1,$2,$3,$4,1,'bl.pdf',$5,1024,1)`,
			tenantID, scheduleID, category, category, category+"-"+time.Now().Format("150405.000000")+"-"+string(rune('a'+scheduleID%26))); err != nil {
			t.Fatal(err)
		}
	}
	addDoc(done, "BILL_OF_LADING_FINAL")
	addDoc(draft, "BILL_OF_LADING_DRAFT") // 草稿不是能交单的东西

	svc := New(pool)
	svc.UseDirectory(stubDirectory{ids: []int64{81, 82}}) // 船务部门两个人

	if _, err := svc.SweepBLReminders(ctx); err != nil {
		t.Fatal(err)
	}

	type key struct {
		no        string
		recipient int64
	}
	got := map[key]int32{}
	rows, err := pool.Query(ctx, `SELECT schedule_no, recipient_employee_id, period_no FROM shipping_bl_reminders WHERE tenant_id=$1`, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var no string
		var rcpt int64
		var period int32
		if err := rows.Scan(&no, &rcpt, &period); err != nil {
			t.Fatal(err)
		}
		got[key{no, rcpt}] = period
	}
	rows.Close()

	for _, no := range []string{"SC-BL-FRESH", "SC-BL-DONE", "SC-BL-ARRIVED"} {
		for _, rcpt := range []int64{81, 82, owner} {
			if _, ok := got[key{no, rcpt}]; ok {
				t.Fatalf("%s 不该催：开船未满三天 / 已有正本 / 已到港", no)
			}
		}
	}
	// 该催的三张，每张都发给部门两人 + 负责人，共三行。
	for no, wantPeriod := range map[string]int32{
		"SC-BL-LATE":  2, // 开船 5 天 → ceil(5/3) = 2
		"SC-BL-OLD":   7, // 开船 20 天 → ceil(20/3) = 7
		"SC-BL-DRAFT": 4, // 开船 10 天 → ceil(10/3) = 4，草稿不算正本
	} {
		for _, rcpt := range []int64{81, 82, owner} {
			period, ok := got[key{no, rcpt}]
			if !ok {
				t.Fatalf("%s 应当提醒到 %d——部门要展开成多行", no, rcpt)
			}
			if period != wantPeriod {
				t.Fatalf("%s 的轮次错了：want %d got %d", no, wantPeriod, period)
			}
		}
	}
	_ = late
	_ = old

	// 幂等：再扫一趟什么都不该多出来。
	again, err := svc.SweepBLReminders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("重复扫描不该再发，实际又写了 %d 条", again)
	}

	// 收件箱按人隔离，且已读会归零。
	inbox, unread, err := svc.BLInbox(ctx, tenantID, 81, true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 3 || unread != 3 {
		t.Fatalf("船务甲应有三条未读，实际 %d 条 / 未读 %d", len(inbox), unread)
	}
	if _, err := svc.MarkBLRemindersRead(ctx, tenantID, 81, nil); err != nil {
		t.Fatal(err)
	}
	if _, unread, err = svc.BLInbox(ctx, tenantID, 81, false, 50); err != nil || unread != 0 {
		t.Fatalf("标记之后不该还有未读：%d", unread)
	}
	// 甲已读不影响乙。
	if _, unreadB, err := svc.BLInbox(ctx, tenantID, 82, true, 50); err != nil || unreadB != 3 {
		t.Fatalf("乙的未读不该被甲点掉：%d", unreadB)
	}
}
