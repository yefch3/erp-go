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

// AcceptSourcingCase 保留旧接口兼容性；现在的业务含义是主动成为主责采购。
func (s *Service) AcceptSourcingCase(ctx context.Context, tenantID, caseID int64, op Operator) (SourcingCaseView, error) {
	_, view, err := s.RequestPrimarySourcingCase(ctx, tenantID, caseID, op)
	return view, err
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

// WithdrawSourcingCase lets the responsible salesperson correct a submitted inquiry.
// It deliberately removes the disposable sourcing round while preserving the case,
// its number, original lines and a compact audit entry.
func (s *Service) WithdrawSourcingCase(ctx context.Context, tenantID, caseID int64, reason string, op Operator) (SourcingCaseView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return SourcingCaseView{}, apierr.Invalid("SC_WITHDRAW_REASON_REQUIRED", "请填写撤回原因")
	}
	if op.ID == 0 {
		return SourcingCaseView{}, apierr.Permission("SC_WITHDRAW_OWNER_ONLY", "只有负责销售可以撤回该询盘")
	}

	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var ownerID int64
		var caseNo, status, handoff string
		if err := tx.QueryRow(ctx, `SELECT owner_id,case_no,status,handoff_status
FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, caseID).
			Scan(&ownerID, &caseNo, &status, &handoff); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("SC_NOT_FOUND", "询盘不存在")
			}
			return err
		}
		if ownerID != op.ID {
			return apierr.Permission("SC_WITHDRAW_OWNER_ONLY", "只有负责销售可以撤回该询盘")
		}
		if status == "INTAKE_PENDING" || status == "CANCELLED" || handoff == "SALES_WITHDRAWN" {
			return apierr.Conflict("SC_WITHDRAW_NOT_ALLOWED", "当前询盘不在可撤回阶段")
		}

		var confirmed int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM sourcing_customer_selections
WHERE tenant_id=$1 AND case_id=$2 AND status='CUSTOMER_CONFIRMED'`, tenantID, caseID).Scan(&confirmed); err != nil {
			return err
		}
		if confirmed > 0 {
			return apierr.Conflict("SC_WITHDRAW_CUSTOMER_CONFIRMED", "客户已经正式确认，不能撤回询盘；请走合同或变更流程")
		}

		// Remove the current sourcing round from the outside in so no orphan task,
		// quote or manager/customer plan can continue to look active.
		statements := []string{
			`DELETE FROM sourcing_customer_selections WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_customer_feedback WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_shipping_rework_requests WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_sales_plans WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM procurement_rework_requests WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM cost_scenarios WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_shipping_plans WHERE tenant_id=$1 AND request_id IN (SELECT id FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2)`,
			`DELETE FROM sourcing_shipping_options WHERE tenant_id=$1 AND request_id IN (SELECT id FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2)`,
			`DELETE FROM sourcing_shipping_requests WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM procurement_plans WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM factory_rfqs WHERE tenant_id=$1 AND case_id=$2`,
			`DELETE FROM sourcing_procurement_participants WHERE tenant_id=$1 AND case_id=$2`,
		}
		for _, statement := range statements {
			if _, err := tx.Exec(ctx, statement, tenantID, caseID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE sourcing_lines
SET decision=CASE WHEN decision='SKIPPED' THEN 'SKIPPED' ELSE 'PENDING' END,
    product_id=0,sku_id=0,uom_id=0,decided_by=0,decided_by_name='',decided_at=NULL,updated_at=now()
WHERE tenant_id=$1 AND case_id=$2`, tenantID, caseID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sourcing_cases
SET status='INTAKE_PENDING',handoff_status='SALES_WITHDRAWN',accepted_by=NULL,accepted_by_name='',accepted_at=NULL,
    returned_by=$3,returned_by_name=$4,returned_at=now(),return_reason=$5,return_fields='{}',updated_at=now()
WHERE tenant_id=$1 AND id=$2`, tenantID, caseID, op.ID, op.Name, reason); err != nil {
			return err
		}
		before, _ := json.Marshal(map[string]string{"caseNo": caseNo, "status": status, "handoffStatus": handoff})
		return s.q.WithTx(tx).CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: caseID, Section: "HANDOFF", Action: "SALES_WITHDRAWN",
			Summary: "销售撤回询盘修改", BeforeJson: before,
			AfterJson: []byte(`{"status":"INTAKE_PENDING","handoffStatus":"SALES_WITHDRAWN"}`),
			Reason:    reason, OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return SourcingCaseView{}, err
	}
	s.nudge(ctx, tenantID)
	return s.GetSourcingCase(ctx, tenantID, caseID)
}
