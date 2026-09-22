package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/contractflow"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/outbox"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
	"strings"
	"time"
)

type WorkflowCommand struct {
	ID             int64
	Action, Reason string
	Data           json.RawMessage
	Revision       int64
}
type WorkflowAction struct {
	Action string          `json:"action"`
	Reason string          `json:"reason"`
	Actor  string          `json:"actor"`
	At     string          `json:"at"`
	Data   json.RawMessage `json:"data"`
}
type WorkflowView struct {
	History  contractflow.History `json:"history"`
	Revision int64                `json:"revision"`
	Actions  []WorkflowAction     `json:"actions"`
}

func (s *Service) ContractWorkflow(ctx context.Context, tenant int64, cmd WorkflowCommand, op Operator) (any, error) {
	actor, ok := grpcx.OperatorFromContext(ctx)
	if !ok || actor.TenantID <= 0 || actor.EmployeeID <= 0 {
		return nil, apierr.Unauthorized("EX_ACTOR_REQUIRED", "请先登录")
	}
	if actor.TenantID != tenant || actor.EmployeeID != op.ID {
		return nil, apierr.Permission("EX_ACTOR_MISMATCH", "操作身份或租户不匹配")
	}
	permission := "export:contract:write"
	if cmd.Action == "get" {
		permission = "export:contract:read"
	}
	if err := s.RequireAnyPermission(ctx, permission); err != nil {
		return nil, err
	}
	if cmd.Action != "get" && !strings.HasPrefix(cmd.Action, "history_") && strings.TrimSpace(cmd.Reason) == "" {
		return nil, apierr.Invalid("EX_ACTION_REASON", "请填写操作原因或核对说明")
	}
	if strings.HasPrefix(cmd.Action, "history_") {
		return s.historyDraft(ctx, tenant, cmd, op)
	}
	view, err := s.GetContractFor(ctx, tenant, cmd.ID, 0, op)
	if err != nil {
		return nil, err
	}
	if cmd.Action == "get" {
		return s.workflowView(ctx, tenant, cmd.ID)
	}
	if err := s.RequireAnyPermission(ctx, "export:contract:write"); err != nil {
		return nil, err
	}
	if err := s.mustOwnContract(ctx, op, view); err != nil {
		return nil, err
	}
	if view.Contract.EntrySource == "HISTORICAL_RECORD" {
		return nil, apierr.Conflict("EX_HISTORY_RECORD_ONLY", "历史合同是资料记录，不走新单审批或履约操作")
	}

	if cmd.Action == "terminate" {
		return s.requestTermination(ctx, tenant, cmd, op, view)
	}
	if cmd.Action == "withdraw" {
		return s.withdrawContract(ctx, tenant, cmd, op, view)
	}
	reason := strings.TrimSpace(cmd.Reason)
	if reason == "" {
		return nil, apierr.Invalid("EX_ACTION_REASON", "请填写操作原因或核对说明")
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state string
		var signed bool
		var quoteID, ownerID int64
		if err := tx.QueryRow(ctx, `SELECT status,signed_at IS NOT NULL,coalesce(quotation_id,0),sales_employee_id FROM contracts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, cmd.ID).Scan(&state, &signed, &quoteID, &ownerID); err != nil {
			return err
		}
		if ownerID != op.ID {
			return apierr.Permission("EX_CONTRACT_NOT_OWNER", "合同负责人已变化，请刷新")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO contract_workflows(tenant_id,contract_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, tenant, cmd.ID); err != nil {
			return err
		}
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM contract_workflows WHERE tenant_id=$1 AND contract_id=$2`, tenant, cmd.ID).Scan(&revision); err != nil {
			return err
		}
		if revision != cmd.Revision {
			return apierr.Conflict("EX_WORKFLOW_REVISION", "合同已更新，请刷新后重新操作")
		}
		var next string
		switch cmd.Action {
		case "pause":
			if state != "EXECUTING" && state != "EFFECTIVE" {
				return apierr.Conflict("EX_PAUSE_STATUS", "只有执行中的合同可以暂停")
			}
			next = "PAUSED"
		case "resume":
			if state != "PAUSED" {
				return apierr.Conflict("EX_RESUME_STATUS", "只有暂停中的合同可以恢复")
			}
			next = "EXECUTING"
		case "void":
			if signed || (state != "DRAFT" && state != "PENDING_SIGN" && state != "REJECTED") {
				return apierr.Conflict("EX_VOID_STATUS", "只有尚未签署且不在审批中的合同可以作废")
			}
			next = "CANCELLED"
		case "delete":
			if state != "DRAFT" || signed || quoteID != 0 {
				return apierr.Conflict("EX_DELETE_STATUS", "只能删除尚未提交的手工合同草稿")
			}
			var submitted bool
			if err := tx.QueryRow(ctx, `SELECT approval_instance_id<>0 OR approval_request_key<>'' OR EXISTS(SELECT 1 FROM contract_workflow_actions WHERE tenant_id=$1 AND contract_id=$2 AND action='withdraw') FROM contracts WHERE tenant_id=$1 AND id=$2`, tenant, cmd.ID).Scan(&submitted); err != nil {
				return err
			}
			if submitted {
				return apierr.Conflict("EX_DELETE_SUBMITTED", "提交过的合同请使用作废，保留办理记录")
			}
			next = "DELETED"
		case "complete":
			if state != "EXECUTING" && state != "EFFECTIVE" && state != "TERMINATED" {
				return apierr.Conflict("EX_CLOSE_STATUS", "只有执行中或已终止的合同可以结案")
			}
			var checklist struct{ Procurement, Logistics, Finance bool }
			if err := json.Unmarshal(cmd.Data, &checklist); err != nil || !checklist.Procurement || !checklist.Logistics || !checklist.Finance {
				return apierr.Invalid("EX_CLOSE_CHECKLIST", "请核对采购、物流和财务善后事项，并全部确认完成")
			}
			next = "COMPLETED"
		default:
			return apierr.Invalid("EX_WORKFLOW_ACTION", "不支持的合同操作")
		}
		if _, err := tx.Exec(ctx, `UPDATE contracts SET status=$3::varchar,completed_at=CASE WHEN $3::varchar='COMPLETED' THEN now() ELSE completed_at END,updated_at=now(),updated_by=$4 WHERE tenant_id=$1 AND id=$2`, tenant, cmd.ID, next, op.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE contract_workflows SET revision=revision+1 WHERE tenant_id=$1 AND contract_id=$2`, tenant, cmd.ID); err != nil {
			return err
		}
		if err := recordWorkflowAction(ctx, tx, tenant, cmd.ID, cmd.Action, reason, cmd.Data, op); err != nil {
			return err
		}
		if next != state {
			return emitContractControl(ctx, tx, tenant, cmd.ID, next, reason)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.workflowView(ctx, tenant, cmd.ID)
}

func recordWorkflowAction(ctx context.Context, tx pgx.Tx, tenant, id int64, action, reason string, data json.RawMessage, op Operator) error {
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	_, err := tx.Exec(ctx, `INSERT INTO contract_workflow_actions(tenant_id,contract_id,action,reason,data,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, id, action, reason, []byte(data), op.ID, op.Name)
	return err
}
func emitContractControl(ctx context.Context, tx pgx.Tx, tenant, id int64, state, reason string) error {
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM contract_workflows WHERE tenant_id=$1 AND contract_id=$2`, tenant, id).Scan(&revision); err != nil {
		return err
	}
	raw, _ := json.Marshal(map[string]any{"contract_id": id, "status": state, "reason": reason, "revision": revision})
	return outbox.Append(ctx, tx, outbox.Event{TenantID: tenant, AggregateType: "contract", AggregateID: fmt.Sprint(id), EventType: "ContractControlChanged", Payload: raw})
}
func (s *Service) workflowView(ctx context.Context, tenant, id int64) (WorkflowView, error) {
	out := WorkflowView{Revision: 1, Actions: []WorkflowAction{}}
	var history []byte
	err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT history FROM contract_workflows WHERE tenant_id=$1 AND contract_id=$2),'{}'),coalesce((SELECT revision FROM contract_workflows WHERE tenant_id=$1 AND contract_id=$2),1)`, tenant, id).Scan(&history, &out.Revision)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(history, &out.History); err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT action,reason,actor_name,created_at::text,data FROM contract_workflow_actions WHERE tenant_id=$1 AND contract_id=$2 ORDER BY id DESC`, tenant, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a WorkflowAction
		if err := rows.Scan(&a.Action, &a.Reason, &a.Actor, &a.At, &a.Data); err != nil {
			return out, err
		}
		out.Actions = append(out.Actions, a)
	}
	return out, rows.Err()
}

func (s *Service) historyDraft(ctx context.Context, tenant int64, cmd WorkflowCommand, op Operator) (any, error) {
	if err := s.RequireAnyPermission(ctx, "export:contract:write"); err != nil {
		return nil, err
	}
	if cmd.Action == "history_list" {
		rows, err := s.pool.Query(ctx, `SELECT id,revision,body,updated_at::text FROM contract_history_drafts WHERE tenant_id=$1 AND owner_id=$2 AND contract_id IS NULL ORDER BY updated_at DESC`, tenant, op.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, rev int64
			var body json.RawMessage
			var at string
			if err := rows.Scan(&id, &rev, &body, &at); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{"id": fmt.Sprint(id), "revision": rev, "body": body, "updatedAt": at})
		}
		return out, rows.Err()
	}
	if cmd.Action == "history_delete" {
		tag, err := s.pool.Exec(ctx, `DELETE FROM contract_history_drafts WHERE tenant_id=$1 AND id=$2 AND owner_id=$3 AND revision=$4 AND contract_id IS NULL`, tenant, cmd.ID, op.ID, cmd.Revision)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, apierr.Conflict("EX_HISTORY_REVISION", "草稿已变化或已接入，不能删除")
		}
		return map[string]bool{"deleted": true}, nil
	}
	if cmd.Action != "history_save" || !json.Valid(cmd.Data) {
		return nil, apierr.Invalid("EX_HISTORY_DRAFT", "草稿资料无效")
	}
	if len(cmd.Data) > 2*1024*1024 {
		return nil, apierr.Invalid("EX_HISTORY_SIZE", "草稿资料过大")
	}
	var id, rev int64
	if cmd.ID == 0 {
		err := s.pool.QueryRow(ctx, `INSERT INTO contract_history_drafts(tenant_id,owner_id,body) VALUES($1,$2,$3) RETURNING id,revision`, tenant, op.ID, []byte(cmd.Data)).Scan(&id, &rev)
		return map[string]any{"id": fmt.Sprint(id), "revision": rev}, err
	}
	// An import response can be lost after commit. Reopening the same owned
	// draft returns that contract instead of making a second import or overwriting it.
	var linked int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(contract_id,0) FROM contract_history_drafts WHERE tenant_id=$1 AND id=$2 AND owner_id=$3`, tenant, cmd.ID, op.ID).Scan(&linked)
	if err != nil {
		return nil, apierr.NotFound("EX_HISTORY_DRAFT", "草稿不存在或无权访问")
	}
	if linked > 0 {
		if _, err := s.GetContractFor(ctx, tenant, linked, 0, op); err != nil {
			return nil, err
		}
		return map[string]any{"id": fmt.Sprint(cmd.ID), "revision": cmd.Revision, "contractId": fmt.Sprint(linked)}, nil
	}
	err = s.pool.QueryRow(ctx, `UPDATE contract_history_drafts SET body=$4,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND owner_id=$3 AND revision=$5 AND contract_id IS NULL RETURNING id,revision`, tenant, cmd.ID, op.ID, []byte(cmd.Data), cmd.Revision).Scan(&id, &rev)
	if err == pgx.ErrNoRows {
		return nil, apierr.Conflict("EX_HISTORY_REVISION", "草稿已更新或已接入，请重新打开")
	}
	return map[string]any{"id": fmt.Sprint(id), "revision": rev}, err
}

