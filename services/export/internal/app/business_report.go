package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/shopspring/decimal"
)

const BusinessReportPermission = "report:business:read"

const reportPageSize = 100

// Use the export database's business day for both timestamps and date-only fields.
var reportLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func reportPeriod(year, quarter int32, now time.Time) (time.Time, time.Time, error) {
	today := now.In(reportLocation)
	if year == 0 && quarter == 0 {
		year, quarter = int32(today.Year()), int32((today.Month()-1)/3+1)
	}
	if year < 1900 || year > 9999 || quarter < 1 || quarter > 4 {
		return time.Time{}, time.Time{}, apierr.Invalid("REPORT_PERIOD_INVALID", "年度或季度无效")
	}
	start := time.Date(int(year), time.Month((quarter-1)*3+1), 1, 0, 0, 0, 0, reportLocation)
	if start.After(today) {
		return time.Time{}, time.Time{}, apierr.Invalid("REPORT_PERIOD_INVALID", "季度尚未开始")
	}
	cutoff := start.AddDate(0, 3, -1)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, reportLocation)
	if cutoff.After(today) {
		cutoff = today
	}
	return start, cutoff, nil
}

// reportReceivables uses the current recorded contract terms and the movements
// through the requested business date. Closure and reopening have their own
// timestamps so a later closure does not hide an earlier overdue balance.
const reportReceivables = `
WITH movements AS (
 SELECT a.contract_id, sum(a.amount + a.fee_amount) AS received
 FROM receipt_allocations a
 WHERE a.tenant_id = $1
   AND coalesce(a.received_at, (a.allocated_at AT TIME ZONE 'Asia/Shanghai')::date) <= $2::date
 GROUP BY a.contract_id
), balances AS (
 SELECT c.id, coalesce(nullif(c.external_contract_no,''), c.contract_no) AS number,
        c.customer_name, c.sales_employee_id, c.sales_employee, c.receivable_due_date AS due,
        v.currency, v.total_amount, c.opening_received_amount + coalesce(m.received,0) AS received,
        greatest(v.total_amount - c.opening_received_amount - coalesce(m.received,0),0) AS balance,
        EXISTS (SELECT 1 FROM contract_receivable_closures cl
          WHERE cl.tenant_id=c.tenant_id AND cl.contract_id=c.id
            AND (cl.created_at AT TIME ZONE 'Asia/Shanghai')::date <= $2::date
            AND (cl.revoked_at IS NULL OR (cl.revoked_at AT TIME ZONE 'Asia/Shanghai')::date > $2::date)) AS closed,
        c.status
 FROM contracts c
 JOIN contract_versions v ON v.tenant_id=c.tenant_id AND v.contract_id=c.id AND v.id=c.current_version_id
 LEFT JOIN movements m ON m.contract_id=c.id
 WHERE c.tenant_id=$1 AND c.entry_source <> 'HISTORICAL_RECORD'
   AND c.status IN ('EFFECTIVE','EXECUTING','COMPLETED','PENDING_APPROVAL','PENDING_SIGN')
   AND (coalesce(c.effective_at,c.signed_at,c.created_at) AT TIME ZONE 'Asia/Shanghai')::date <= $2::date
)
`

const reportOverdue = "NOT closed AND balance > 0.01 AND due < $2::date"

const reportSigned = `
SELECT to_char(c.signed_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM'), v.currency,
       c.sales_employee_id, max(c.sales_employee), count(*), sum(v.total_amount)::text
FROM contracts c
JOIN contract_versions v ON v.tenant_id=c.tenant_id AND v.contract_id=c.id AND v.version_no=1
WHERE c.tenant_id=$1 AND c.customer_id > 0 AND c.current_version_id IS NOT NULL
  AND c.status <> 'CANCELLED' AND c.signed_at >= $2 AND c.signed_at < $3
GROUP BY 1,2,3 ORDER BY 1,2,3
`

const reportReceived = `
WITH receipts AS (
 SELECT a.currency, a.amount+a.fee_amount AS amount,
        coalesce(a.received_at, CASE WHEN a.reversal_of IS NOT NULL AND original.received_at IS NOT NULL
          THEN (a.allocated_at AT TIME ZONE 'Asia/Shanghai')::date END) AS received_on,
        (a.allocated_at AT TIME ZONE 'Asia/Shanghai')::date AS recorded_on
 FROM receipt_allocations a
 LEFT JOIN receipt_allocations original ON original.tenant_id=a.tenant_id AND original.id=a.reversal_of
 WHERE a.tenant_id=$1
)
SELECT coalesce(to_char(received_on,'YYYY-MM'),''), currency, count(*), sum(amount)::text
FROM receipts
WHERE coalesce(received_on,recorded_on) BETWEEN $2::date AND $3::date
GROUP BY 1,2 ORDER BY 1,2
`

