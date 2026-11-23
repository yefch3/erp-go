package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
	_ "time/tzdata"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

const dailyReportTimeZone = "Asia/Shanghai"

func (s *Server) getManagerDailyReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	zone, err := time.LoadLocation(dailyReportTimeZone)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "REPORT_TIME_ZONE_UNAVAILABLE", "无法读取报表时区")
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "day"
	}
	window, err := resolveManagerReportWindow(time.Now().In(zone), period, r.URL.Query().Get("date"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "REPORT_DATE_INVALID", "请选择有效的报表日期和周期")
		return
	}
	start, end := window.start.Unix(), window.end.Unix()
	report, err := s.Emails.GetDailyMailReport(ctx, &mailv1.GetDailyMailReportRequest{
		DayStart: start, AsOf: end, TrendUnit: window.trendUnit,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	report.Date, report.TimeZone, report.Period = window.date, dailyReportTimeZone, period
	trend := make(map[string]*mailv1.ReportTrendBucket)
	for _, bucket := range report.Trend {
		trend[bucket.GetStartDate()] = bucket
	}
	s.populateDailyBusinessActivity(ctx, report, trend, start, end, window.trendUnit)
	report.Trend = window.orderedTrend(trend)

	// Only enrich rows the data services already authorized using IAM data scope.
	remaining := make(map[int64]*mailv1.DailyMailEmployee, len(report.Employees))
	for _, row := range report.Employees {
		remaining[row.GetEmployeeId()] = row
	}
	const pageSize = 200
	for page := int32(1); len(remaining) > 0; page++ {
		list, err := s.Directory.ListEmployees(ctx, &iamv1.ListEmployeesRequest{
			Page: &commonv1.PageRequest{Page: page, PageSize: pageSize},
		})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		for _, employee := range list.GetEmployees() {
			if row, ok := remaining[employee.GetId()]; ok {
				row.Name, row.Department = employee.GetName(), employee.GetDepartmentName()
				delete(remaining, employee.GetId())
			}
		}
		if len(list.GetEmployees()) < pageSize || int64(page)*pageSize >= list.GetMeta().GetTotal() {
			break
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	s.writeProto(w, report)
}

func (s *Server) populateDailyBusinessActivity(ctx context.Context, report *mailv1.GetDailyMailReportResponse, trend map[string]*mailv1.ReportTrendBucket, start, end int64, trendUnit string) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	type activitySource struct {
		code string
		load func() ([]*commonv1.DailyActivityRow, error)
	}
	sources := []activitySource{
		{"PURCHASE", func() ([]*commonv1.DailyActivityRow, error) {
			result, err := s.Orders.GetDailyPurchaseActivity(ctx, &prv1.GetDailyPurchaseActivityRequest{DayStart: start, AsOf: end, TrendUnit: trendUnit})
			return result.GetRows(), err
		}},
		{"QUOTATION", func() ([]*commonv1.DailyActivityRow, error) {
			result, err := s.Quotations.GetDailyQuotationActivity(ctx, &exv1.GetDailyQuotationActivityRequest{DayStart: start, AsOf: end, TrendUnit: trendUnit})
			return result.GetRows(), err
		}},
		{"SHIPPING", func() ([]*commonv1.DailyActivityRow, error) {
			result, err := s.Shipping.GetDailyShippingActivity(ctx, &shippingv1.GetDailyShippingActivityRequest{DayStart: start, AsOf: end, TrendUnit: trendUnit})
			return result.GetRows(), err
		}},
	}
	type result struct {
		rows        []*commonv1.DailyActivityRow
		unavailable bool
	}
	results := make([]result, len(sources))
	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Go(func() {
			rows, err := source.load()
			results[i] = result{rows: rows, unavailable: err != nil}
		})
	}
	wg.Wait()

	employees := make(map[int64]*mailv1.DailyMailEmployee, len(report.Employees))
	for _, employee := range report.Employees {
		employees[employee.GetEmployeeId()] = employee
	}
	report.ActivityTotal = &commonv1.DailyActivityRow{}
	for i, res := range results {
		if res.unavailable {
			report.UnavailableActivitySources = append(report.UnavailableActivitySources, sources[i].code)
			continue
		}
		for _, row := range res.rows {
			if key := row.GetBucket(); key != "" {
				bucket := reportTrendBucket(trend, key)
				mergeDailyActivity(bucket.Activity, row)
				continue
			}
			if row.GetEmployeeId() == 0 {
				mergeDailyActivity(report.ActivityTotal, row)
				continue
			}
			id := row.GetEmployeeId()
			employee := employees[id]
			if employee == nil {
				employee = &mailv1.DailyMailEmployee{EmployeeId: id}
				employees[id] = employee
				report.Employees = append(report.Employees, employee)
			}
			if employee.Activity == nil {
				employee.Activity = &commonv1.DailyActivityRow{EmployeeId: id}
			}
			mergeDailyActivity(employee.Activity, row)
		}
	}
}

func mergeDailyActivity(summary, activity *commonv1.DailyActivityRow) {
	summary.InquiriesCreated += activity.GetInquiriesCreated()
	summary.QuotationsSent += activity.GetQuotationsSent()
	summary.PurchaseOrdersHandled += activity.GetPurchaseOrdersHandled()
	summary.SchedulesUpdated += activity.GetSchedulesUpdated()
}

type managerReportWindow struct {
	date, trendUnit string
	start, end      time.Time
}

func resolveManagerReportWindow(now time.Time, period, date string) (managerReportWindow, error) {
	var layout, trendUnit string
	var days, months, years int
	switch period {
	case "day":
		layout, days = time.DateOnly, 1
	case "month":
		layout, months, trendUnit = "2006-01", 1, "week"
	case "year":
		layout, years, trendUnit = "2006", 1, "month"
	default:
		return managerReportWindow{}, fmt.Errorf("invalid report period %q", period)
	}
	if date == "" {
		date = now.Format(layout)
	}
	start, err := time.ParseInLocation(layout, date, now.Location())
	if err != nil || start.Format(layout) != date || start.After(now) || start.Year() < 2000 {
		return managerReportWindow{}, fmt.Errorf("invalid report date %q", date)
	}
	end := start.AddDate(years, months, days)
	if now.Before(end) {
		end = now
	}
	return managerReportWindow{date: date, trendUnit: trendUnit, start: start, end: end}, nil
}

func reportTrendBucket(buckets map[string]*mailv1.ReportTrendBucket, key string) *mailv1.ReportTrendBucket {
	bucket := buckets[key]
	if bucket == nil {
		bucket = &mailv1.ReportTrendBucket{StartDate: key}
		buckets[key] = bucket
	}
	if bucket.Activity == nil {
		bucket.Activity = &commonv1.DailyActivityRow{}
	}
	return bucket
}

func (w managerReportWindow) orderedTrend(buckets map[string]*mailv1.ReportTrendBucket) []*mailv1.ReportTrendBucket {
	if w.trendUnit == "" || !w.start.Before(w.end) {
		return nil
	}
	var days, months int
	first := w.start
	if w.trendUnit == "week" {
		days = 7
		first = first.AddDate(0, 0, -(int(first.Weekday())+6)%7)
	} else {
		months = 1
	}
	var out []*mailv1.ReportTrendBucket
	for start := first; start.Before(w.end); start = start.AddDate(0, months, days) {
		bucket := reportTrendBucket(buckets, start.Format(time.DateOnly))
		end := start.AddDate(0, months, days)
		if end.After(w.end) {
			end = w.end
		}
		if start.Before(w.start) {
			bucket.StartDate = w.start.Format(time.DateOnly)
		}
		bucket.EndDate = end.Add(-time.Nanosecond).Format(time.DateOnly)
		out = append(out, bucket)
	}
	return out
}
