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

		// Warned about rather than refused. This account is seeded from the
		// deployment's environment before anybody can log in to fix it, so a
		// hard refusal here is a service that will not start — and the
		// operator standing up the system is the one person who cannot be
		// told about it through the system. A loud line in the startup log is
		// the channel that actually reaches them.
		if err := checkPasswordStrength(seed.InitialPassword, adminEmail, seed.CompanyName); err != nil {
			s.log.Warn("the seeded administrator password does not meet the policy every other account must",
				"admin", adminEmail, "reason", err.Error())
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
		// 数据范围，和权限是两件事：权限决定能用哪些功能，范围决定能看谁的
		// 单据。引导程序原来只给了前者，于是超管能打开每一个页面，却在每个
		// 页面上只看得见自己经手的那几张单——解析器对未配置的模块兜底 SELF。
		//
		// 采购几个模块看起来正常纯属巧合：它们的范围种子迁移（00031/00037/
		// 00041）用 FROM roles 无条件插入，跑的时候超管已经被引导创建出来了。
		// 而 export 的种子（00005）跑在引导之前，那时 roles 表还是空的。
		for _, module := range superAdminScopeModules {
			if err := q.SetRoleDataScope(ctx, store.SetRoleDataScopeParams{
				TenantID: tenantID, RoleID: role.ID, Module: module,
				ScopeType: "ALL", CustomDeptIds: []int64{},
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

// superAdminScopeModules 是引导时给超管铺开的数据范围。
//
// **mail 故意不在列内。** 邮件正文是这套系统里最私密的东西——客户的报价
// 往来、员工的私人通信都在里面。让超管看别人的邮箱应当是一个显式的、
// 有人负责的决定（在角色页上点出来，留下变更记录），不该由引导程序在
// 没人看着的时候默默给出。需要时管理员自己开，一次点击的事。
//
// 加新模块时记得同时补这里和订正迁移——漏了的症状是「管理员说他看不见
// 别人的单子」，而权限页上一切正常。
var superAdminScopeModules = []string{
	"export",
	"shipping",
	"procurement_order",
	"procurement_requirement",
	"procurement_sourcing",
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
