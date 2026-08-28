package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// AcceptSourcingCase 由采购员接收销售提交的寻源任务，并记录实际接单人和时间。
func (s *Service) AcceptSourcingCase(ctx context.Context, tenantID, caseID int64, op Operator) (SourcingCaseView, error) {
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.AcceptSourcingCase(ctx, store.AcceptSourcingCaseParams{
			OperatorID: &op.ID, OperatorName: op.Name, TenantID: tenantID, ID: caseID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return apierr.Conflict("SC_HANDOFF_NOT_WAITING", "任务已被接收或当前不在待接单状态")
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: caseID, Section: "HANDOFF", Action: "PROCUREMENT_ACCEPTED",
			Summary: "采购接收寻源任务", BeforeJson: []byte(`{"handoffStatus":"WAITING_ACCEPTANCE"}`),
			AfterJson: []byte(`{"handoffStatus":"IN_PROGRESS"}`), OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return SourcingCaseView{}, err
	}
	return s.GetSourcingCase(ctx, tenantID, caseID)
}

// ReturnSourcingCase 将资料不完整的任务退回销售补充；缺失项和简短原因会永久保留在变更记录中。
func (s *Service) ReturnSourcingCase(ctx context.Context, tenantID, caseID int64, fields []string, reason string, op Operator) (SourcingCaseView, error) {
	reason = strings.TrimSpace(reason)
	cleaned := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" && !seen[field] {
			seen[field] = true
			cleaned = append(cleaned, field)
		}
	}
	if len(cleaned) == 0 || reason == "" {
		return SourcingCaseView{}, apierr.Invalid("SC_RETURN_REQUIRED", "请选择需要补充的资料并填写简短原因")
	}
	fieldsJSON, _ := json.Marshal(cleaned)
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.ReturnSourcingCase(ctx, store.ReturnSourcingCaseParams{
			OperatorID: &op.ID, OperatorName: op.Name, Reason: reason, Fields: cleaned,
			TenantID: tenantID, ID: caseID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return apierr.Conflict("SC_HANDOFF_NOT_RETURNABLE", "当前任务不能退回销售补充")
		}
		// 退回后客户规格可能发生变化，旧的确认和产品映射不能继续冒充当前版本。
		if _, err = tx.Exec(ctx, `UPDATE sourcing_lines
SET decision=CASE WHEN decision='SKIPPED' THEN 'SKIPPED' ELSE 'PENDING' END,
    product_id=0, sku_id=0, uom_id=0,
    decided_by=0, decided_by_name='', decided_at=NULL, updated_at=now()
WHERE tenant_id=$1 AND case_id=$2`, tenantID, caseID); err != nil {
			return err
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: caseID, Section: "HANDOFF", Action: "RETURNED_FOR_SUPPLEMENT",
			Summary: "采购退回销售补充资料", BeforeJson: []byte(`{"handoffStatus":"IN_PROGRESS"}`),
			AfterJson: fieldsJSON, Reason: reason, OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return SourcingCaseView{}, err
	}
	return s.GetSourcingCase(ctx, tenantID, caseID)
}
