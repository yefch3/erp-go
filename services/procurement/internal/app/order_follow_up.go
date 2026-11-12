package app

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type OrderFollowUp struct {
	ID                         int64
	Note, CreatedBy, CreatedAt string
}

// Follow-ups are append-only and never alter quantities, supplier confirmations,
// inspection results, delivery records, or historical production milestones.
func (s *Service) AddOrderFollowUp(ctx context.Context, tenantID, poID int64, note string, op Operator) (OrderFollowUp, error) {
	note = strings.TrimSpace(note)
	if note == "" || utf8.RuneCountInString(note) > 4000 {
		return OrderFollowUp{}, apierr.Invalid("PO_FOLLOW_UP_INVALID", "请填写跟进备注，最多4000字")
	}
	if _, err := s.GetOrder(ctx, tenantID, poID); err != nil {
		return OrderFollowUp{}, err
	}
	var out OrderFollowUp
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state string
		var closed bool
		if err := tx.QueryRow(ctx, `SELECT status,closed_at IS NOT NULL FROM purchase_orders WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, poID).Scan(&state, &closed); err != nil {
			return err
		}
		if closed || (state != poOrdered && state != poPartial && state != poReceived) {
			return apierr.Conflict("PO_FOLLOW_UP_READ_ONLY", "只有履约中的采购单可以添加跟进备注")
		}
		return tx.QueryRow(ctx, `INSERT INTO purchase_order_follow_ups(tenant_id,po_id,note,created_by_id,created_by_name) VALUES($1,$2,$3,$4,$5) RETURNING id,note,created_by_name,created_at::text`, tenantID, poID, note, op.ID, op.Name).Scan(&out.ID, &out.Note, &out.CreatedBy, &out.CreatedAt)
	})
	return out, err
}

func (s *Service) orderFollowUps(ctx context.Context, tenantID, poID int64) ([]OrderFollowUp, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,note,created_by_name,created_at::text FROM purchase_order_follow_ups WHERE tenant_id=$1 AND po_id=$2 ORDER BY created_at DESC,id DESC`, tenantID, poID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrderFollowUp{}
	for rows.Next() {
		var row OrderFollowUp
		if err := rows.Scan(&row.ID, &row.Note, &row.CreatedBy, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
