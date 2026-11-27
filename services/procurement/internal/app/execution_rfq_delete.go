package app

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

func (s *Service) deleteExecutionRFQBatch(ctx context.Context, tenantID int64, op Operator, batchKey string) error {
	if s.permissions == nil {
		return apierr.Permission("EXECUTION_RFQ_DELETE_PERMISSION", "需要「删除实单询价」权限")
	}
	allowed, err := s.permissions.Allowed(ctx, op.ID, "procurement:execution-rfq:delete")
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.Permission("EXECUTION_RFQ_DELETE_PERMISSION", "需要「删除实单询价」权限")
	}
	batchKey = strings.TrimSpace(batchKey)
	if batchKey == "" {
		return apierr.Invalid("EXECUTION_RFQ_DELETE_BATCH_REQUIRED", "请选择要删除的实单询价")
	}
	visible, err := s.visibleRequirementsTo(ctx, op)
	if err != nil {
		return err
	}
	notFound := apierr.NotFound("EXECUTION_RFQ_DELETE_BATCH_NOT_FOUND", "找不到可删除的实单询价")
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// Writers lock the same requirement rows before creating tasks or orders.
		// Inspect older versions too because their valid orders can cover current demand.
		rows, err := tx.Query(ctx, `SELECT r.id,r.owner_id,
			(`+executionRFQNotDeletedSQL+` AND `+executionRFQVisibleStateSQL+` AND `+executionRFQCurrentVersionSQL+`)
			FROM purchase_requirements r WHERE r.tenant_id=$1 AND (`+executionRFQBatchKeySQL+`)=$2
			ORDER BY r.id FOR UPDATE OF r`, tenantID, batchKey)
		if err != nil {
			return err
		}
		var allIDs, currentIDs []int64
		var id, ownerID int64
		var current bool
		_, err = pgx.ForEachRow(rows, []any{&id, &ownerID, &current}, func() error {
			allIDs = append(allIDs, id)
			if current {
				if !ownerVisible(visible, ownerID) {
					return notFound
				}
				currentIDs = append(currentIDs, id)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(currentIDs) == 0 {
			return notFound
		}
		var activeTask, activeOrder bool
		err = tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM purchase_execution_assignments a WHERE a.tenant_id=$1 AND a.requirement_id=ANY($2::bigint[]) AND NOT a.cancelled AND a.po_id IS NULL),
			EXISTS (SELECT 1 FROM purchase_order_items i JOIN purchase_orders o ON o.tenant_id=i.tenant_id AND o.id=i.po_id
				WHERE i.tenant_id=$1 AND i.requirement_id=ANY($2::bigint[]) AND o.status<>'CANCELLED')`, tenantID, allIDs).Scan(&activeTask, &activeOrder)
		if err != nil {
			return err
		}
		if activeOrder {
			return apierr.Conflict("EXECUTION_RFQ_DELETE_ORDER_EXISTS", "该询价关联了采购单，请先处理采购草稿或采购单")
		}
		if activeTask {
			return apierr.Conflict("EXECUTION_RFQ_DELETE_TASK_EXISTS", "该询价还有进行中的议价任务，请先取消指派")
		}
		_, err = tx.Exec(ctx, `UPDATE purchase_requirements SET execution_rfq_deleted_at=now(),execution_rfq_deleted_by=$3
			WHERE tenant_id=$1 AND id=ANY($2::bigint[])`, tenantID, currentIDs, op.ID)
		return err
	})
	if err == nil {
		s.nudge(ctx, tenantID)
	}
	return err
}
