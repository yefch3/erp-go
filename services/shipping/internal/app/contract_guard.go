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
	var current, manual bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM manual_shipping_orders m WHERE m.tenant_id=h.tenant_id AND m.handoff_id=h.id),h.contract_id,h.version_no=(SELECT max(version_no) FROM contract_shipping_handoffs WHERE tenant_id=h.tenant_id AND contract_id=h.contract_id) FROM contract_shipping_handoffs h WHERE h.tenant_id=$1 AND h.id=$2`, tenant, id).Scan(&manual, &contractID, &current); err != nil {
		return err
	}
	// A manually recorded contract number is not yet a sales contract link.
	if manual && contractID == 0 {
		return nil
	}
	if !current {
		return apierr.Conflict("SHIPPING_CONTRACT_VERSION_CHANGED", "合同已变更，请从最新版本物流任务继续办理")
	}
	return s.contractGuard.Check(ctx, contractID)
}