func (s *Service) BusinessReport(ctx context.Context, req *exv1.GetBusinessReportRequest) (*exv1.GetBusinessReportResponse, error) {
	if err := s.RequireAnyPermission(ctx, BusinessReportPermission); err != nil {
		return nil, err
	}
	start, cutoff, err := reportPeriod(req.GetYear(), req.GetQuarter(), time.Now())
	if err != nil {
		return nil, err
	}
	page := req.GetPage()
	if page < 1 {
		page = 1
	}
	startDate, cutoffDate := start.Format(time.DateOnly), cutoff.Format(time.DateOnly)
	report := &exv1.GetBusinessReportResponse{StartDate: startDate, CutoffDate: cutoffDate}
	tenantID := grpcx.TenantID(ctx)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	switch req.GetView() {
	case "", "quarter":
		err = loadReportQuarter(ctx, tx, tenantID, start, cutoff, report)
	case "overdue", "contracts", "contract":
		err = loadReportContracts(ctx, tx, tenantID, cutoffDate, req.GetView(), int64(page), req.GetContractId(), report)
	case "sales":
		err = loadReportSales(ctx, tx, tenantID, start, cutoff, report)
	default:
		return nil, apierr.Invalid("REPORT_VIEW_INVALID", "报表类型无效")
	}
	if err != nil {
		return nil, err
	}
	if req.GetView() == "contract" {
		if err := loadReportCostBasis(ctx, tx, tenantID, req.GetContractId(), report); err != nil {
			return nil, err
		}
	}
	return report, tx.Commit(ctx)
}

func addReportAmount(amounts *[]*exv1.BusinessReportAmount, currency, amount string) {
	for _, current := range *amounts {
		if current.Currency == currency {
			current.Amount = decimal.RequireFromString(current.Amount).Add(decimal.RequireFromString(amount)).StringFixed(2)
			return
		}
	}
	*amounts = append(*amounts, &exv1.BusinessReportAmount{Currency: currency, Amount: amount})
}

