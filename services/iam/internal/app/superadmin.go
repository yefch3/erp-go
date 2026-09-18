package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// EnsureSuperAdminAccess repairs the one role whose meaning is fixed by the
// system: SUPER_ADMIN always has every feature permission and every ordinary
// business-data scope. Older migrations granted new permissions one by one,
// and the role editor used to allow removing them, so the role name could say
// “super administrator” while the database contained a partial role.
//
// Mail stays excluded from automatic ALL scope. Reading another employee's
// mailbox remains an explicit, audited decision; see superAdminScopeModules.
func (s *Service) EnsureSuperAdminAccess(ctx context.Context) error {
	tenants, err := s.q.ListTenantIDs(ctx)
	if err != nil {
		return fmt.Errorf("iam: list tenants for super admin repair: %w", err)
	}
	for _, tenantID := range tenants {
		if err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			roles, err := q.ListRoles(ctx, tenantID)
			if err != nil {
				return err
			}
			var superAdminID int64
			for _, role := range roles {
				if role.Code == "SUPER_ADMIN" {
					superAdminID = role.ID
					break
				}
			}
			if superAdminID == 0 {
				return fmt.Errorf("tenant %d has no active SUPER_ADMIN role", tenantID)
			}
			permissions, err := q.ListPermissions(ctx)
			if err != nil {
				return err
			}
			for _, permission := range permissions {
				if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{
					TenantID: tenantID, RoleID: superAdminID, PermissionID: permission.ID,
				}); err != nil {
					return err
				}
			}
			for _, module := range superAdminScopeModules {
				if err := q.SetRoleDataScope(ctx, store.SetRoleDataScopeParams{
					TenantID: tenantID, RoleID: superAdminID, Module: module,
					ScopeType: "ALL", CustomDeptIds: []int64{},
				}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("iam: repair super admin for tenant %d: %w", tenantID, err)
		}
	}
	return nil
}
