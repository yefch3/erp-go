package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// EnsureAdmin bootstraps an empty database with a HQ department, an admin
// employee holding every permission, and its login account. It is a no-op
// once any user exists, so it runs unconditionally at startup.
func (s *Service) EnsureAdmin(ctx context.Context, tenantID int64, initialPassword string) error {
	n, err := s.q.CountUsers(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("bootstrap: count users: %w", err)
	}
	if n > 0 {
		return nil
	}

	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		dept, err := q.CreateDepartment(ctx, store.CreateDepartmentParams{
			TenantID: tenantID, Code: "HQ", Name: "总部", Path: "/", Level: 1,
		})
		if err != nil {
			return err
		}
		if err := q.SetDepartmentPath(ctx, store.SetDepartmentPathParams{
			TenantID: tenantID, ID: dept.ID, Path: fmt.Sprintf("/%d/", dept.ID), Level: 1,
		}); err != nil {
			return err
		}

		emp, err := q.CreateEmployee(ctx, store.CreateEmployeeParams{
			TenantID: tenantID, Code: "ADMIN", Name: "系统管理员", DepartmentID: dept.ID,
		})
		if err != nil {
			return err
		}

		hash, err := HashPassword(initialPassword)
		if err != nil {
			return err
		}
		if _, err := q.CreateUser(ctx, store.CreateUserParams{
			TenantID: tenantID, EmployeeID: emp.ID, Username: "admin", PasswordHash: hash,
		}); err != nil {
			return err
		}

		role, err := q.CreateRole(ctx, store.CreateRoleParams{
			TenantID: tenantID, Code: "SUPER_ADMIN", Name: "超级管理员",
			Description: "系统引导创建，持有全部权限",
		})
		if err != nil {
			return err
		}
		perms, err := q.ListPermissions(ctx)
		if err != nil {
			return err
		}
		for _, p := range perms {
			if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{
				TenantID: tenantID, RoleID: role.ID, PermissionID: p.ID,
			}); err != nil {
				return err
			}
		}
		if err := q.AddEmployeeRole(ctx, store.AddEmployeeRoleParams{
			TenantID: tenantID, EmployeeID: emp.ID, RoleID: role.ID,
		}); err != nil {
			return err
		}

		s.log.Info("bootstrap: admin account created",
			"username", "admin", "employee_id", emp.ID, "permissions", len(perms))
		return nil
	})
}
