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
		TargetEta: pgtype.Date{Time: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), Valid: true},
	}
	title, content, link := arrivalReminderText(row)
	for _, want := range []string{"SCH-42", "CON-7", "测试客户", "海星号", "V001", "纽约", "2026-08-12"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content %q does not contain %q", content, want)
		}
	}
	if title == "" || link != "/shipping/42" {
		t.Fatalf("title=%q link=%q", title, link)
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