func loadReportQuarter(ctx context.Context, tx pgx.Tx, tenantID int64, start, cutoff time.Time, out *exv1.GetBusinessReportResponse) error {
	months := make(map[string]*exv1.BusinessReportMonth, 3)
	for m := start; !m.After(cutoff); m = m.AddDate(0, 1, 0) {
		row := &exv1.BusinessReportMonth{Month: m.Format("2006-01")}
		months[row.Month] = row
		out.Months = append(out.Months, row)
	}
	rows, err := tx.Query(ctx, reportSigned, tenantID, start, cutoff.AddDate(0, 0, 1))
	if err != nil {
		return err
	}
	var month, currency, owner, amount string
	var employee, count int64
	_, err = pgx.ForEachRow(rows, []any{&month, &currency, &employee, &owner, &count, &amount}, func() error {
		out.SignedCount += count
		addReportAmount(&out.Signed, currency, amount)
		addReportAmount(&months[month].Signed, currency, amount)
		return nil
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, reportReceived, tenantID, out.StartDate, out.CutoffDate)
	if err != nil {
		return err
	}
	_, err = pgx.ForEachRow(rows, []any{&month, &currency, &count, &amount}, func() error {
		if month == "" {
			out.UndatedReceiptCount += count
		} else {
			addReportAmount(&out.Received, currency, amount)
			addReportAmount(&months[month].Received, currency, amount)
		}
		return nil
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, reportReceivables+`
SELECT currency, count(*) FILTER (WHERE `+reportOverdue+`),
       count(*) FILTER (WHERE NOT closed AND balance > 0.01 AND due IS NULL),
       coalesce(sum(balance) FILTER (WHERE `+reportOverdue+`),0)::text
FROM balances GROUP BY currency ORDER BY currency`, tenantID, out.CutoffDate)
	if err != nil {
		return err
	}
	var missing int64
	_, err = pgx.ForEachRow(rows, []any{&currency, &count, &missing, &amount}, func() error {
		out.Total += count
		out.MissingDueDateCount += missing
		if count > 0 {
			addReportAmount(&out.Overdue, currency, amount)
		}
		return nil
	})
	return err
}

func loadReportContracts(ctx context.Context, tx pgx.Tx, tenantID int64, cutoff, view string, page, id int64, out *exv1.GetBusinessReportResponse) error {
	filter := "balance > 0 OR received <> 0 OR total_amount > 0"
	switch view {
	case "overdue":
		filter = reportOverdue
	case "contract":
		filter = "id=$3"
	}
	query := reportReceivables + `
SELECT id, number, customer_name, sales_employee, coalesce(due::text,''), currency,
       total_amount::text, received::text, balance::text,
       coalesce(greatest($2::date-due,0),0), closed, status
FROM balances WHERE ` + filter + ` ORDER BY due NULLS LAST, id DESC`
	args := []any{tenantID, cutoff}
	if view == "contract" {
		args = append(args, id)
	} else {
		if err := tx.QueryRow(ctx, reportReceivables+"SELECT count(*) FROM balances WHERE "+filter, args...).Scan(&out.Total); err != nil {
			return err
		}
		query += " LIMIT $3 OFFSET $4"
		args = append(args, reportPageSize, (page-1)*reportPageSize)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	out.Contracts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (*exv1.BusinessReportContract, error) {
		c := &exv1.BusinessReportContract{}
		err := row.Scan(&c.ContractId, &c.ContractNo, &c.Customer, &c.Owner, &c.DueDate, &c.Currency,
			&c.ContractAmount, &c.Received, &c.Balance, &c.OverdueDays, &c.Closed, &c.Status)
		return c, err
	})
	if view == "contract" && len(out.Contracts) == 0 && err == nil {
		return apierr.NotFound("REPORT_CONTRACT_NOT_FOUND", "未找到此合同")
	}
	return err
}

func loadReportSales(ctx context.Context, tx pgx.Tx, tenantID int64, start, cutoff time.Time, out *exv1.GetBusinessReportResponse) error {
	employees := make(map[int64]*exv1.BusinessReportSales)
	sale := func(id int64, name string) *exv1.BusinessReportSales {
		if employees[id] == nil {
			employees[id] = &exv1.BusinessReportSales{EmployeeId: id, Employee: name}
			out.Sales = append(out.Sales, employees[id])
		} else if employees[id].Employee == "" {
			employees[id].Employee = name
		}
		return employees[id]
	}
	rows, err := tx.Query(ctx, reportSigned, tenantID, start, cutoff.AddDate(0, 0, 1))
	if err != nil {
		return err
	}
	var month, currency, name, amount string
	var id, count int64
	_, err = pgx.ForEachRow(rows, []any{&month, &currency, &id, &name, &count, &amount}, func() error {
		item := sale(id, name)
		item.SignedCount += count
		addReportAmount(&item.Signed, currency, amount)
		return nil
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `
SELECT sales_employee_id,max(sales_employee),count(*) FROM quotations
WHERE tenant_id=$1 AND status <> 'CANCELLED' AND sent_at >= $2 AND sent_at < $3
GROUP BY 1 ORDER BY 1`, tenantID, start, cutoff.AddDate(0, 0, 1))
	if err != nil {
		return err
	}
	_, err = pgx.ForEachRow(rows, []any{&id, &name, &count}, func() error {
		sale(id, name).QuoteCount = count
		return nil
	})
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, reportReceivables+`
SELECT sales_employee_id,max(sales_employee),currency,sum(balance)::text
FROM balances WHERE `+reportOverdue+` GROUP BY 1,3 ORDER BY 1,3`, tenantID, out.CutoffDate)
	if err != nil {
		return err
	}
	_, err = pgx.ForEachRow(rows, []any{&id, &name, &currency, &amount}, func() error {
		addReportAmount(&sale(id, name).Overdue, currency, amount)
		return nil
	})
	return err
}

func loadReportCostBasis(ctx context.Context, tx pgx.Tx, tenantID, id int64, out *exv1.GetBusinessReportResponse) error {
	var versionID int64
	var completed bool
	var batches []int32
	err := tx.QueryRow(ctx, `
SELECT c.contract_no,c.current_version_id,
       coalesce((c.completed_at AT TIME ZONE 'Asia/Shanghai')::date <= $3::date,false),
       ARRAY(SELECT (b.batch->>'batchNo')::int FROM jsonb_array_elements(hp.batches) AS b(batch))
FROM contracts c
LEFT JOIN contract_handoff_plans hp ON hp.tenant_id=c.tenant_id AND hp.contract_version_id=c.current_version_id
WHERE c.tenant_id=$1 AND c.id=$2`, tenantID, id, out.CutoffDate).Scan(&out.InternalContractNo, &versionID, &completed, &batches)
	if err != nil || !completed || out.Contracts[0].GetStatus() != "COMPLETED" || len(batches) == 0 {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT id,qty::text FROM contract_items WHERE tenant_id=$1 AND contract_version_id=$2", tenantID, versionID)
	if err != nil {
		return err
	}
	out.FreightBatches = batches
	out.ContractItemQuantities = make(map[int64]string)
	var itemID int64
	var quantity string
	_, err = pgx.ForEachRow(rows, []any{&itemID, &quantity}, func() error {
		out.ContractItemQuantities[itemID] = quantity
		return nil
	})
	return err
}
