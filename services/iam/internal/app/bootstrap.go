package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// SeedTenant holds what an empty database needs to become one company's.
type SeedTenant struct {
	CompanyName     string
	MailDomains     string // comma-separated
	AdminEmail      string
	InitialPassword string
}

// EnsureAdmin bootstraps an empty database with a tenant, its mail domains, a
// HQ department, an admin employee holding every permission, and its login
// account. It is a no-op once any user exists, so it runs at every startup.
//
// This is the one account that cannot be invited, and it is worth being exact
// about why. Every mail this system sends goes out through some employee's own
// bound mailbox; the first employee has none, so there is no channel to send
// an invitation down. The chain has to be started by hand and then runs on its
// own: this admin binds their mailbox, and from that moment every other
// invitation is sent from it.
//
// It is also the one place email_verified_at is stamped without a mail being
// answered. The proof is different in kind rather than absent — whoever runs
// this seed is the operator standing up the system for that company, and they
// are asserting the address. Nobody else gets that shortcut.
func (s *Service) EnsureAdmin(ctx context.Context, tenantID int64, seed SeedTenant) error {
	exists, err := s.q.HasAnyUser(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("bootstrap: probe users: %w", err)
	}
	if exists {
		return nil
	}
	adminEmail := strings.ToLower(strings.TrimSpace(seed.AdminEmail))
	domains := splitDomains(seed.MailDomains)
	if adminEmail == "" || len(domains) == 0 {
		return fmt.Errorf("bootstrap: COMPANY_MAIL_DOMAINS and ADMIN_EMAIL are both required")
	}
	// Refusing rather than repairing. An admin address outside the company's
	// own domains would be an account nobody could reproduce through the
	// ordinary path, and a seed that quietly widened the rule it is seeding
	// would be the worst possible place to be lenient.
	if !domainAllowed(adminEmail, domains) {
		return fmt.Errorf("bootstrap: ADMIN_EMAIL %q is not on any of %v", adminEmail, domains)
	}

	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)

		ten, err := q.CreateTenant(ctx, seed.CompanyName)
		if err != nil {
			return err
		}
		// Ignoring the tenant id the sequence just handed us in favour of the
		// caller's would be a lie waiting to be found; use what was created.
		tenantID = ten.ID
		for _, d := range domains {
			if err := q.AddTenantDomain(ctx, store.AddTenantDomainParams{
				Domain: d, TenantID: tenantID,
			}); err != nil {
				return err
			}
		}

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
		// The address is the login identity, and stamping it verified here is
		// the shortcut described above.
		if err := q.SetEmployeeEmailVerified(ctx, store.SetEmployeeEmailVerifiedParams{
			TenantID: tenantID, ID: emp.ID, Email: adminEmail,
		}); err != nil {
			return err
		}

		hash, err := HashPassword(seed.InitialPassword)
		if err != nil {
			return err
		}
		if _, err := q.CreateUser(ctx, store.CreateUserParams{
			// username is no longer the login identity — the address is — but
			// the column is NOT NULL UNIQUE and other account paths still
			// write it. Holding the address keeps it unambiguous until those
			// paths are reworked and the column can go.
			TenantID: tenantID, EmployeeID: emp.ID, Username: adminEmail, PasswordHash: hash,
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

		s.log.Info("bootstrap: tenant and admin created",
			"tenant", ten.Name, "tenant_id", tenantID, "domains", domains,
			"admin", adminEmail, "employee_id", emp.ID, "permissions", len(perms))
		return nil
	})
}

// splitDomains turns the configured list into lower-cased entries, dropping
// blanks so a trailing comma is not a domain named "".
func splitDomains(csv string) []string {
	out := []string{}
	for _, part := range strings.Split(csv, ",") {
		if d := strings.ToLower(strings.TrimSpace(part)); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func domainAllowed(email string, domains []string) bool {
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}
	host := email[at+1:]
	for _, d := range domains {
		if host == d {
			return true
		}
	}
	return false
}
