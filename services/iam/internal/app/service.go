// Package app holds the iam use cases. It depends on the sqlc store and
// pkg libraries only; gRPC types never reach this layer.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/authtoken"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	jwtSecret string
	jwtTTL    time.Duration
	log       *slog.Logger
}

func New(pool *pgxpool.Pool, jwtSecret string, jwtTTL time.Duration, log *slog.Logger) *Service {
	return &Service{pool: pool, q: store.New(pool), jwtSecret: jwtSecret, jwtTTL: jwtTTL, log: log}
}

// ---------------------------------------------------------------- auth

var (
	errBadCredentials = apierr.Unauthorized("IAM_BAD_CREDENTIALS", "用户名或密码错误")
	errAccountLocked  = apierr.Unauthorized("IAM_ACCOUNT_LOCKED", "账号已锁定，请联系管理员")
)

type LoginResult struct {
	Token            string
	ExpiresInSeconds int64
	Employee         store.GetEmployeeRow
	PermissionCodes  []string
}

func (s *Service) Login(ctx context.Context, tenantID int64, username, password string) (*LoginResult, error) {
	u, err := s.q.GetUserByUsername(ctx, store.GetUserByUsernameParams{TenantID: tenantID, Username: username})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errBadCredentials
		}
		return nil, fmt.Errorf("login: %w", err)
	}
	if u.Status == "LOCKED" || u.Status == "DISABLED" || u.EmployeeStatus != "ACTIVE" {
		return nil, errAccountLocked
	}
	if !VerifyPassword(u.PasswordHash, password) {
		if row, ferr := s.q.RecordLoginFailure(ctx, u.ID); ferr == nil && row.Status == "LOCKED" {
			return nil, errAccountLocked
		}
		return nil, errBadCredentials
	}
	if err := s.q.RecordLoginSuccess(ctx, u.ID); err != nil {
		return nil, fmt.Errorf("login: record success: %w", err)
	}

	token, err := authtoken.Issue(s.jwtSecret, s.jwtTTL, tenantID, u.EmployeeID, u.EmployeeName)
	if err != nil {
		return nil, fmt.Errorf("login: issue token: %w", err)
	}
	emp, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: u.EmployeeID})
	if err != nil {
		return nil, fmt.Errorf("login: load employee: %w", err)
	}
	perms, err := s.q.ListEmployeePermissionCodes(ctx,
		store.ListEmployeePermissionCodesParams{TenantID: tenantID, EmployeeID: u.EmployeeID})
	if err != nil {
		return nil, fmt.Errorf("login: load permissions: %w", err)
	}
	return &LoginResult{
		Token:            token,
		ExpiresInSeconds: int64(s.jwtTTL.Seconds()),
		Employee:         emp,
		PermissionCodes:  perms,
	}, nil
}

// ---------------------------------------------------------------- directory

func (s *Service) CreateDepartment(ctx context.Context, tenantID int64, code, name string, parentID int64) (store.Department, error) {
	if code == "" || name == "" {
		return store.Department{}, apierr.Invalid("IAM_DEPT_FIELDS_REQUIRED", "部门编码和名称必填")
	}
	var out store.Department
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		parentPath, level := "/", int32(1)
		var pid *int64
		if parentID > 0 {
			parent, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: parentID})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apierr.NotFound("IAM_DEPT_PARENT_NOT_FOUND", "上级部门不存在")
				}
				return err
			}
			parentPath, level, pid = parent.Path, parent.Level+1, &parentID
		}
		dept, err := q.CreateDepartment(ctx, store.CreateDepartmentParams{
			TenantID: tenantID, Code: code, Name: name, ParentID: pid, Path: parentPath, Level: level,
		})
		if err != nil {
			return translateUnique(err, "IAM_DEPT_CODE_TAKEN", "部门编码已存在")
		}
		// Materialised path needs the generated id: /1/5/<new>/.
		path := fmt.Sprintf("%s%d/", parentPath, dept.ID)
		if err := q.SetDepartmentPath(ctx, store.SetDepartmentPathParams{
			TenantID: tenantID, ID: dept.ID, Path: path, Level: level,
		}); err != nil {
			return err
		}
		dept.Path = path
		out = dept
		return nil
	})
	return out, err
}

func (s *Service) ListDepartments(ctx context.Context, tenantID int64) ([]store.Department, error) {
	return s.q.ListDepartments(ctx, tenantID)
}

type CreateEmployeeInput struct {
	Code, Name, Position, Email, Phone string
	DepartmentID                       int64
	Username, InitialPassword          string // optional login account
}

func (s *Service) CreateEmployee(ctx context.Context, tenantID int64, in CreateEmployeeInput) (store.GetEmployeeRow, error) {
	if in.Code == "" || in.Name == "" || in.DepartmentID == 0 {
		return store.GetEmployeeRow{}, apierr.Invalid("IAM_EMP_FIELDS_REQUIRED", "工号、姓名、部门必填")
	}
	if (in.Username == "") != (in.InitialPassword == "") {
		return store.GetEmployeeRow{}, apierr.Invalid("IAM_EMP_ACCOUNT_INCOMPLETE", "用户名与初始密码需同时提供")
	}
	var id int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: in.DepartmentID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("IAM_DEPT_NOT_FOUND", "部门不存在")
			}
			return err
		}
		emp, err := q.CreateEmployee(ctx, store.CreateEmployeeParams{
			TenantID: tenantID, Code: in.Code, Name: in.Name, DepartmentID: in.DepartmentID,
			Position: in.Position, Email: in.Email, Phone: in.Phone,
		})
		if err != nil {
			return translateUnique(err, "IAM_EMP_CODE_TAKEN", "工号已存在")
		}
		id = emp.ID
		if in.Username != "" {
			hash, err := HashPassword(in.InitialPassword)
			if err != nil {
				return err
			}
			if _, err := q.CreateUser(ctx, store.CreateUserParams{
				TenantID: tenantID, EmployeeID: emp.ID, Username: in.Username, PasswordHash: hash,
			}); err != nil {
				return translateUnique(err, "IAM_USERNAME_TAKEN", "用户名已存在")
			}
		}
		return nil
	})
	if err != nil {
		return store.GetEmployeeRow{}, err
	}
	return s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
}

