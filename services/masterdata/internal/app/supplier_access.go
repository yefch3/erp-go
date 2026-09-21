package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type supplierAccessKey struct{}

// WithSupplierAccess is set by the authenticated RPC boundary. Zero means the
// existing SUPER_ADMIN role; a positive ID means that employee's owner rows.
func WithSupplierAccess(ctx context.Context, employeeID int64) context.Context {
	return context.WithValue(ctx, supplierAccessKey{}, employeeID)
}

func supplierAccessEmployee(ctx context.Context) int64 {
	id, _ := ctx.Value(supplierAccessKey{}).(int64)
	return id
}

func supplierOwnerDisplayName(employeeID int64, name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return fmt.Sprintf("员工 %d", employeeID)
}

func (s *Service) AuthorizeSupplier(ctx context.Context, tenantID, supplierID int64) error {
	employeeID := supplierAccessEmployee(ctx)
	if employeeID == 0 {
		return nil
	}
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM supplier_owners WHERE tenant_id=$1 AND supplier_id=$2
		AND employee_id=$3 AND status='ACTIVE'
		AND (start_date IS NULL OR start_date <= CURRENT_DATE)
		AND (end_date IS NULL OR end_date >= CURRENT_DATE))`, tenantID, supplierID, employeeID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在或无权访问")
	}
	return nil
}
