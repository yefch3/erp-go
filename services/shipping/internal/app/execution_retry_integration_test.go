package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExecutionRetriesPreserveTimesAndCompletion(t *testing.T) {
	if os.Getenv("SHIPPING_TEST_DSN") == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("SHIPPING_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer cleanupShippingTenant(ctx, pool, tenant)
	svc := New(pool)
	op := Operator{ID: 1, Name: "执行验收"}
	in := validInput()
	in.LoadingPortTimezone, in.DischargePortTimezone = "Asia/Shanghai", "America/Los_Angeles"
	schedule, err := svc.CreateSchedule(ctx, tenant, in, op, false)
	if err != nil {
		t.Fatal(err)
	}
	attachPassedDeliveryFixture(t, ctx, svc, pool, tenant, schedule.ID)
	detail, err := svc.GetScheduleDetails(ctx, tenant, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	depart := ProgressInput{RouteNodeID: detail.Route[0].ID, RouteVersion: schedule.RouteVersion, Action: "DEPART", Reason: "开船"}
	_, first, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, depart, op)
	if err != nil {
		t.Fatal(err)
	}
	_, again, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, depart, op)
	if err != nil || !sameTimestamp(first[0].ActualDepartureAt, again[0].ActualDepartureAt) {
		t.Fatalf("departure retry changed actual time: %v", err)
	}
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	local := time.Now().In(location).AddDate(0, 0, 2)
	localArrival := time.Date(local.Year(), local.Month(), local.Day(), 20, 30, 0, 0, location)
	arrival := localArrival.UTC().Format(time.RFC3339)
	arrived, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{RouteNodeID: detail.Route[1].ID, RouteVersion: schedule.RouteVersion, Action: "ARRIVE", ActualTime: arrival, Reason: "到港"}, op)
	if err != nil || arrived.Status != "ARRIVED" {
		t.Fatalf("arrival: %v", err)
	}
	if dateText(arrived.Ata) != localArrival.Format("2006-01-02") {
		t.Fatal("actual arrival summary used the UTC date instead of the port date")
	}
	completed, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "COMPLETED", "业务结束", op)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "COMPLETED", "业务结束", op)
	if err != nil || retry.ID != completed.ID || retry.Status != "COMPLETED" {
		t.Fatalf("completion retry failed: %v", err)
	}
	var completionCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM shipping_schedule_changes WHERE tenant_id=$1 AND schedule_id=$2 AND field_name='status' AND new_value='COMPLETED'`, tenant, schedule.ID).Scan(&completionCount); err != nil || completionCount != 1 {
		t.Fatalf("completion retry duplicated history: count=%d err=%v", completionCount, err)
	}
}

func TestConcurrentScheduleEditCreatesOneAlertPerRecipient(t *testing.T) {
	if os.Getenv("SHIPPING_TEST_DSN") == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("SHIPPING_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer cleanupShippingTenant(ctx, pool, tenant)
	svc := New(pool)
	svc.UseDirectory(stubDirectory{ids: []int64{7}})
	op := Operator{ID: 1, Name: "并发验收"}
	in := validInput()
	in.WarehouseEntryDate = "2026-10-01"
	schedule, err := svc.CreateSchedule(ctx, tenant, in, op, false)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Rollback(ctx) }()
	if _, err = gate.Exec(ctx, "SELECT id FROM shipping_schedules WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, schedule.ID); err != nil {
		t.Fatal(err)
	}
	in.WarehouseEntryDate = "2026-10-02"
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, e := svc.UpdateSchedule(ctx, tenant, schedule.ID, in, "同一次进仓延期", op, false)
			results <- e
		}()
	}
	blocked := false
	for range 100 {
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '-- name: GetScheduleForUpdate%'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= 2 {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = <-results; err != nil {
			t.Fatal(err)
		}
	}
	if !blocked {
		t.Fatal("concurrent edits did not reach the controlled lock")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM shipping_operational_alerts WHERE tenant_id=$1 AND schedule_id=$2 AND recipient_employee_id=7 AND alert_type='WAREHOUSE_DELAYED'`, tenant, schedule.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate alerts after concurrent edit: count=%d err=%v", count, err)
	}
}
