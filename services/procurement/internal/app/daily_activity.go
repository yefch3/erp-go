package app

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/dailyreport"
)

func (s *Service) DailyPurchaseActivity(ctx context.Context, tenantID, viewerID, start, end int64, trendUnit string) ([]*commonv1.DailyActivityRow, error) {
	if viewerID <= 0 || s.scopes == nil {
		return nil, dailyreport.MissingScope()
	}
	scope, err := s.scopes.VisibleEmployees(ctx, viewerID, dailyreport.Scope)
	if err != nil {
		return nil, err
	}
	return dailyreport.Activity(ctx, s.pool, dailyPurchaseActivitySQL, tenantID, scope.All, scope.EmployeeIDs, start, end, trendUnit)
}

const dailyPurchaseActivitySQL = `
WITH events AS (
    SELECT 'inquiry' AS kind, c.id AS document_id, COALESCE(a.operator_id,c.owner_id) AS employee_id, c.created_at AS event_at
    FROM sourcing_cases c
    LEFT JOIN LATERAL (
        SELECT x.operator_id FROM sourcing_case_changes x
        WHERE x.tenant_id=c.tenant_id AND x.case_id=c.id AND x.section='INQUIRY'
          AND x.action='SAVE' AND x.created_at=c.created_at AND x.operator_id>0
        ORDER BY x.id LIMIT 1
    ) a ON true
    WHERE c.tenant_id=$1 AND c.created_at >= $4 AND c.created_at < $5 AND c.deleted_at IS NULL

    UNION ALL
    SELECT 'order', po_id, operator_id, submitted_at FROM purchase_order_submission_activity
    WHERE tenant_id=$1 AND submitted_at >= $4 AND submitted_at < $5
    UNION ALL
    SELECT 'order', po_id, sender_employee_id, completed_at FROM purchase_order_send_attempts
    WHERE tenant_id=$1 AND status='SENT' AND completed_at >= $4 AND completed_at < $5
    UNION ALL
    SELECT 'order', po_id, created_by_id, created_at FROM purchase_order_follow_ups
    WHERE tenant_id=$1 AND created_at >= $4 AND created_at < $5
    UNION ALL
    SELECT 'order', po_id, recorded_by_id, recorded_at FROM historical_purchase_orders
    WHERE tenant_id=$1 AND recorded_at >= $4 AND recorded_at < $5
    UNION ALL
    SELECT 'order', po_id, created_by_id, created_at FROM purchase_supplier_confirmations
    WHERE tenant_id=$1 AND created_at >= $4 AND created_at < $5
    UNION ALL
    SELECT 'order', po_id, changed_by_id, created_at FROM purchase_order_due_changes
    WHERE tenant_id=$1 AND created_at >= $4 AND created_at < $5
    UNION ALL
    SELECT 'order', po_id, changed_by_id, created_at FROM purchase_order_buyer_changes
    WHERE tenant_id=$1 AND created_at >= $4 AND created_at < $5
    UNION ALL
    SELECT 'order', id, closed_by_id, closed_at FROM purchase_orders
    WHERE tenant_id=$1 AND closed_at >= $4 AND closed_at < $5
), bucketed AS (
    SELECT *, date_trunc(nullif($6::text,''), event_at AT TIME ZONE 'Asia/Shanghai') AS bucket FROM events
)
SELECT coalesce(employee_id,0),
       count(DISTINCT document_id) FILTER (WHERE kind='inquiry'),
       0::bigint,
       count(DISTINCT document_id) FILTER (WHERE kind='order'),
       0::bigint, coalesce(to_char(bucket,'YYYY-MM-DD'),'')
FROM bucketed
WHERE employee_id>0 AND ($2::bool OR employee_id=ANY($3::bigint[]))
GROUP BY GROUPING SETS ((), (employee_id), (bucket))
HAVING GROUPING(bucket)=1 OR bucket IS NOT NULL
`
