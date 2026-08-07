package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type reminderNotifierStub struct{ calls int }

func (n *reminderNotifierStub) NotifyArrivalReminder(context.Context, int64, int64, int64) {
	n.calls++
}

// TestArrivalReminderLifecycle 验证补发、幂等、ETA 改期、员工数据隔离、
// 已读状态以及终止状态取消提醒的完整生命周期。
func TestArrivalReminderLifecycle(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("set SHIPPING_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()/1000 + 930000000
	defer cleanupShippingTenant(ctx, pool, tenantID)

	notifier := &reminderNotifierStub{}
	svc := New(pool)
	svc.UseReminderNotifier(notifier)
	op := Operator{ID: 303, Name: "D3 Test"}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	in := validInput()
	in.ContractNo = "CON-D3-001"
	in.CustomerName = "D3 Customer"
	in.ETD = today.AddDate(0, 0, -1).Format("2006-01-02")
	in.ETA = today.AddDate(0, 0, 5).Format("2006-01-02")
	in.ResponsibleEmployeeID = 303
	in.ResponsibleName = "D3 Test"
	created, err := svc.CreateSchedule(ctx, tenantID, in, op, false)
	if err != nil {
		t.Fatal(err)
	}
	days, err := svc.UpdateArrivalReminderRules(ctx, tenantID, created.ID, []int32{14, 3, 0, 3}, op)
	if err != nil || fmt.Sprint(days) != "[14 3 0]" {
		t.Fatalf("自定义提醒规则=%v err=%v", days, err)
	}
	days, err = svc.GetArrivalReminderRules(ctx, tenantID, created.ID)
	if err != nil || fmt.Sprint(days) != "[14 3 0]" {
		t.Fatalf("读取提醒规则=%v err=%v", days, err)
	}

	if sent, err := svc.ProcessDueArrivalReminders(ctx, 10); err != nil || sent != 1 {
		t.Fatalf("first sweep sent=%d err=%v", sent, err)
	}
	if sent, err := svc.ProcessDueArrivalReminders(ctx, 10); err != nil || sent != 0 {
		t.Fatalf("idempotent sweep sent=%d err=%v", sent, err)
	}
	page, err := svc.ListArrivalNotifications(ctx, tenantID, 303, false)
	if err != nil || len(page.Reminders) != 1 || page.UnreadCount != 1 || notifier.calls != 1 {
		t.Fatalf("page=%+v calls=%d err=%v", page, notifier.calls, err)
	}
	if page.Reminders[0].DetailUrl != "/shipping/"+fmt.Sprint(created.ID) {
		t.Fatalf("detail url=%q", page.Reminders[0].DetailUrl)
	}
	if other, err := svc.ListArrivalNotifications(ctx, tenantID, 999, false); err != nil || len(other.Reminders) != 0 {
		t.Fatalf("employee isolation page=%+v err=%v", other, err)
	}
	if _, err = svc.MarkArrivalReminderRead(ctx, tenantID, 999, page.Reminders[0].ID); errorCode(err) != "SHIPPING_REMINDER_NOT_FOUND" {
		t.Fatalf("cross employee mark code=%q err=%v", errorCode(err), err)
	}
	if _, err = svc.MarkArrivalReminderRead(ctx, tenantID, 303, page.Reminders[0].ID); err != nil {
		t.Fatal(err)
	}
	page, err = svc.ListArrivalNotifications(ctx, tenantID, 303, true)
	if err != nil || len(page.Reminders) != 0 || page.UnreadCount != 0 {
		t.Fatalf("read page=%+v err=%v", page, err)
	}

	in.ETA = today.AddDate(0, 0, 6).Format("2006-01-02")
	if _, err = svc.UpdateSchedule(ctx, tenantID, created.ID, in, "船公司调整 ETA", op, true); err != nil {
		t.Fatal(err)
	}
	if sent, err := svc.ProcessDueArrivalReminders(ctx, 10); err != nil || sent != 1 {
		t.Fatalf("revised ETA sent=%d err=%v", sent, err)
	}
	page, err = svc.ListArrivalNotifications(ctx, tenantID, 303, false)
	if err != nil || len(page.Reminders) != 2 || page.UnreadCount != 1 {
		t.Fatalf("revised page=%+v err=%v", page, err)
	}

	in.ETA = today.AddDate(0, 0, 30).Format("2006-01-02")
	if _, err = svc.UpdateSchedule(ctx, tenantID, created.ID, in, "延期一个月", op, true); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CancelSchedule(ctx, tenantID, created.ID, "测试终态停止提醒", op); err != nil {
		t.Fatal(err)
	}
	if sent, err := svc.ProcessDueArrivalReminders(ctx, 10); err != nil || sent != 0 {
		t.Fatalf("cancelled schedule sent=%d err=%v", sent, err)
	}
}
