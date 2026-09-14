package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestHolidayCalendarUsesLastSuccessfulCache(t *testing.T) {
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
	const employeeID = int64(991)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM shipping_user_reminder_preferences WHERE tenant_id=$1", tenantID)
	}()

	var unavailable atomic.Bool
	calendar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if unavailable.Load() {
			http.Error(w, "temporary outage", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nDTSTART;VALUE=DATE:20261001\r\nSUMMARY:National Day\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	}))
	defer calendar.Close()

	service := New(pool)
	service.UseHolidayCalendarClient(calendar.Client(), calendar.URL)
	op := Operator{ID: employeeID, Name: "D6 calendar test"}
	preference, err := service.UpdateUserReminderPreference(ctx, tenantID, []int32{14, 7}, "Asia/Shanghai", []string{"CN"}, op)
	if err != nil {
		t.Fatal(err)
	}
	if preference.CalendarSyncStatus != "SYNCED" || preference.LastSuccessAt == "" {
		t.Fatalf("first sync = %#v, want successful cached calendar", preference)
	}
	var successfulCache []byte
	if err = pool.QueryRow(ctx, "SELECT cached_holidays FROM shipping_user_reminder_preferences WHERE tenant_id=$1 AND employee_id=$2", tenantID, employeeID).Scan(&successfulCache); err != nil {
		t.Fatal(err)
	}

	unavailable.Store(true)
	preference, err = service.SyncUserHolidayCalendars(ctx, tenantID, op)
	if err != nil {
		t.Fatal(err)
	}
	if preference.CalendarSyncStatus != "STALE" || preference.LastSuccessAt == "" || preference.LastError == "" {
		t.Fatalf("fallback sync = %#v, want STALE with last success and visible error", preference)
	}
	var fallbackCache []byte
	if err = pool.QueryRow(ctx, "SELECT cached_holidays FROM shipping_user_reminder_preferences WHERE tenant_id=$1 AND employee_id=$2", tenantID, employeeID).Scan(&fallbackCache); err != nil {
		t.Fatal(err)
	}
	if string(fallbackCache) != string(successfulCache) {
		t.Fatalf("cached holidays changed during outage: before=%s after=%s", successfulCache, fallbackCache)
	}
}
