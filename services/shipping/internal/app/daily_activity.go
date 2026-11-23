package app

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	"github.com/sgao19/erp-go/pkg/dailyreport"
)

func (s *Service) DailyShippingActivity(ctx context.Context, tenantID, viewerID, start, end int64, trendUnit string) ([]*commonv1.DailyActivityRow, error) {
	if viewerID <= 0 || s.scopes == nil {
		return nil, dailyreport.MissingScope()
	}
	scope, err := s.scopes.VisibleEmployees(ctx, viewerID, dailyreport.Scope)
	if err != nil {
		return nil, err
	}
	return dailyreport.Activity(ctx, s.pool, dailyShippingActivitySQL, tenantID, scope.All, scope.EmployeeIDs, start, end, trendUnit)
}

const dailyShippingActivitySQL = `
WITH changes AS (
SELECT operator_id, schedule_id,
       date_trunc(nullif($6::text,''), created_at AT TIME ZONE 'Asia/Shanghai') AS bucket
FROM shipping_schedule_changes
WHERE tenant_id=$1 AND created_at >= $4 AND created_at < $5
  AND operator_id>0 AND ($2::bool OR operator_id=ANY($3::bigint[]))
  AND change_type IN ('DATE','ETA','STATUS','CANCEL','VESSEL_VOYAGE','ROUTE','TEMPORARY_CALL','PROGRESS','DELAY','DOCUMENT')
  AND field_name<>'document_download'
)
SELECT coalesce(operator_id,0), 0::bigint, 0::bigint, 0::bigint, count(DISTINCT schedule_id),
       coalesce(to_char(bucket,'YYYY-MM-DD'),'')
FROM changes
GROUP BY GROUPING SETS ((), (operator_id), (bucket))
HAVING GROUPING(bucket)=1 OR bucket IS NOT NULL
`
