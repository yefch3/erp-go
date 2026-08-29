package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type SourcingParticipant struct {
	ID                 int64
	EmployeeID         int64
	EmployeeName       string
	Role               string
	Status             string
	PrimaryRequestedAt *time.Time
	JoinedAt           time.Time
	UpdatedAt          time.Time
}

func (s *Service) ListSourcingParticipants(ctx context.Context, tenantID, caseID int64) ([]SourcingParticipant, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, employee_id, employee_name, participant_role, status,
primary_requested_at, joined_at, updated_at
FROM sourcing_procurement_participants
WHERE tenant_id=$1 AND case_id=$2 AND status='ACTIVE'
ORDER BY CASE participant_role WHEN 'PRIMARY' THEN 0 ELSE 1 END, joined_at, id`, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SourcingParticipant, 0)
	for rows.Next() {
		var item SourcingParticipant
		if err := rows.Scan(&item.ID, &item.EmployeeID, &item.EmployeeName, &item.Role, &item.Status,
			&item.PrimaryRequestedAt, &item.JoinedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) requireSourcingParticipant(ctx context.Context, tenantID, caseID, employeeID int64) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sourcing_procurement_participants
WHERE tenant_id=$1 AND case_id=$2 AND employee_id=$3 AND status='ACTIVE')`, tenantID, caseID, employeeID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return apierr.Permission("SC_PARTICIPANT_REQUIRED", "请先参与本案件，再上报询价和报价")
	}
	return nil
}

