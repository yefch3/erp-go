package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type ContractGuard interface {
	Check(context.Context, int64) error
}

func (s *Service) UseContractGuard(g ContractGuard) { s.contractGuard = g }
func (s *Service) checkHandoffExecution(ctx context.Context, tenant, id int64) error {
	if s.contractGuard == nil {
		return nil
	}
	var contractID int64
	var current bool
	if err := s.pool.QueryRow(ctx, `SELECT h.contract_id,h.version_no=(SELECT max(version_no) FROM contract_shipping_handoffs WHERE tenant_id=h.tenant_id AND contract_id=h.contract_id) FROM contract_shipping_handoffs h WHERE h.tenant_id=$1 AND h.id=$2`, tenant, id).Scan(&contractID, &current); err != nil {
		return err
	}
	if !current {
		return apierr.Conflict("SHIPPING_CONTRACT_VERSION_CHANGED", "合同已变更，请从最新版本物流任务继续办理")
	}
	return s.contractGuard.Check(ctx, contractID)
}
