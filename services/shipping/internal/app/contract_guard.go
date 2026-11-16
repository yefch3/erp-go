package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

type ContractGuard interface {
	Check(context.Context, int64) error
}

func (s *Service) UseContractGuard(g ContractGuard) { s.contractGuard = g }

// Recheck the live sales contract at the first physical movement, even when a
// schedule was planned while it was still executable. Later voyage updates
// describe goods that already left and remain available for settlement.
func (s *Service) checkDispatchContract(ctx context.Context, tx pgx.Tx, current store.ShippingSchedule, contractID int64) error {
	dispatched, err := recordedDispatch(ctx, tx, current)
	if err != nil || dispatched {
		return err
	}
	if s.contractGuard == nil {
		return apierr.Conflict("SHIPPING_CONTRACT_EXECUTION_UNAVAILABLE", "合同履约核验未配置，不能确认首次发运")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.contractGuard.Check(checkCtx, contractID)
}

func (s *Service) checkHandoffExecution(ctx context.Context, tenant, id int64) error {
	if s.contractGuard == nil {
		return nil
	}
	var contractID, linkedContractID int64
	var current, manual bool
	if err := s.pool.QueryRow(ctx, `SELECT m.handoff_id IS NOT NULL,h.contract_id,coalesce(m.linked_contract_id,0),h.version_no=(SELECT max(version_no) FROM contract_shipping_handoffs WHERE tenant_id=h.tenant_id AND contract_id=h.contract_id)
	 FROM contract_shipping_handoffs h LEFT JOIN manual_shipping_orders m ON m.tenant_id=h.tenant_id AND m.handoff_id=h.id
	 WHERE h.tenant_id=$1 AND h.id=$2`, tenant, id).Scan(&manual, &contractID, &linkedContractID, &current); err != nil {
		return err
	}
	// A manually recorded contract number is not yet a sales contract link.
	if manual && contractID == 0 {
		if linkedContractID <= 0 {
			return nil
		}
		// A linked standalone order keeps its own version namespace. Its
		// real sales contract still controls new delegation and scheduling.
		contractID = linkedContractID
	} else if !current {
		return apierr.Conflict("SHIPPING_CONTRACT_VERSION_CHANGED", "合同已变更，请从最新版本物流任务继续办理")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.contractGuard.Check(checkCtx, contractID)
}