type approvalWithdrawer interface {
	Withdraw(context.Context, int64, string) error
}

func (s *Service) withdrawContract(ctx context.Context, tenant int64, cmd WorkflowCommand, op Operator, view ContractView) (any, error) {
	if view.Contract.Status != "PENDING_APPROVAL" {
		return nil, apierr.Conflict("EX_WITHDRAW_STATUS", "只有待上级确认的合同可以撤回")
	}
	a, ok := s.approvals.(approvalWithdrawer)
	if !ok {
		return nil, apierr.Invalid("EX_WITHDRAW_UNAVAILABLE", "审批撤回服务不可用")
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state string
		var instance, ownerID int64
		if err := tx.QueryRow(ctx, `SELECT status,approval_instance_id,sales_employee_id FROM contracts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, cmd.ID).Scan(&state, &instance, &ownerID); err != nil {
			return err
		}
		if ownerID != op.ID {
			return apierr.Permission("EX_CONTRACT_NOT_OWNER", "合同负责人已变化，请刷新")
		}
		if state != "PENDING_APPROVAL" {
			return apierr.Conflict("EX_WITHDRAW_CHANGED", "审批状态已变化，请刷新")
		}
		if err := checkWorkflowRevision(ctx, tx, tenant, cmd.ID, cmd.Revision); err != nil {
			return err
		}
		if err := a.Withdraw(ctx, instance, cmd.Reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE contracts SET status='DRAFT',approval_request_key='',approval_instance_id=0,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, cmd.ID); err != nil {
			return err
		}
		if err := s.q.WithTx(tx).SetContractVersionStatus(ctx, store.SetContractVersionStatusParams{TenantID: tenant, ID: view.Version.ID, NewStatus: "DRAFT"}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE contract_workflows SET revision=revision+1 WHERE tenant_id=$1 AND contract_id=$2`, tenant, cmd.ID); err != nil {
			return err
		}
		return recordWorkflowAction(ctx, tx, tenant, cmd.ID, "withdraw", cmd.Reason, nil, op)
	})
	if err != nil {
		return nil, err
	}
	return s.workflowView(ctx, tenant, cmd.ID)
}

