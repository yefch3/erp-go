package grpcin

import (
	"context"
	"time"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/mail/internal/app"
)

func (h *Handler) GetDailyMailReport(ctx context.Context, req *mailv1.GetDailyMailReportRequest) (*mailv1.GetDailyMailReportResponse, error) {
	report, err := h.svc.DailyReport(ctx, grpcx.TenantID(ctx), operator(ctx).ID,
		time.Unix(req.GetDayStart(), 0), time.Unix(req.GetAsOf(), 0), req.GetTrendUnit())
	if err != nil {
		return nil, err
	}

	out := &mailv1.GetDailyMailReportResponse{
		AsOf: report.AsOf.Unix(), Total: dailyCounts(report.Total),
		Mailboxes: report.Mailboxes, SyncedMailboxes: report.SyncedMailboxes,
	}
	for _, employee := range report.Employees {
		out.Employees = append(out.Employees, &mailv1.DailyMailEmployee{
			EmployeeId: employee.EmployeeID, Counts: dailyCounts(employee.DailyMailCounts),
		})
	}
	for bucket, counts := range report.Trend {
		out.Trend = append(out.Trend, &mailv1.ReportTrendBucket{StartDate: bucket, Mail: dailyCounts(counts)})
	}
	return out, nil
}

func dailyCounts(counts app.DailyMailCounts) *mailv1.DailyMailCounts {
	out := &mailv1.DailyMailCounts{
		Received: counts.Received, Sent: counts.Sent, Replied: counts.Replied,
		Pending: counts.Pending, Overdue: counts.Overdue,
	}
	if !counts.OldestPendingAt.IsZero() {
		out.OldestPendingAt = counts.OldestPendingAt.Unix()
	}
	return out
}
