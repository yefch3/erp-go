package app

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/store"
)

// Withdrawal locks the same instance as Act. A completed decision wins over a
// later withdrawal; cancelling pending tasks does not erase prior decisions.
func (s *Service) Withdraw(ctx context.Context, tenant, id, actor int64, reason string) error {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.TenantID <= 0 || op.EmployeeID <= 0 {
		return apierr.Unauthorized("AP_ACTOR_REQUIRED", "请先登录")
	}
	if op.TenantID != tenant || op.EmployeeID != actor {
		return apierr.Permission("AP_ACTOR_MISMATCH", "操作人或租户不匹配")
	}
	reason = strings.TrimSpace(reason)
	var withdrawn store.ApprovalInstance
	var pendingAssignees []int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		inst, err := q.GetInstanceForUpdate(ctx, store.GetInstanceForUpdateParams{TenantID: tenant, ID: id})
		if err != nil {
			return err
		}
		allowed, err := s.canWithdrawInstance(ctx, inst, actor)
		if err != nil {
			return err
		}
		if !allowed {
			return apierr.Permission("AP_WITHDRAW_OWNER", "无权撤回这笔审批")
		}
		if reason == "" && inst.BizType != "PURCHASE_ORDER" {
			return apierr.Invalid("AP_WITHDRAW_REASON", "请填写撤回原因")
		}
		if inst.Status == "CANCELLED" {
			return nil
		}
		if inst.Status != "RUNNING" {
			return apierr.Conflict("AP_WITHDRAW_FINISHED", "审批已结束，不能撤回")
		}
		pendingAssignees, err = q.CancelInstanceTasks(ctx, store.CancelInstanceTasksParams{TenantID: tenant, InstanceID: id})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE approval_instances
			SET status='CANCELLED',finished_at=now(),
			    biz_summary=biz_summary||jsonb_strip_nulls(jsonb_build_object('withdrawal_reason',nullif($3::text,''),'withdrawn_by',$4::bigint))
			WHERE tenant_id=$1 AND id=$2`, tenant, id, reason, actor)
		withdrawn = inst
		return err
	})
	if err == nil && withdrawn.ID != 0 {
		if withdrawn.BizType == reimbursementBizType && slices.Contains(pendingAssignees, int64(0)) {
			if approvers, lookupErr := s.reimbursementApprovers(ctx); lookupErr == nil {
				pendingAssignees = approvers
			}
		}
		s.nudge(ctx, tenant, pendingAssignees, livefeed.TodoChanged, subjectOf(withdrawn))
		s.nudge(ctx, tenant, []int64{withdrawn.SubmitterID}, livefeed.DocChanged, subjectOf(withdrawn))
	}
	return err
}

func (s *Service) canWithdrawInstance(ctx context.Context, inst store.ApprovalInstance, actor int64) (bool, error) {
	if inst.SubmitterID == actor {
		return true, nil
	}
	if inst.BizType != "PURCHASE_ORDER" || s.dir == nil {
		return false, nil
	}
	for _, role := range []string{"PROCUREMENT_MANAGER", "BOSS", superAdminRoleCode} {
		members, found, err := s.dir.RoleMembersByCode(ctx, role)
		if err != nil {
			return false, err
		}
		if found && slices.Contains(members, actor) {
			return true, nil
		}
	}
	return false, nil
}

// A cancelled instance may still be attached to a pending order if the
// procurement service could not commit after approval cancelled its tasks.
func (s *Service) PurchaseOrderWithdrawalInstance(ctx context.Context, tenantID, orderID, actor int64) (int64, error) {
	instances, err := s.ListInstances(ctx, tenantID, "PURCHASE_ORDER", orderID)
	if err != nil || len(instances) == 0 {
		return 0, err
	}
	inst := instances[0]
	if inst.Status != statusRunning && inst.Status != "CANCELLED" {
		return 0, nil
	}
	allowed, err := s.canWithdrawInstance(ctx, inst, actor)
	if !allowed || err != nil {
		return 0, err
	}
	return inst.ID, nil
}
