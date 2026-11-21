package app

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// Eligibility, search and pagination work at batch level. Quotes from an
// inaccessible line never contribute to a batch's status for this operator.
// The batch key matches the existing detail URLs in the procurement UI.
const executionBatchPageSQL = `
WITH scoped AS (
  SELECT r.id, r.tenant_id, r.status, r.required_date, r.contract_no, r.product_name, r.product_code, CASE
    WHEN contract_id <> 0 THEN 'CONTRACT-ID:' || contract_id
    WHEN btrim(contract_no) <> '' THEN 'CONTRACT-NO:' || btrim(contract_no)
    WHEN quotation_id <> 0 THEN 'QUOTATION-ID:' || quotation_id
    WHEN btrim(quotation_no) <> '' THEN 'QUOTATION-NO:' || btrim(quotation_no)
    ELSE 'MANUAL:' || id END AS batch_key
  FROM purchase_requirements r
  WHERE tenant_id = $1 AND ($2::bool OR owner_id = ANY($3::bigint[]))
    AND status IN ('WAITING_REQUOTE', 'PENDING', 'PARTIALLY_ORDERED', 'ORDERED', 'RECEIVED')
), pending AS (
  SELECT r.batch_key, r.id, r.required_date, r.contract_no, r.product_name, r.product_code
  FROM scoped r
  WHERE r.status = 'WAITING_REQUOTE'
    AND ($4::text = '' OR r.batch_key = $4)
    AND contract_requirement_open(r.tenant_id, r.id) > (
      SELECT coalesce(sum(i.qty), 0)
      FROM purchase_order_items i
      JOIN purchase_orders o ON o.tenant_id = i.tenant_id AND o.id = i.po_id
      WHERE i.tenant_id = r.tenant_id AND i.requirement_id = r.id
        AND o.status IN ('DRAFT', 'PENDING_APPROVAL')
    )
), candidates AS (
  SELECT batch_key, min(required_date) AS earliest, array_agg(id ORDER BY id) AS ids
  FROM pending
  GROUP BY batch_key
  HAVING $5::text = '' OR bool_or(contract_no ILIKE '%' || $5 || '%' OR
    product_name ILIKE '%' || $5 || '%' OR product_code ILIKE '%' || $5 || '%')
), quote_owners AS (
  SELECT r.batch_key, bool_or(q.created_by_id = $6 AND $6::bigint > 0) AS mine
  FROM candidates c
  JOIN scoped r ON r.batch_key = c.batch_key
  JOIN purchase_execution_supplier_quotes q ON q.tenant_id = r.tenant_id AND q.requirement_id = r.id
  GROUP BY r.batch_key
), matching AS (
  SELECT c.*, CASE WHEN q.mine THEN 'MY_QUOTED'
    WHEN q.batch_key IS NOT NULL THEN 'COLLEAGUE_QUOTED'
    ELSE 'WAITING' END AS quote_status
  FROM candidates c LEFT JOIN quote_owners q USING(batch_key)
), filtered AS (
  SELECT * FROM matching WHERE $7::text = '' OR quote_status = $7
), page AS (
  SELECT * FROM filtered
  ORDER BY earliest NULLS LAST, batch_key
  LIMIT $8::int OFFSET $9::bigint
)
SELECT coalesce(page.batch_key, ''), coalesce(page.quote_status, ''), coalesce(page.ids, '{}'::bigint[]), totals.total
FROM (SELECT count(*) AS total FROM filtered) totals LEFT JOIN page ON true
ORDER BY page.earliest NULLS LAST, page.batch_key`

func (s *Service) ListExecutionBatchRequirements(ctx context.Context, tenantID int64, op Operator, keyword, batchKey, quoteStatus string, page, size int32) ([]store.ListRequirementsRow, int64, map[string]string, error) {
	switch quoteStatus {
	case "", "MY_QUOTED", "COLLEAGUE_QUOTED", "WAITING":
	default:
		return nil, 0, nil, apierr.Invalid("PR_EXECUTION_QUOTE_STATUS", "无效的实单报价状态")
	}
	visible, err := s.visibleRequirementsTo(ctx, op)
	if err != nil {
		return nil, 0, nil, err
	}
	page, size = normalizePage(page, size)
	if batchKey != "" {
		page, size, keyword, quoteStatus = 1, 1, "", ""
	}
	statuses := map[string]string{}
	var total int64
	var requirements []store.ListRequirementsRow
	err = pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, executionBatchPageSQL, tenantID, visible.All, visible.EmployeeIDs, batchKey, keyword, op.ID, quoteStatus, size, (int64(page)-1)*int64(size))
		if err != nil {
			return err
		}
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var key, status string
			var lines []int64
			if err := rows.Scan(&key, &status, &lines, &total); err != nil {
				return err
			}
			if key != "" {
				statuses[key] = status
				ids = append(ids, lines...)
			}
		}
		if err := rows.Err(); err != nil || len(ids) == 0 {
			return err
		}
		requirements, err = s.q.WithTx(tx).ListRequirements(ctx, store.ListRequirementsParams{
			TenantID: tenantID, Status: "WAITING_REQUOTE", ScopeAll: visible.All, OwnerIds: visible.EmployeeIDs,
			RequirementIds: ids, RowLimit: math.MaxInt32,
		})
		return err
	})
	return requirements, total, statuses, err
}
