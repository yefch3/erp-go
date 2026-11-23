package app

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/dailyreport"
)

func (s *Service) DailyQuotationActivity(ctx context.Context, tenantID, viewerID, start, end int64, trendUnit string) ([]*commonv1.DailyActivityRow, error) {
	if viewerID <= 0 || s.scopes == nil {
		return nil, dailyreport.MissingScope()
	}
	scope, err := s.scopes.VisibleEmployees(ctx, viewerID, dailyreport.Scope)
	if err != nil {
		return nil, err
	}
	return dailyreport.Activity(ctx, s.pool, dailyQuotationActivitySQL, tenantID, scope.All, scope.EmployeeIDs, start, end, trendUnit)
}

// Older rows can be attributed only when the last update was the send
// itself. A later response or reassignment must not get credit for the send.
const dailyQuotationActivitySQL = `
WITH sent AS (
    SELECT coalesce(sent_by, CASE WHEN updated_at=sent_at THEN updated_by END) AS employee_id,
           date_trunc(nullif($6::text,''), sent_at AT TIME ZONE 'Asia/Shanghai') AS bucket
    FROM quotations WHERE tenant_id=$1 AND sent_at >= $4 AND sent_at < $5
)
SELECT coalesce(employee_id,0), 0::bigint, count(*), 0::bigint, 0::bigint, coalesce(to_char(bucket,'YYYY-MM-DD'),'')
FROM sent
WHERE employee_id>0 AND ($2::bool OR employee_id=ANY($3::bigint[]))
GROUP BY GROUPING SETS ((), (employee_id), (bucket))
HAVING GROUPING(bucket)=1 OR bucket IS NOT NULL
`
