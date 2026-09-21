package app

import (
	"context"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type customerAccessKey struct{}

// WithCustomerAccess is set by the authenticated RPC boundary. Zero means the
// existing SUPER_ADMIN role; a positive ID means that employee's owner rows.
func WithCustomerAccess(ctx context.Context, employeeID int64) context.Context {
	return context.WithValue(ctx, customerAccessKey{}, employeeID)
}

func customerAccessEmployee(ctx context.Context) int64 {
	id, _ := ctx.Value(customerAccessKey{}).(int64)
	return id
}

func (s *Service) AuthorizeCustomer(ctx context.Context, tenantID, customerID int64) error {
	employeeID := customerAccessEmployee(ctx)
	if employeeID == 0 {
		return nil
	}
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM customer_owners WHERE tenant_id=$1 AND customer_id=$2
		AND employee_id=$3 AND status='ACTIVE'
		AND (start_date IS NULL OR start_date <= CURRENT_DATE)
		AND (end_date IS NULL OR end_date >= CURRENT_DATE))`, tenantID, customerID, employeeID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在或无权访问")
	}
	return nil
}
