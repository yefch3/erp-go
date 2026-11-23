package dailyreport

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
)

const Scope = "business_report"

func ValidateWindow(start, end time.Time, trendUnit string) error {
	if start.Year() < 2000 || end.Before(start) || end.Sub(start) > 366*24*time.Hour ||
		(trendUnit != "" && trendUnit != "week" && trendUnit != "month") {
		return apierr.Invalid("REPORT_DATE_INVALID", "报表时间范围不正确")
	}
	return nil
}

// Activity reads only daily counts. The supplied query owns the metric's
// business rules and must scope by tenant and employee before aggregating.
func Activity(ctx context.Context, pool *pgxpool.Pool, query string, tenantID int64, all bool, employees []int64, start, end int64, trendUnit string) ([]*commonv1.DailyActivityRow, error) {
	if tenantID <= 0 {
		return nil, MissingScope()
	}
	day, asOf := time.Unix(start, 0), time.Unix(end, 0)
	if err := ValidateWindow(day, asOf, trendUnit); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, query, tenantID, all, employees, day, asOf, trendUnit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*commonv1.DailyActivityRow
	for rows.Next() {
		row := &commonv1.DailyActivityRow{}
		if err := rows.Scan(&row.EmployeeId, &row.InquiriesCreated, &row.QuotationsSent, &row.PurchaseOrdersHandled, &row.SchedulesUpdated, &row.Bucket); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func MissingScope() error {
	return apierr.Permission("REPORT_SCOPE_REQUIRED", "无法确认经营报表的数据范围")
}
