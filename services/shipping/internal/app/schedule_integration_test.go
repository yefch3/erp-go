package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestScheduleLifecycle runs against an explicitly selected shipping database.
// It uses an isolated tenant and removes only the rows it created.
func TestScheduleLifecycle(t *testing.T) {
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
	tenantID := time.Now().UnixNano()/1000 + 900000000
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM shipping_schedule_changes WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM shipping_schedules WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool)
	op := Operator{ID: 101, Name: "D1 Test"}

	input := validInput()
	input.ContractNo = "CON-D1-001"
	input.CustomerName = "D1 Customer"
	created, err := svc.CreateSchedule(ctx, tenantID, input, op, false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ScheduleNo == "" || created.Status != "PLANNED" {
		t.Fatalf("created=%+v", created)
	}

	if _, err = svc.CreateSchedule(ctx, tenantID, input, op, false); errorCode(err) != "SHIPPING_POSSIBLE_DUPLICATE" {
		t.Fatalf("duplicate code=%q err=%v", errorCode(err), err)
	}
	second, err := svc.CreateSchedule(ctx, tenantID, input, op, true)
	if err != nil || second.ScheduleNo == created.ScheduleNo {
		t.Fatalf("confirmed duplicate: second=%+v err=%v", second, err)
	}

	bad := input
	bad.ETA = "2026-08-01"
	if _, err = svc.CreateSchedule(ctx, tenantID, bad, op, false); errorCode(err) != "SHIPPING_ETA_BEFORE_ETD" {
		t.Fatalf("invalid dates code=%q err=%v", errorCode(err), err)
	}

	rows, total, page, size, err := svc.ListSchedules(ctx, tenantID, ListFilter{Keyword: "CON-D1", PortOfLoading: "Shanghai", Page: 1, PageSize: 1})
	if err != nil || len(rows) != 1 || total != 2 || page != 1 || size != 1 {
		t.Fatalf("list rows=%d total=%d page=%d size=%d err=%v", len(rows), total, page, size, err)
	}
	if rows[0].ID != second.ID {
		t.Fatalf("default sort returned id=%d, want most recently updated id=%d", rows[0].ID, second.ID)
	}

	changed := input
	changed.ETD = "2026-08-11"
	changed.ETA = "2026-09-06"
	if _, err = svc.UpdateSchedule(ctx, tenantID, created.ID, changed, "", op, true); errorCode(err) != "SHIPPING_DATE_REASON_REQUIRED" {
		t.Fatalf("missing date reason code=%q err=%v", errorCode(err), err)
	}
	updated, err := svc.UpdateSchedule(ctx, tenantID, created.ID, changed, "船公司调整计划", op, true)
	if err != nil || dateText(updated.Etd) != changed.ETD {
		t.Fatalf("update=%+v err=%v", updated, err)
	}

	updated, err = svc.UpdateScheduleStatus(ctx, tenantID, created.ID, "SAILED", "已收到开船确认", op)
	if err != nil || updated.Status != "SAILED" {
		t.Fatalf("status update=%+v err=%v", updated, err)
	}
	if _, err = svc.UpdateScheduleStatus(ctx, tenantID, created.ID, "COMPLETED", "跳级", op); errorCode(err) != "SHIPPING_STATUS_TRANSITION_INVALID" {
		t.Fatalf("invalid transition code=%q err=%v", errorCode(err), err)
	}

	cancelled, err := svc.CancelSchedule(ctx, tenantID, second.ID, "客户取消订舱", op)
	if err != nil || cancelled.Status != "CANCELLED" {
		t.Fatalf("cancel=%+v err=%v", cancelled, err)
	}
	if _, err = svc.UpdateSchedule(ctx, tenantID, second.ID, input, "", op, true); errorCode(err) != "SHIPPING_FINAL_STATE" {
		t.Fatalf("edit cancelled code=%q err=%v", errorCode(err), err)
	}
	rows, total, _, _, err = svc.ListSchedules(ctx, tenantID, ListFilter{Status: "CANCELLED", ETATo: "2026-09-05", Page: 1, PageSize: 20})
	if err != nil || len(rows) != 1 || total != 1 || rows[0].ID != second.ID {
		t.Fatalf("status/date filter rows=%d total=%d err=%v", len(rows), total, err)
	}

	got, changes, err := svc.GetSchedule(ctx, tenantID, created.ID)
	if err != nil || got.Status != "SAILED" || len(changes) != 3 {
		t.Fatalf("detail=%+v changes=%d err=%v", got, len(changes), err)
	}
}
