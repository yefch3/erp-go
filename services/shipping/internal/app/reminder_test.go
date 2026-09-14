package app

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

func TestArrivalReminderTextContainsRequiredScheduleFields(t *testing.T) {
	row := store.GetDueArrivalReminderForUpdateRow{
		ScheduleID: 42, ScheduleNo: "SCH-42", ContractNo: "CON-7", CustomerName: "测试客户",
		VesselName: "海星号", VoyageNo: "V001", PortOfDischarge: "纽约",
		ReminderType: "ARRIVAL_14D",
		TargetEta:    pgtype.Date{Time: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), Valid: true},
	}
	title, content, link := arrivalReminderText(row, time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{"SCH-42", "CON-7", "测试客户", "海星号", "V001", "纽约", "2026-08-12"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content %q does not contain %q", content, want)
		}
	}
	if title != "船期预计 5 天后到港" || link != "/shipping/42" {
		t.Fatalf("title=%q link=%q", title, link)
	}
}

func TestTransportReminderTextForDeparture(t *testing.T) {
	row := store.GetDueArrivalReminderForUpdateRow{
		ScheduleID: 42, ScheduleNo: "SCH-42", PortOfLoading: "Shanghai", PortName: "Shanghai",
		ReminderType: "DEPARTURE_3D",
		TargetEta:    pgtype.Date{Time: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), Valid: true},
	}
	title, content, _ := arrivalReminderText(row, time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC))
	if title != "船期预计 2 天后开船" {
		t.Fatalf("title=%q", title)
	}
	for _, want := range []string{"Shanghai", "预计离港（ETD）", "2026-09-14"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content %q does not contain %q", content, want)
		}
	}
}

func TestTimestampEventDateUsesPortTimezone(t *testing.T) {
	value := pgtype.Timestamptz{Time: time.Date(2026, 9, 13, 16, 0, 0, 0, time.UTC), Valid: true}
	got := timestampEventDate(value, "Asia/Shanghai")
	if got.Time.Format("2006-01-02") != "2026-09-14" {
		t.Fatalf("date=%s", got.Time.Format("2006-01-02"))
	}
}

func TestArrivalReminderTextUsesCurrentCountdown(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		eta       time.Time
		valid     bool
		wantTitle string
	}{
		{eta: time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC), valid: true, wantTitle: "船期预计 11 天后到港"},
		{eta: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC), valid: true, wantTitle: "船期预计今天到港"},
		{eta: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), valid: true, wantTitle: "船期预计已逾期 2 天"},
		{wantTitle: "船期即将到港"},
	}
	for _, tt := range tests {
		t.Run(tt.wantTitle, func(t *testing.T) {
			title, _, _ := arrivalReminderText(store.GetDueArrivalReminderForUpdateRow{
				TargetEta: pgtype.Date{Time: tt.eta, Valid: tt.valid},
			}, now)
			if title != tt.wantTitle {
				t.Fatalf("title=%q，期望=%q", title, tt.wantTitle)
			}
		})
	}
}

func TestReminderRetryUsesBoundedBackoff(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	if got := reminderRetryAt(0, now); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("first retry=%v", got)
	}
	if got := reminderRetryAt(20, now); !got.Equal(now.Add(64 * time.Minute)) {
		t.Fatalf("bounded retry=%v", got)
	}
}

func TestNormalizeArrivalReminderDays(t *testing.T) {
	got, err := normalizeArrivalReminderDays([]int32{3, 14, 3, 0, 7})
	if err != nil {
		t.Fatal(err)
	}
	want := []int32{14, 7, 3, 0}
	if len(got) != len(want) {
		t.Fatalf("提醒规则=%v，期望=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("提醒规则=%v，期望=%v", got, want)
		}
	}
	if _, err = normalizeArrivalReminderDays([]int32{-1}); errorCode(err) != "SHIPPING_REMINDER_DAYS_INVALID" {
		t.Fatalf("负数提醒应被拒绝，错误=%v", err)
	}
}

func TestArrivalReminderDueAtUsesBusinessTimezoneAndSkipsWeekend(t *testing.T) {
	eta := pgtype.Date{Time: time.Date(2026, time.September, 14, 0, 0, 0, 0, time.UTC), Valid: true} // Monday
	cache := []byte(`{"CN":[{"date":"2026-09-11","summary":"holiday"}]}`)
	due := arrivalReminderDueAt(eta, 1, "Asia/Shanghai", cache)
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got := due.Time.In(location).Format("2006-01-02 15:04"); got != "2026-09-11 00:00" {
		t.Fatalf("due at = %s, want previous weekday", got)
	}
	sameDay := arrivalReminderDueAt(eta, 0, "Asia/Shanghai", cache)
	if got := sameDay.Time.In(location).Format("2006-01-02 15:04"); got != "2026-09-14 00:00" {
		t.Fatalf("same-day reminder = %s", got)
	}
}
