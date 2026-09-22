package app

import "context"

type ContractGuard interface {
	Check(context.Context, int64) error
}

func (s *Service) UseContractGuard(g ContractGuard) { s.contractGuard = g }
func (s *Service) checkContractExecution(ctx context.Context, tenant, orderID int64) error {
	if s.contractGuard == nil {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT r.contract_id FROM purchase_order_items i JOIN purchase_requirements r ON r.id=i.requirement_id AND r.tenant_id=i.tenant_id WHERE i.tenant_id=$1 AND i.po_id=$2 AND r.contract_id>0`, tenant, orderID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if err := s.contractGuard.Check(ctx, id); err != nil {
			return err
		}
	}
	return rows.Err()
}