// Save a stable request before crossing the service boundary. If approval accepts
// but the local commit fails, a retry and the callback still refer to this attempt.
func (s *Service) requestTermination(ctx context.Context, tenant int64, cmd WorkflowCommand, op Operator, view ContractView) (any, error) {
	if s.approvals == nil {
		return nil, apierr.Invalid("EX_APPROVAL_UNAVAILABLE", "审批服务不可用")
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state string
		var owner int64
		if err := tx.QueryRow(ctx, `SELECT status,sales_employee_id FROM contracts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, cmd.ID).Scan(&state, &owner); err != nil {
			return err
		}
		if owner != op.ID {
			return apierr.Permission("EX_CONTRACT_NOT_OWNER", "只有负责销售可以操作")
		}
		if state == "TERMINATING" {
			return nil
		}
		if state != "EXECUTING" && state != "EFFECTIVE" && state != "PAUSED" {
			return apierr.Conflict("EX_TERMINATE_STATUS", "只有执行中或暂停中的合同可以申请终止")
		}
		if err := checkWorkflowRevision(ctx, tx, tenant, cmd.ID, cmd.Revision); err != nil {
			return err
		}
		var current bool
		if err := tx.QueryRow(ctx, `SELECT current_version_id=(SELECT id FROM contract_versions WHERE tenant_id=$1 AND contract_id=$2 ORDER BY version_no DESC LIMIT 1) FROM contracts WHERE tenant_id=$1 AND id=$2`, tenant, cmd.ID).Scan(&current); err != nil {
			return err
		}
		if !current {
			return apierr.Conflict("EX_CHANGE_IN_FLIGHT", "请先处理尚未完成的合同变更")
		}
		key := fmt.Sprintf("terminate:%d:%d:%d", tenant, cmd.ID, time.Now().UnixNano())
		if _, err := tx.Exec(ctx, `UPDATE contract_workflows SET previous_status=$3,termination_request_key=$4,termination_instance_id=0,termination_reason=$5,revision=revision+1 WHERE tenant_id=$1 AND contract_id=$2`, tenant, cmd.ID, state, key, strings.TrimSpace(cmd.Reason)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE contracts SET status='TERMINATING',updated_at=now(),updated_by=$3 WHERE tenant_id=$1 AND id=$2`, tenant, cmd.ID, op.ID); err != nil {
			return err
		}
		if err := recordWorkflowAction(ctx, tx, tenant, cmd.ID, "terminate", cmd.Reason, nil, op); err != nil {
			return err
		}
		return emitContractControl(ctx, tx, tenant, cmd.ID, "TERMINATING", cmd.Reason)
	})
	if err != nil {
		return nil, err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state, key, reason string
		var instance int64
		if err := tx.QueryRow(ctx, `SELECT c.status,w.termination_request_key,w.termination_reason,w.termination_instance_id FROM contracts c JOIN contract_workflows w ON w.tenant_id=c.tenant_id AND w.contract_id=c.id WHERE c.tenant_id=$1 AND c.id=$2 FOR UPDATE OF c`, tenant, cmd.ID).Scan(&state, &key, &reason, &instance); err != nil {
			return err
		}
		if state != "TERMINATING" || instance > 0 {
			return nil
		}
		summary, _ := json.Marshal(map[string]any{"approval_request_key": key, "operation": "终止合同", "reason": reason, "contract_no": view.Contract.ContractNo, "customer_name": view.Contract.CustomerName, "total_amount": view.Version.TotalAmount})
		instance, err := s.approvals.Submit(ctx, ApprovalSubmission{BizType: BizTypeContract, BizID: cmd.ID, BizNo: view.Contract.ContractNo, Summary: string(summary), SubmitterID: op.ID, SubmitterName: op.Name, Amount: view.Version.BaseAmount})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE contract_workflows SET termination_instance_id=$3 WHERE tenant_id=$1 AND contract_id=$2`, tenant, cmd.ID, instance)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.workflowView(ctx, tenant, cmd.ID)
}

// Caller holds the contract row lock so every action uses one revision sequence.
func checkWorkflowRevision(ctx context.Context, tx pgx.Tx, tenant, id, expected int64) error {
	if _, err := tx.Exec(ctx, `INSERT INTO contract_workflows(tenant_id,contract_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, tenant, id); err != nil {
		return err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM contract_workflows WHERE tenant_id=$1 AND contract_id=$2`, tenant, id).Scan(&revision); err != nil {
		return err
	}
	if revision != expected {
		return apierr.Conflict("EX_WORKFLOW_REVISION", "合同已更新，请刷新后重新操作")
	}
	return nil
}

func (s *Service) applyTerminationDecision(ctx context.Context, tx pgx.Tx, tenant, id int64, result string, proof ApprovalProof) (bool, string, error) {
	if !strings.HasPrefix(proof.RequestKey, "terminate:") {
		return false, "", nil
	}
	var key, state, previous string
	var instance int64
	err := tx.QueryRow(ctx, `SELECT c.status,w.termination_request_key,w.previous_status,w.termination_instance_id FROM contracts c JOIN contract_workflows w ON w.contract_id=c.id AND w.tenant_id=c.tenant_id WHERE c.tenant_id=$1 AND c.id=$2 FOR UPDATE OF c`, tenant, id).Scan(&state, &key, &previous, &instance)
	if err != nil {
		return true, "", err
	}
	if state != "TERMINATING" || key != proof.RequestKey || proof.InstanceID <= 0 || (instance > 0 && instance != proof.InstanceID) {
		return true, state, nil
	}
	next := previous
	if result == "APPROVED" {
		next = "TERMINATED"
	} else if result != "RETURNED" && result != "REJECTED" {
		return true, "", apierr.Invalid("EX_APPROVAL_RESULT", "审批结果无效")
	}
	if _, err = tx.Exec(ctx, `UPDATE contracts SET status=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, id, next); err != nil {
		return true, "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE contract_workflows SET revision=revision+1 WHERE tenant_id=$1 AND contract_id=$2`, tenant, id); err != nil {
		return true, "", err
	}
	if err = recordWorkflowAction(ctx, tx, tenant, id, "termination_"+strings.ToLower(result), "终止申请审批结果", nil, Operator{Name: "上级确认"}); err != nil {
		return true, "", err
	}
	return true, next, emitContractControl(ctx, tx, tenant, id, next, "终止申请审批结果")
}
