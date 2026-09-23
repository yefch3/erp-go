package app

import (
	"context"

	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

// Imported or manually assigned supplier codes do not advance number_sequences.
// Skip those occupied codes when assigning the next automatic code. Keep the
// sequence update and the existence check in the caller's create transaction.
func (s *Service) nextAvailableSupplierCode(ctx context.Context, q *store.Queries, tenantID int64) (string, error) {
	for {
		code, err := s.nextNumber(ctx, q, tenantID, "SUPPLIER")
		if err != nil {
			return "", err
		}
		exists, err := q.SupplierCodeExists(ctx, store.SupplierCodeExistsParams{TenantID: tenantID, Code: code})
		if err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
}
