package app

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/approval/internal/store"
	"strings"
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
	if reason == "" {
		return apierr.Invalid("AP_WITHDRAW_REASON", "请填写撤回原因")
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		inst, err := q.GetInstanceForUpdate(ctx, store.GetInstanceForUpdateParams{TenantID: tenant, ID: id})
		if err != nil {
			return err
		}
		if inst.SubmitterID != actor {
			return apierr.Permission("AP_WITHDRAW_OWNER", "只有原提交人可以撤回审批")
		}
		if inst.Status == "CANCELLED" {
			return nil
		}
		if inst.Status != "RUNNING" {
			return apierr.Conflict("AP_WITHDRAW_FINISHED", "审批已结束，不能撤回")
		}
		if _, err = q.CancelInstanceTasks(ctx, store.CancelInstanceTasksParams{TenantID: tenant, InstanceID: id}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE approval_instances SET status='CANCELLED',finished_at=now(),biz_summary=biz_summary||jsonb_build_object('withdrawal_reason',$3::text) WHERE tenant_id=$1 AND id=$2`, tenant, id, reason)
		return err
	})
}
