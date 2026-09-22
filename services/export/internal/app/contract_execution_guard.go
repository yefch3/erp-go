package app

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (s *Service) CheckContractExecution(ctx context.Context, tenant, id int64) (bool, string, error) {
	actor, ok := grpcx.OperatorFromContext(ctx)
	if !ok || actor.TenantID <= 0 || actor.EmployeeID <= 0 {
		return false, "", apierr.Unauthorized("EX_ACTOR_REQUIRED", "请先登录")
	}
	if actor.TenantID != tenant {
		return false, "", apierr.Permission("EX_TENANT_MISMATCH", "租户不匹配")
	}
	if err := s.RequireAnyPermission(ctx, "export:contract:read", "procurement:order:write", "procurement:requirement:write", "shipping:schedule:write", "export:shipment:write"); err != nil {
		return false, "", err
	}
	if tenant <= 0 || id <= 0 {
		return false, "", apierr.Invalid("EX_CONTRACT_REQUIRED", "请关联合同")
	}
	var state, source string
	var released bool
	err := s.pool.QueryRow(ctx, `SELECT entry_source,status,condition_confirmed_at IS NOT NULL FROM contracts WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&source, &state, &released)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", apierr.NotFound("EX_CONTRACT_NOT_FOUND", "合同不存在或无权访问")
	}
	if err != nil {
		return false, "", err
	}
	if source == "HISTORICAL_RECORD" {
		return false, "历史合同仅供资料关联，请在对应模块建立业务单据", nil
	}
	switch state {
	case "PAUSED":
		return false, "外销合同已暂停，不能新增采购、订舱或发运", nil
	case "TERMINATING", "TERMINATED":
		return false, "外销合同正在终止或已终止，请仅处理已发生业务的善后", nil
	case "COMPLETED", "CANCELLED", "DELETED":
		return false, "外销合同已关闭，不能新增履约", nil
	case "EXECUTING", "EFFECTIVE":
	default:
		return false, "外销合同尚未完成确认和签署，不能新增履约", nil
	}
	if !released {
		return false, "请先由财务确认合同执行条件，再开展采购和物流实单", nil
	}
	return true, "", nil
}
