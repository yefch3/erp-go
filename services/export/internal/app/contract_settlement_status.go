package app

import (
	"context"
)

// Called only after GetContractFor has applied the owner's contract data scope.
// A zero balance alone does not settle an account: Finance must confirm it.
func (s *Service) ContractReceiptSettlementStatus(ctx context.Context, tenant, id int64) (string, error) {
	var closed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM contract_receivable_closures WHERE tenant_id=$1 AND contract_id=$2 AND revoked_at IS NULL)`, tenant, id).Scan(&closed)
	if err != nil {
		return "", err
	}
	if closed {
		return "SETTLED", nil
	}
	return "PENDING", nil
}
