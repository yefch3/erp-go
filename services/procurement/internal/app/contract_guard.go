package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type ContractGuard interface {
	Check(context.Context, int64) error
}

func (s *Service) UseContractGuard(g ContractGuard) { s.contractGuard = g }
func (s *Service) checkContractExecution(ctx context.Context, tenant, orderID int64) error {
	if s.contractGuard == nil {
		return nil
	}
	contracts, err := purchaseOrderContractIDs(ctx, s.pool, tenant, orderID, nil)
	if err != nil {
		return err
	}
	return s.checkContracts(ctx, contracts)
}

type contractExecutionSource interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Delivery uses its current transaction and the persisted purchase sources.
// A receipt checks only the selected items; an order-wide action passes nil.
// Independent purchases have no contract to verify. Real linked contracts
// must fail closed if their execution status cannot be checked.
func (s *Service) checkDeliveryContractExecution(ctx context.Context, source contractExecutionSource, tenant, orderID int64, itemIDs []int64) error {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	contracts, err := purchaseOrderContractIDs(checkCtx, source, tenant, orderID, itemIDs)
	if err != nil {
		return err
	}
	if len(contracts) == 0 {
		return nil
	}
	if s.contractGuard == nil {
		return apierr.Conflict("PO_CONTRACT_EXECUTION_UNAVAILABLE", "合同执行核验服务未配置，不能新增交货")
	}
	return s.checkContracts(checkCtx, contracts)
}

func purchaseOrderContractIDs(ctx context.Context, source contractExecutionSource, tenant, orderID int64, itemIDs []int64) ([]int64, error) {
	rows, err := source.Query(ctx, `SELECT DISTINCT r.contract_id FROM purchase_order_items i
	 JOIN purchase_requirements r ON r.id=i.requirement_id AND r.tenant_id=i.tenant_id
	 WHERE i.tenant_id=$1 AND i.po_id=$2 AND r.contract_id>0
	 AND (coalesce(cardinality($3::bigint[]),0)=0 OR i.id=ANY($3)) ORDER BY r.contract_id`, tenant, orderID, itemIDs)
	if err != nil {
		return nil, err
	}
	var contracts []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		contracts = append(contracts, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return contracts, nil
}

func (s *Service) checkContracts(ctx context.Context, contracts []int64) error {
	for _, id := range contracts {
		if err := s.contractGuard.Check(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
