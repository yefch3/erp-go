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
	rows, err := s.pool.Query(ctx, `SELECT case_id,CASE WHEN confirmed_at IS NULL THEN 'QUOTED' ELSE 'CONFIRMED' END,COALESCE(to_char(confirmed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),'') FROM customer_offers WHERE tenant_id=$1 AND ($2 OR updated_by=$3 OR updated_by=ANY($4::bigint[]))`, op.TenantID, visible.All, op.EmployeeID, visible.EmployeeIDs)
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
