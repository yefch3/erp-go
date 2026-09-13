package app

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

func testDate(t *testing.T, value string) pgtype.Date {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatal(err)
	}
	return pgtype.Date{Time: parsed, Valid: true}
}

func TestScheduleChangeAlertPlansNotifiesFinanceWhenETADelayed(t *testing.T) {
	service := &Service{directory: stubDirectory{ids: []int64{51}}}
	current := store.ShippingSchedule{ScheduleNo: "S-D6-002", Status: "PLANNED", Eta: testDate(t, "2026-10-15")}
	next := store.CreateScheduleParams{Eta: testDate(t, "2026-10-18")}
	plans, err := service.scheduleChangeAlertPlans(context.Background(), 7, current, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].AlertType != "ETA_DELAYED" || plans[0].Role != "FINANCE" {
		t.Fatalf("plans=%#v, want one finance ETA_DELAYED alert", plans)
	}
}

func TestScheduleChangeAlertPlans(t *testing.T) {
	service := &Service{directory: stubDirectory{ids: []int64{41, 42}}}
	current := store.ShippingSchedule{
		ScheduleNo:         "S-D6-001",
		Status:             "PLANNED",
		Etd:                testDate(t, "2026-09-15"),
		Eta:                testDate(t, "2026-10-15"),
		WarehouseEntryDate: testDate(t, "2026-09-12"),
	}
	next := store.CreateScheduleParams{
		Etd:                testDate(t, "2026-09-17"),
		Eta:                testDate(t, "2026-10-12"),
		WarehouseEntryDate: "2026-09-14",
	}

	plans, err := service.scheduleChangeAlertPlans(context.Background(), 7, current, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 3 {
		t.Fatalf("got %d alert plans, want ETA, ETD and warehouse alerts", len(plans))
	}
	want := []struct{ alertType, role string }{
		{"ETA_ADVANCED", "FINANCE"},
		{"ETD_DELAYED", "PROCUREMENT"},
		{"WAREHOUSE_DELAYED", "PROCUREMENT"},
	}
	for i, expected := range want {
		if plans[i].AlertType != expected.alertType || plans[i].Role != expected.role {
			t.Fatalf("plan %d = %s/%s, want %s/%s", i, plans[i].AlertType, plans[i].Role, expected.alertType, expected.role)
		}
		if len(plans[i].Recipients) != 2 {
			t.Fatalf("plan %d has %d recipients, want 2", i, len(plans[i].Recipients))
		}
	}

	current.Atd = testDate(t, "2026-09-16")
	plans, err = service.scheduleChangeAlertPlans(context.Background(), 7, current, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].AlertType != "ETA_ADVANCED" {
		t.Fatalf("departed schedule plans = %#v, want only ETA_ADVANCED", plans)
	}
}