// JoinSourcingCase records participation without claiming or locking the case.
func (s *Service) JoinSourcingCase(ctx context.Context, tenantID, caseID int64, op Operator) ([]SourcingParticipant, error) {
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT handoff_status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`, tenantID, caseID).Scan(&status); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("SC_NOT_FOUND", "询价案件不存在")
			}
			return err
		}
		if status != "WAITING_ACCEPTANCE" && status != "IN_PROGRESS" {
			return apierr.Conflict("SC_PARTICIPATION_NOT_OPEN", "当前案件尚未开放采购参与")
		}
		result, err := tx.Exec(ctx, `INSERT INTO sourcing_procurement_participants
(tenant_id,case_id,employee_id,employee_name,participant_role,status)
VALUES($1,$2,$3,$4,'COLLABORATOR','ACTIVE')
ON CONFLICT (tenant_id,case_id,employee_id) DO NOTHING`, tenantID, caseID, op.ID, op.Name)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			_, err = tx.Exec(ctx, `UPDATE sourcing_procurement_participants SET employee_name=$4,status='ACTIVE',updated_at=now()
WHERE tenant_id=$1 AND case_id=$2 AND employee_id=$3`, tenantID, caseID, op.ID, op.Name)
			return err
		}
		if result.RowsAffected() > 0 {
			return s.q.WithTx(tx).CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
				TenantID: tenantID, CaseID: caseID, Section: "PROCUREMENT_PARTICIPATION", Action: "PARTICIPANT_JOINED",
				Summary: "采购参与询价", BeforeJson: []byte(`{}`), AfterJson: participantJSON(op.ID, op.Name), OperatorID: op.ID, OperatorName: op.Name,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ListSourcingParticipants(ctx, tenantID, caseID)
}

// RequestPrimarySourcingCase lets a buyer claim an unowned case immediately;
// otherwise it records a request for a manager to review.
func (s *Service) RequestPrimarySourcingCase(ctx context.Context, tenantID, caseID int64, op Operator) ([]SourcingParticipant, SourcingCaseView, error) {
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var acceptedBy *int64
		var handoffStatus string
		if err := tx.QueryRow(ctx, `SELECT accepted_by,handoff_status FROM sourcing_cases
WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, caseID).Scan(&acceptedBy, &handoffStatus); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("SC_NOT_FOUND", "询价案件不存在")
			}
			return err
		}
		if handoffStatus != "WAITING_ACCEPTANCE" && handoffStatus != "IN_PROGRESS" {
			return apierr.Conflict("SC_PRIMARY_NOT_OPEN", "当前案件不能申请主责采购")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sourcing_procurement_participants
(tenant_id,case_id,employee_id,employee_name,participant_role,status,primary_requested_at)
VALUES($1,$2,$3,$4,'COLLABORATOR','ACTIVE',now())
ON CONFLICT (tenant_id,case_id,employee_id) DO UPDATE SET
employee_name=excluded.employee_name,status='ACTIVE',primary_requested_at=now(),updated_at=now()`, tenantID, caseID, op.ID, op.Name); err != nil {
			return err
		}
		q := s.q.WithTx(tx)
		if acceptedBy == nil {
			if err := setPrimaryBuyer(ctx, tx, tenantID, caseID, op.ID, op.Name); err != nil {
				return err
			}
			return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
				TenantID: tenantID, CaseID: caseID, Section: "PROCUREMENT_PARTICIPATION", Action: "PRIMARY_ASSIGNED",
				Summary: "采购主动成为主责采购", BeforeJson: []byte(`{}`), AfterJson: participantJSON(op.ID, op.Name), OperatorID: op.ID, OperatorName: op.Name,
			})
		}
		if *acceptedBy == op.ID {
			_, err := tx.Exec(ctx, `UPDATE sourcing_procurement_participants SET primary_requested_at=NULL,updated_at=now()
WHERE tenant_id=$1 AND case_id=$2 AND employee_id=$3`, tenantID, caseID, op.ID)
			return err
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: caseID, Section: "PROCUREMENT_PARTICIPATION", Action: "PRIMARY_REQUESTED",
			Summary: "采购申请成为主责采购", BeforeJson: []byte(`{}`), AfterJson: participantJSON(op.ID, op.Name), OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return nil, SourcingCaseView{}, err
	}
	participants, err := s.ListSourcingParticipants(ctx, tenantID, caseID)
	if err != nil {
		return nil, SourcingCaseView{}, err
	}
	view, err := s.GetSourcingCase(ctx, tenantID, caseID)
	return participants, view, err
}

// AssignPrimarySourcingCase is called only through the manager/Super permission route.
func (s *Service) AssignPrimarySourcingCase(ctx context.Context, tenantID, caseID, employeeID int64, reason string, op Operator) ([]SourcingParticipant, SourcingCaseView, error) {
	reason = strings.TrimSpace(reason)
	if employeeID == 0 {
		return nil, SourcingCaseView{}, apierr.Invalid("SC_PRIMARY_REQUIRED", "请选择参与采购作为主责")
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var oldID *int64
		var oldName, handoffStatus string
		if err := tx.QueryRow(ctx, `SELECT accepted_by,accepted_by_name,handoff_status FROM sourcing_cases
WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, caseID).Scan(&oldID, &oldName, &handoffStatus); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.NotFound("SC_NOT_FOUND", "询价案件不存在")
			}
			return err
		}
		if handoffStatus != "WAITING_ACCEPTANCE" && handoffStatus != "IN_PROGRESS" {
			return apierr.Conflict("SC_PRIMARY_NOT_OPEN", "当前案件不能指定主责采购")
		}
		if oldID != nil && *oldID != employeeID && reason == "" {
			return apierr.Invalid("SC_PRIMARY_CHANGE_REASON_REQUIRED", "更换主责采购必须填写原因")
		}
		var employeeName string
		if err := tx.QueryRow(ctx, `SELECT employee_name FROM sourcing_procurement_participants
WHERE tenant_id=$1 AND case_id=$2 AND employee_id=$3 AND status='ACTIVE'`, tenantID, caseID, employeeID).Scan(&employeeName); err != nil {
			if err == pgx.ErrNoRows {
				return apierr.Invalid("SC_PRIMARY_NOT_PARTICIPANT", "只能从当前参与采购中指定主责")
			}
			return err
		}
		if oldID != nil && *oldID == employeeID {
			return nil
		}
		if err := setPrimaryBuyer(ctx, tx, tenantID, caseID, employeeID, employeeName); err != nil {
			return err
		}
		before, _ := json.Marshal(map[string]any{"employeeId": oldID, "employeeName": oldName})
		after, _ := json.Marshal(map[string]any{"employeeId": employeeID, "employeeName": employeeName})
		action, summary := "PRIMARY_ASSIGNED", "经理指定主责采购"
		if oldID != nil {
			action, summary = "PRIMARY_CHANGED", "经理更换主责采购"
		}
		return s.q.WithTx(tx).CreateSourcingChange(ctx, store.CreateSourcingChangeParams{
			TenantID: tenantID, CaseID: caseID, Section: "PROCUREMENT_PARTICIPATION", Action: action,
			Summary: summary, BeforeJson: before, AfterJson: after, Reason: reason, OperatorID: op.ID, OperatorName: op.Name,
		})
	})
	if err != nil {
		return nil, SourcingCaseView{}, err
	}
	participants, err := s.ListSourcingParticipants(ctx, tenantID, caseID)
	if err != nil {
		return nil, SourcingCaseView{}, err
	}
	view, err := s.GetSourcingCase(ctx, tenantID, caseID)
	return participants, view, err
}

func setPrimaryBuyer(ctx context.Context, tx pgx.Tx, tenantID, caseID, employeeID int64, employeeName string) error {
	if _, err := tx.Exec(ctx, `UPDATE sourcing_procurement_participants SET participant_role='COLLABORATOR',updated_at=now()
WHERE tenant_id=$1 AND case_id=$2 AND participant_role='PRIMARY' AND employee_id<>$3`, tenantID, caseID, employeeID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE sourcing_procurement_participants SET participant_role='PRIMARY',status='ACTIVE',
primary_requested_at=NULL,updated_at=now() WHERE tenant_id=$1 AND case_id=$2 AND employee_id=$3`, tenantID, caseID, employeeID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE sourcing_cases SET status=CASE WHEN status='REVIEWING' THEN 'SOURCING' ELSE status END,
handoff_status=CASE WHEN handoff_status='WAITING_ACCEPTANCE' THEN 'IN_PROGRESS' ELSE handoff_status END,
accepted_by=$3,accepted_by_name=$4,accepted_at=now(),return_reason='',return_fields='{}',updated_at=now()
WHERE tenant_id=$1 AND id=$2`, tenantID, caseID, employeeID, employeeName)
	return err
}

func participantJSON(employeeID int64, employeeName string) []byte {
	out, _ := json.Marshal(map[string]any{"employeeId": employeeID, "employeeName": employeeName})
	return out
}
