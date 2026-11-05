package app

import (
	"context"
	"encoding/json"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"strconv"
)

func (s *Service) offerSummaries(ctx context.Context, op grpcx.Operator) (string, error) {
	visible, err := s.visibleTo(ctx, Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return "", err
	}
	customerIDs, customerAll, err := s.visibleCustomers(ctx, op.EmployeeID)
	if err != nil {
		return "", err
	}
	// Team visibility follows the responsible salesperson, not the last editor.
	// Older snapshots without an owner retain their existing editor-based scope.
	rows, err := s.pool.Query(ctx, `WITH offers AS (
		SELECT *,COALESCE(NULLIF(source_snapshot->>'ownerId',''),updated_by::text) AS owner_id
		FROM customer_offers WHERE tenant_id=$1
	) SELECT case_id,CASE WHEN confirmed_at IS NULL THEN 'QUOTED' ELSE 'CONFIRMED' END,
	COALESCE(to_char(confirmed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),'')
	FROM offers WHERE ($2 OR owner_id=$3::bigint::text OR owner_id=ANY(SELECT unnest($4::bigint[])::text))
	AND ($5 OR body->>'customerId'=ANY(SELECT unnest($6::bigint[])::text)
		OR ($7 AND owner_id<>$3::bigint::text))`, op.TenantID, visible.All, op.EmployeeID, visible.EmployeeIDs, customerAll, customerIDs, s.scopes != nil)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	summaries := map[string]map[string]string{}
	for rows.Next() {
		var id int64
		var state, at string
		if err := rows.Scan(&id, &state, &at); err != nil {
			return "", err
		}
		summaries[strconv.FormatInt(id, 10)] = map[string]string{"status": state, "confirmedAt": at}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]any{"summaries": summaries})
	return string(data), err
}