func (s *Service) GetEmployee(ctx context.Context, tenantID, id int64) (store.GetEmployeeRow, []int64, error) {
	emp, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.GetEmployeeRow{}, nil, apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return store.GetEmployeeRow{}, nil, err
	}
	roleIDs, err := s.q.ListEmployeeRoleIDs(ctx, store.ListEmployeeRoleIDsParams{TenantID: tenantID, EmployeeID: id})
	if err != nil {
		return store.GetEmployeeRow{}, nil, err
	}
	return emp, roleIDs, nil
}

func (s *Service) ListEmployees(ctx context.Context, tenantID int64, departmentID int64, keyword string, page, size int32) ([]store.ListEmployeesRow, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	rows, err := s.q.ListEmployees(ctx, store.ListEmployeesParams{
		TenantID: tenantID, Column2: departmentID, Column3: keyword,
		Limit: size, Offset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

func (s *Service) DeactivateEmployee(ctx context.Context, tenantID, id int64) error {
	n, err := s.q.DeactivateEmployee(ctx, store.DeactivateEmployeeParams{TenantID: tenantID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在或已停用")
	}
	return nil
}

// ---------------------------------------------------------------- access

func (s *Service) CreateRole(ctx context.Context, tenantID int64, code, name, description string) (store.Role, error) {
	if code == "" || name == "" {
		return store.Role{}, apierr.Invalid("IAM_ROLE_FIELDS_REQUIRED", "角色编码和名称必填")
	}
	r, err := s.q.CreateRole(ctx, store.CreateRoleParams{TenantID: tenantID, Code: code, Name: name, Description: description})
	if err != nil {
		return store.Role{}, translateUnique(err, "IAM_ROLE_CODE_TAKEN", "角色编码已存在")
	}
	return r, nil
}

func (s *Service) ListRoles(ctx context.Context, tenantID int64) ([]store.Role, map[int64][]string, error) {
	roles, err := s.q.ListRoles(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	codes := make(map[int64][]string, len(roles))
	for _, r := range roles {
		cs, err := s.q.ListRolePermissionCodes(ctx, store.ListRolePermissionCodesParams{TenantID: tenantID, RoleID: r.ID})
		if err != nil {
			return nil, nil, err
		}
		codes[r.ID] = cs
	}
	return roles, codes, nil
}

func (s *Service) ListPermissions(ctx context.Context) ([]store.Permission, error) {
	return s.q.ListPermissions(ctx)
}

// GrantRolePermissions replaces the role's permission set atomically.
func (s *Service) GrantRolePermissions(ctx context.Context, tenantID, roleID int64, codes []string) error {
	ids, err := s.q.GetPermissionIDsByCodes(ctx, codes)
	if err != nil {
		return err
	}
	if len(ids) != len(codes) {
		return apierr.Invalid("IAM_PERMISSION_UNKNOWN", "存在未知的权限编码").
			WithMeta("requested", fmt.Sprint(len(codes)), "matched", fmt.Sprint(len(ids)))
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ReplaceRolePermissions(ctx, store.ReplaceRolePermissionsParams{TenantID: tenantID, RoleID: roleID}); err != nil {
			return err
		}
		for _, pid := range ids {
			if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{TenantID: tenantID, RoleID: roleID, PermissionID: pid}); err != nil {
				return err
			}
		}
		return nil
	})
}

// AssignEmployeeRoles replaces the employee's role set atomically.
func (s *Service) AssignEmployeeRoles(ctx context.Context, tenantID, employeeID int64, roleIDs []int64) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ReplaceEmployeeRoles(ctx, store.ReplaceEmployeeRolesParams{TenantID: tenantID, EmployeeID: employeeID}); err != nil {
			return err
		}
		for _, rid := range roleIDs {
			if err := q.AddEmployeeRole(ctx, store.AddEmployeeRoleParams{TenantID: tenantID, EmployeeID: employeeID, RoleID: rid}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) CheckPermission(ctx context.Context, tenantID, employeeID int64, code string) (bool, error) {
	return s.q.EmployeeHasPermission(ctx, store.EmployeeHasPermissionParams{
		TenantID: tenantID, EmployeeID: employeeID, Code: code,
	})
}

func (s *Service) ListEmployeePermissions(ctx context.Context, tenantID, employeeID int64) ([]string, error) {
	return s.q.ListEmployeePermissionCodes(ctx,
		store.ListEmployeePermissionCodesParams{TenantID: tenantID, EmployeeID: employeeID})
}

// translateUnique turns a unique-violation into the given business error and
// passes everything else through untouched.
func translateUnique(err error, code, msg string) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return apierr.Conflict(code, msg)
	}
	return err
}
