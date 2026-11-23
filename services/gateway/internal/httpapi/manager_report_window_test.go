package httpapi

import (
	"testing"
	"time"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
)

func TestManagerReportWindow(t *testing.T) {
	zone := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, zone)
	month, err := resolveManagerReportWindow(now, "month", "2026-11")
	if err != nil || !month.end.Equal(now) || month.trendUnit != "week" {
		t.Fatalf("unexpected current month: %+v, %v", month, err)
	}
	weeks := month.orderedTrend(make(map[string]*mailv1.ReportTrendBucket))
	if len(weeks) != 4 || weeks[0].GetStartDate() != "2026-11-01" || weeks[0].GetEndDate() != "2026-11-01" ||
		weeks[3].GetStartDate() != "2026-11-16" || weeks[3].GetEndDate() != "2026-11-22" {
		t.Fatalf("unexpected week buckets: %v", weeks)
	}
	year, err := resolveManagerReportWindow(now, "year", "2024")
	if err != nil || year.end.Sub(year.start) != 366*24*time.Hour || year.trendUnit != "month" {
		t.Fatalf("unexpected leap year: %+v, %v", year, err)
	}
	months := year.orderedTrend(make(map[string]*mailv1.ReportTrendBucket))
	if len(months) != 12 || months[1].GetEndDate() != "2024-02-29" || months[11].GetEndDate() != "2024-12-31" {
		t.Fatalf("unexpected month buckets: %v", months)
	}
	for _, input := range [][2]string{{"quarter", ""}, {"month", "2026-12"}, {"day", "2026-11-23"}, {"year", "2027"}, {"month", "2026-1"}} {
		if _, err := resolveManagerReportWindow(now, input[0], input[1]); err == nil {
			t.Errorf("accepted invalid period and date: %q", input)
		}
	}
}
