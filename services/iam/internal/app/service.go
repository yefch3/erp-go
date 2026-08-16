// Package app holds the iam use cases. It depends on the sqlc store and
// pkg libraries only; gRPC types never reach this layer.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	errBadCredentials = apierr.Unauthorized("IAM_BAD_CREDENTIALS", "邮箱或密码错误")
	// A decision: disabled, or no longer employed here. Waiting does not help,
	// so the message sends them to somebody who can act.
	errAccountLocked = apierr.Unauthorized("IAM_ACCOUNT_LOCKED", "账号已停用，请联系管理员")
	// Told apart from a bad password on purpose. Trying harder cannot fix it;
	// what the person needs is the invitation mail, so the message says so.
	errNotActivated = apierr.Unauthorized("IAM_NOT_ACTIVATED", "账号尚未激活，请查收邀请邮件")
)

// errTooManyAttempts says when, not just no.
//
// The number is the whole difference between this and the state it replaces.
// "账号已锁定，请联系管理员" was true and useless: there was nobody to contact
// who could do anything, and no amount of waiting helped either. A person who
// is told to come back in forty seconds can come back in forty seconds.
//
// Seconds while the wait is short enough to sit through, minutes once it is
// not. A lock measured in seconds and reported as "1 分钟" reads as far worse
// news than it is, and the whole point of the short window is that this is
// now an interruption rather than an outage.
func errTooManyAttempts(wait time.Duration) error {
	if wait < 90*time.Second {
		seconds := int(wait/time.Second) + 1
		return apierr.Throttled("IAM_TOO_MANY_ATTEMPTS",
			fmt.Sprintf("密码错误次数过多，请在 %d 秒后重试", seconds))
	}
	minutes := int(wait/time.Minute) + 1
	return apierr.Throttled("IAM_TOO_MANY_ATTEMPTS",
		fmt.Sprintf("密码错误次数过多，请在 %d 分钟后重试", minutes))
}

type LoginResult struct {
	Token            string
	ExpiresInSeconds int64
	Employee         store.GetEmployeeRow
	PermissionCodes  []string
}

// Login takes the company address somebody typed, not a username.
//
// The address does two jobs. Its domain says which company this is, so there
// is no "choose your company" dropdown and no tenant to pass in — a login page
// serving twenty companies is the same page. And the address itself is the
// account, which is only meaningful because it had to be proved: an employee
// row carries email_verified_at only if somebody opened a one-time link sent
// to that mailbox. A person the company never gave a mailbox has no way to
// reach that state.
//
// Nothing here talks to the mail host. That question was asked once, at
// activation, and its answer is the timestamp. Asking it again on every login
// would tie the ERP's availability to 263's, and could not be done anyway:
// the password typed here is ours, not the mailbox's.
func (s *Service) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	addr := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(addr, "@")
	if at < 1 || at == len(addr)-1 {
		burnPasswordTime()
		return nil, errBadCredentials
	}
	// The address identifies the account outright; the domain is not consulted.
	//
	// It used to be: domain names the tenant, then (tenant, address) names the
	// account. That reads naturally and is wrong for any address on a public
	// mail service — and not merely "wrong company": tenant_domains.domain is
	// a PRIMARY KEY, so the first company to register gmail.com owned it and
	// the second could not be onboarded at all.
	//
	// Deciding *whether* an address is a company mailbox is a separate rule
	// and still enforced, where it belongs: when an employee is imported or
	// invited (ListTenantDomains, IsTenantDomain). Login does not repeat it —
	// an address that reached the employees table already passed it.
	u, err := s.q.GetUserByEmail(ctx, addr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Deliberately identical to a wrong password, in wording and in
			// time. Addresses here are guessable — 名字@公司域名 — so a login
			// page that answered differently would be a staff directory.
			burnPasswordTime()
			return nil, errBadCredentials
		}
		return nil, fmt.Errorf("login: %w", err)
	}
	if u.TenantStatus != "ACTIVE" {
		return nil, errAccountLocked
	}
	tenantID := u.TenantID
	if !u.EmailVerifiedAt.Valid {
		// Imported but never activated. Named rather than folded into "wrong
		// password": the person cannot fix this by trying harder, and the
		// thing they need is the invitation mail.
		return nil, errNotActivated
	}
	if u.Status == "DISABLED" || u.EmployeeStatus != "ACTIVE" {
		// A decision somebody made about this account, which no amount of
		// waiting changes. Distinct from the lock below, which is a deadline.
		burnPasswordTime()
		return nil, errAccountLocked
	}
	// Too many recent failures. Refused before the password is checked, which
	// is what makes it a rate limit rather than a message: verifying first
	// would let the guessing continue at full speed and reduce the lock to a
	// different sentence on the same page.
	//
	// This is the per-account half of the pair. The other half is per-source
	// and lives at the gateway, which is the only layer that can see who is
	// asking — the same split ERPNext makes, and for the same reason: one
	// counter answers "is this account under attack", the other answers "is
	// this source attacking".
	//
	// A minute, not a quarter of an hour. Somebody who can reach this route
	// can still keep a named person out by spending ten guesses a minute, and
	// no per-account counter can ever prevent that — the counter cannot tell
	// the attacker from the owner. What it can do is decide how much a
	// successful nuisance costs its victim, and sixty seconds is a different
	// thing from fifteen minutes.
	if u.LockedUntil.Valid && time.Now().Before(u.LockedUntil.Time) {
		burnPasswordTime()
		return nil, errTooManyAttempts(time.Until(u.LockedUntil.Time))
	}
	if !VerifyPassword(u.PasswordHash, password) {
		if row, ferr := s.q.RecordLoginFailure(ctx, u.ID); ferr == nil &&
			row.LockedUntil.Valid && time.Now().Before(row.LockedUntil.Time) {
			return nil, errTooManyAttempts(time.Until(row.LockedUntil.Time))
		}
		return nil, errBadCredentials
	}
	if err := s.q.RecordLoginSuccess(ctx, u.ID); err != nil {
		return nil, fmt.Errorf("login: record success: %w", err)
	}

	token, err := authtoken.Issue(s.jwtSecret, s.jwtTTL, tenantID, u.EmployeeID, u.EmployeeName, addr)
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

type CreateDepartmentInput struct {
	Code, Name string
	ParentID   int64
	SortOrder  int32
	OperatorID int64
}

// CreateDepartment 新建部门并在同一事务中建立物化路径和审计记录。
func (s *Service) CreateDepartment(ctx context.Context, tenantID int64, in CreateDepartmentInput) (store.Department, error) {
	code, name := strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	if code == "" || name == "" {
		return store.Department{}, apierr.Invalid("IAM_DEPT_FIELDS_REQUIRED", "部门编码和名称必填")
	}
	var out store.Department
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		parentPath, level := "/", int32(1)
		var pid *int64
		if in.ParentID > 0 {
			parent, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: in.ParentID})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apierr.NotFound("IAM_DEPT_PARENT_NOT_FOUND", "上级部门不存在")
				}
				return err
			}
			if parent.Status != "ACTIVE" {
				return apierr.Conflict("IAM_DEPT_PARENT_INACTIVE", "上级部门已停用")
			}
			parentPath, level, pid = parent.Path, parent.Level+1, &in.ParentID
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
		dept.SortOrder = in.SortOrder
		if in.SortOrder != 0 {
			dept, err = q.UpdateDepartmentDetails(ctx, store.UpdateDepartmentDetailsParams{
				Code: code, Name: name, ParentID: in.ParentID, SortOrder: in.SortOrder,
				TenantID: tenantID, ID: dept.ID, ExpectedVersion: dept.Version,
			})
			if err != nil {
				return err
			}
			dept.Path = path
		}
		if err := q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "DEPARTMENT", EntityID: dept.ID, Action: "CREATE",
			BeforeData: []byte(`{}`), AfterData: snapshotJSON(dept), OperatorID: in.OperatorID,
		}); err != nil {
			return err
		}
		out = dept
		return nil
	})
	return out, err
}

func (s *Service) ListDepartments(ctx context.Context, tenantID int64) ([]store.Department, error) {
	return s.q.ListDepartments(ctx, tenantID)
}

type UpdateDepartmentInput struct {
	ID, ParentID, LeaderEmployeeID, OperatorID int64
	Code, Name, Status                         string
	SortOrder, ExpectedVersion                 int32
}

// UpdateDepartment 修改部门资料、移动整棵子树并校验停用条件，任何一步失败都会整体回滚。
func (s *Service) UpdateDepartment(ctx context.Context, tenantID int64, in UpdateDepartmentInput) (store.Department, error) {
	code, name := strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	if in.ID == 0 || code == "" || name == "" || in.ExpectedVersion < 1 {
		return store.Department{}, apierr.Invalid("IAM_DEPT_FIELDS_REQUIRED", "部门、编码、名称和版本必填")
	}
	var out store.Department
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: in.ID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("IAM_DEPT_NOT_FOUND", "部门不存在")
			}
			return err
		}
		if current.Version != in.ExpectedVersion {
			return apierr.Conflict("IAM_DEPT_VERSION_CONFLICT", "部门已被其他人修改，请刷新后重试")
		}

		parentPath, level := "/", int32(1)
		if in.ParentID > 0 {
			if in.ParentID == in.ID {
				return apierr.Invalid("IAM_DEPT_CYCLE", "部门不能成为自己的上级")
			}
			parent, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: in.ParentID})
			if err != nil {
				return apierr.NotFound("IAM_DEPT_PARENT_NOT_FOUND", "上级部门不存在")
			}
			if parent.Status != "ACTIVE" {
				return apierr.Conflict("IAM_DEPT_PARENT_INACTIVE", "上级部门已停用")
			}
			if strings.HasPrefix(parent.Path, current.Path) {
				return apierr.Invalid("IAM_DEPT_CYCLE", "不能把部门移动到自己的下级部门")
			}
			parentPath, level = parent.Path, parent.Level+1
		}

		if in.LeaderEmployeeID > 0 {
			leader, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: in.LeaderEmployeeID})
			if err != nil || leader.Status != "ACTIVE" {
				return apierr.Invalid("IAM_DEPT_LEADER_INVALID", "部门负责人必须是在职员工")
			}
			if leader.DepartmentID != in.ID {
				return apierr.Invalid("IAM_DEPT_LEADER_OUTSIDE", "部门负责人必须属于当前部门")
			}
		}

		status := in.Status
		if status == "" {
			status = current.Status
		}
		if status != "ACTIVE" && status != "INACTIVE" {
			return apierr.Invalid("IAM_DEPT_STATUS_INVALID", "部门状态无效")
		}
		if current.Status == "ACTIVE" && status == "INACTIVE" {
			employees, err := q.CountActiveEmployeesInDepartment(ctx, store.CountActiveEmployeesInDepartmentParams{TenantID: tenantID, DepartmentID: in.ID})
			if err != nil {
				return err
			}
			children, err := q.CountActiveChildDepartments(ctx, store.CountActiveChildDepartmentsParams{TenantID: tenantID, ParentID: in.ID})
			if err != nil {
				return err
			}
			if employees > 0 || children > 0 {
				return apierr.Conflict("IAM_DEPT_IN_USE", "部门仍有在职员工或启用中的下级部门，不能停用")
			}
		}

		updated, err := q.UpdateDepartmentDetails(ctx, store.UpdateDepartmentDetailsParams{
			Code: code, Name: name, ParentID: in.ParentID, SortOrder: in.SortOrder,
			LeaderEmployeeID: in.LeaderEmployeeID, TenantID: tenantID, ID: in.ID,
			ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.Conflict("IAM_DEPT_VERSION_CONFLICT", "部门已被其他人修改，请刷新后重试")
			}
			return translateUnique(err, "IAM_DEPT_CODE_TAKEN", "部门编码已存在")
		}
		newPath := fmt.Sprintf("%s%d/", parentPath, in.ID)
		if newPath != current.Path || level != current.Level {
			if err := q.UpdateDepartmentSubtree(ctx, store.UpdateDepartmentSubtreeParams{
				NewPath: newPath, OldPath: current.Path, LevelDelta: level - current.Level, TenantID: tenantID,
			}); err != nil {
				return err
			}
			updated.Path, updated.Level = newPath, level
		}
		if status != current.Status {
			updated, err = q.SetDepartmentStatus(ctx, store.SetDepartmentStatusParams{
				Status: status, TenantID: tenantID, ID: in.ID, ExpectedVersion: updated.Version,
			})
			if err != nil {
				return apierr.Conflict("IAM_DEPT_VERSION_CONFLICT", "部门已被其他人修改，请刷新后重试")
			}
			updated.Path, updated.Level = newPath, level
		}
		if err := q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "DEPARTMENT", EntityID: in.ID, Action: "UPDATE",
			BeforeData: snapshotJSON(current), AfterData: snapshotJSON(updated), OperatorID: in.OperatorID,
		}); err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

type CreateEmployeeInput struct {
	Code, Name, EnglishName, Position, Email, Phone, HireDate, Remark string
	DepartmentID                                                      int64
	Username, InitialPassword                                         string // optional login account
	// Who they report to; approval nodes can target it.
	ManagerID  int64
	OperatorID int64
}

func (s *Service) CreateEmployee(ctx context.Context, tenantID int64, in CreateEmployeeInput) (store.GetEmployeeRow, error) {
	in.Code, in.Name = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Code == "" || in.Name == "" || in.DepartmentID == 0 {
		return store.GetEmployeeRow{}, apierr.Invalid("IAM_EMP_FIELDS_REQUIRED", "工号、姓名、部门必填")
	}
	if err := validateEmployeeEmail(in.Email); err != nil {
		return store.GetEmployeeRow{}, err
	}
	if err := validateEmployeePhone(in.Phone); err != nil {
		return store.GetEmployeeRow{}, err
	}
	hireDate, err := parseOptionalDate(in.HireDate, "入职日期")
	if err != nil {
		return store.GetEmployeeRow{}, err
	}
	if (in.Username == "") != (in.InitialPassword == "") {
		return store.GetEmployeeRow{}, apierr.Invalid("IAM_EMP_ACCOUNT_INCOMPLETE", "用户名与初始密码需同时提供")
	}
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		dept, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: in.DepartmentID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("IAM_DEPT_NOT_FOUND", "部门不存在")
			}
			return err
		}
		if dept.Status != "ACTIVE" {
			return apierr.Conflict("IAM_DEPT_INACTIVE", "不能把员工加入已停用部门")
		}
		if in.ManagerID > 0 {
			manager, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: in.ManagerID})
			if err != nil || manager.Status != "ACTIVE" {
				return apierr.Invalid("IAM_MANAGER_INVALID", "直属主管必须是在职员工")
			}
		}
		emp, err := q.CreateEmployee(ctx, store.CreateEmployeeParams{
			TenantID: tenantID, Code: in.Code, Name: in.Name, DepartmentID: in.DepartmentID,
			Position: in.Position, Email: in.Email, Phone: in.Phone,
			ManagerID: in.ManagerID,
		})
		if err != nil {
			return translateUnique(err, "IAM_EMP_CODE_TAKEN", "工号已存在")
		}
		id = emp.ID
		if in.EnglishName != "" || in.HireDate != "" || in.Remark != "" {
			emp, err = q.UpdateEmployeeDetails(ctx, store.UpdateEmployeeDetailsParams{
				Code: in.Code, Name: in.Name, EnglishName: strings.TrimSpace(in.EnglishName),
				DepartmentID: in.DepartmentID, Position: strings.TrimSpace(in.Position),
				Email: in.Email, Phone: strings.TrimSpace(in.Phone), ManagerID: in.ManagerID,
				HireDate: hireDate, LeaveDate: pgtype.Date{}, Remark: strings.TrimSpace(in.Remark),
				TenantID: tenantID, ID: emp.ID, ExpectedVersion: emp.Version,
			})
			if err != nil {
				return err
			}
		}
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
		if err := q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: emp.ID, Action: "CREATE",
			BeforeData: []byte(`{}`), AfterData: snapshotJSON(emp), OperatorID: in.OperatorID,
		}); err != nil {
			return err
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

type ListEmployeesFilter struct {
	DepartmentID, ManagerID, RoleID          int64
	Keyword, AccountStatus, EmploymentStatus string
	Page, Size                               int32
}

// ListEmployees 使用后端分页和组合筛选，避免把完整员工目录一次加载到浏览器。
func (s *Service) ListEmployees(ctx context.Context, tenantID int64, filter ListEmployeesFilter) ([]store.ListEmployeesFilteredRow, int64, error) {
	page, size := filter.Page, filter.Size
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	rows, err := s.q.ListEmployeesFiltered(ctx, store.ListEmployeesFilteredParams{
		TenantID: tenantID, DepartmentID: filter.DepartmentID, ManagerID: filter.ManagerID,
		RoleID: filter.RoleID, EmploymentStatus: filter.EmploymentStatus,
		AccountStatus: filter.AccountStatus, Keyword: strings.TrimSpace(filter.Keyword),
		PageSize: size, PageOffset: (page - 1) * size,
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

type UpdateEmployeeInput struct {
	ID, DepartmentID, ManagerID, OperatorID         int64
	ExpectedVersion                                 int32
	Code, Name, EnglishName, Position, Email, Phone string
	HireDate, LeaveDate, Remark                     string
}

// UpdateEmployee 保存员工资料并在服务端校验部门、主管循环、日期和并发版本。
func (s *Service) UpdateEmployee(ctx context.Context, tenantID int64, in UpdateEmployeeInput) (store.GetEmployeeRow, error) {
	in.Code, in.Name = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.ID == 0 || in.Code == "" || in.Name == "" || in.DepartmentID == 0 || in.ExpectedVersion < 1 {
		return store.GetEmployeeRow{}, apierr.Invalid("IAM_EMP_FIELDS_REQUIRED", "员工、工号、姓名、部门和版本必填")
	}
	if err := validateEmployeeEmail(in.Email); err != nil {
		return store.GetEmployeeRow{}, err
	}
	if err := validateEmployeePhone(in.Phone); err != nil {
		return store.GetEmployeeRow{}, err
	}
	hireDate, err := parseOptionalDate(in.HireDate, "入职日期")
	if err != nil {
		return store.GetEmployeeRow{}, err
	}
	leaveDate, err := parseOptionalDate(in.LeaveDate, "离职日期")
	if err != nil {
		return store.GetEmployeeRow{}, err
	}
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: in.ID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
			}
			return err
		}
		if current.Version != in.ExpectedVersion {
			return apierr.Conflict("IAM_EMP_VERSION_CONFLICT", "员工资料已被其他人修改，请刷新后重试")
		}
		dept, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: in.DepartmentID})
		if err != nil || dept.Status != "ACTIVE" {
			return apierr.Invalid("IAM_DEPT_INVALID", "员工必须属于启用中的部门")
		}
		if in.ManagerID == in.ID {
			return apierr.Invalid("IAM_MANAGER_SELF", "直属主管不能选择本人")
		}
		if in.ManagerID > 0 {
			manager, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: in.ManagerID})
			if err != nil || manager.Status != "ACTIVE" {
				return apierr.Invalid("IAM_MANAGER_INVALID", "直属主管必须是在职员工")
			}
			cycle, err := q.ManagerCycleExists(ctx, store.ManagerCycleExistsParams{
				TenantID: tenantID, ManagerID: in.ManagerID, EmployeeID: in.ID,
			})
			if err != nil {
				return err
			}
			if cycle {
				return apierr.Invalid("IAM_MANAGER_CYCLE", "直属主管关系不能形成循环")
			}
		}
		updated, err := q.UpdateEmployeeDetails(ctx, store.UpdateEmployeeDetailsParams{
			Code: in.Code, Name: in.Name, EnglishName: strings.TrimSpace(in.EnglishName),
			DepartmentID: in.DepartmentID, Position: strings.TrimSpace(in.Position),
			Email: in.Email, Phone: strings.TrimSpace(in.Phone), ManagerID: in.ManagerID,
			HireDate: hireDate, LeaveDate: leaveDate, Remark: strings.TrimSpace(in.Remark),
			TenantID: tenantID, ID: in.ID, ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.Conflict("IAM_EMP_VERSION_CONFLICT", "员工资料已被其他人修改，请刷新后重试")
			}
			return translateUnique(err, "IAM_EMP_CODE_TAKEN", "工号已存在")
		}
		if err := q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: in.ID, Action: "UPDATE",
			BeforeData: snapshotJSON(current), AfterData: snapshotJSON(updated), OperatorID: in.OperatorID,
		}); err != nil {
			return err
		}
		id = in.ID
		return nil
	})
	if err != nil {
		return store.GetEmployeeRow{}, err
	}
	return s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
}

// ListDirectoryChanges 返回部门或员工最近的变更记录，供详情页审计标签使用。
func (s *Service) ListDirectoryChanges(ctx context.Context, tenantID int64, entityType string, entityID int64) ([]store.DirectoryChangeLog, error) {
	entityType = strings.ToUpper(strings.TrimSpace(entityType))
	if (entityType != "DEPARTMENT" && entityType != "EMPLOYEE") || entityID == 0 {
		return nil, apierr.Invalid("IAM_DIRECTORY_ENTITY_INVALID", "变更记录对象无效")
	}
	return s.q.ListDirectoryChanges(ctx, store.ListDirectoryChangesParams{
		TenantID: tenantID, EntityType: entityType, EntityID: entityID,
	})
}

func validateEmployeeEmail(email string) error {
	if email == "" {
		return nil
	}
	address, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(address.Address, email) {
		return apierr.Invalid("IAM_EMP_EMAIL_INVALID", "员工邮箱格式不正确")
	}
	return nil
}

func validateEmployeePhone(phone string) error {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return nil
	}
	if len(phone) < 6 || len(phone) > 30 {
		return apierr.Invalid("IAM_EMP_PHONE_INVALID", "员工电话长度应为 6 至 30 个字符")
	}
	for _, r := range phone {
		if (r < '0' || r > '9') && !strings.ContainsRune("+()- .", r) {
			return apierr.Invalid("IAM_EMP_PHONE_INVALID", "员工电话只能包含数字、空格及 +()-.")
		}
	}
	return nil
}

func parseOptionalDate(value, field string) (pgtype.Date, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Date{}, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return pgtype.Date{}, apierr.Invalid("IAM_EMP_DATE_INVALID", field+"格式必须为 YYYY-MM-DD")
	}
	return pgtype.Date{Time: parsed, Valid: true}, nil
}

func snapshotJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return data
}

// adminPermission is the capability that must never disappear: whoever holds
// it is the only one who can bring anybody (including themselves) back.
const adminPermission = "iam:employee:write"

// DeactivateEmployee 标记员工离职；离职前必须完成直属下级和部门负责人的转交，
// 同时禁止管理员将自己或最后一名员工管理员停用，避免系统失去管理入口。
func (s *Service) DeactivateEmployee(ctx context.Context, tenantID, id, actorID int64) error {
	if id == actorID {
		return apierr.Invalid("IAM_CANNOT_LEAVE_SELF", "不能把自己标记为离职，请让其他管理员操作")
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
		if err != nil || current.Status != "ACTIVE" {
			return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在或已离职")
		}
		reports, err := q.CountActiveDirectReports(ctx, store.CountActiveDirectReportsParams{TenantID: tenantID, ManagerID: id})
		if err != nil {
			return err
		}
		led, err := q.CountDepartmentsLedByEmployee(ctx, store.CountDepartmentsLedByEmployeeParams{TenantID: tenantID, EmployeeID: id})
		if err != nil {
			return err
		}
		if reports > 0 || led > 0 {
			return apierr.Conflict("IAM_EMP_TRANSFER_REQUIRED", "员工仍是直属主管或部门负责人，请先完成转交")
		}
		others, err := q.CountOtherHoldersOf(ctx, store.CountOtherHoldersOfParams{TenantID: tenantID, ID: id, Code: adminPermission})
		if err != nil {
			return err
		}
		if others == 0 {
			holds, err := q.EmployeeHasPermission(ctx, store.EmployeeHasPermissionParams{TenantID: tenantID, EmployeeID: id, Code: adminPermission})
			if err != nil {
				return err
			}
			if holds {
				return apierr.Conflict("IAM_LAST_ADMIN", "这是最后一个可以管理员工的账号，停用后将无人能恢复")
			}
		}
		n, err := q.DeactivateEmployee(ctx, store.DeactivateEmployeeParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在或已离职")
		}
		after, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: id, Action: "DEACTIVATE",
			BeforeData: snapshotJSON(current), AfterData: snapshotJSON(after), OperatorID: actorID,
		})
	})
}

// ActivateEmployee 恢复离职员工；只有原所属部门仍在启用时才能复职。
func (s *Service) ActivateEmployee(ctx context.Context, tenantID, id, actorID int64) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
		if err != nil || current.Status != "INACTIVE" {
			return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在或已在职")
		}
		dept, err := q.GetDepartment(ctx, store.GetDepartmentParams{TenantID: tenantID, ID: current.DepartmentID})
		if err != nil || dept.Status != "ACTIVE" {
			return apierr.Conflict("IAM_DEPT_INACTIVE", "员工所属部门已停用，不能复职")
		}
		n, err := q.ActivateEmployee(ctx, store.ActivateEmployeeParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在或已在职")
		}
		after, err := q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: id, Action: "ACTIVATE",
			BeforeData: snapshotJSON(current), AfterData: snapshotJSON(after), OperatorID: actorID,
		})
	})
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

// GrantRolePermissions 原子替换角色权限，并记录修改前后权限及操作人。
func (s *Service) GrantRolePermissions(ctx context.Context, tenantID, roleID int64, codes []string, operatorIDs ...int64) error {
	ids, err := s.q.GetPermissionIDsByCodes(ctx, codes)
	if err != nil {
		return err
	}
	if len(ids) != len(codes) {
		return apierr.Invalid("IAM_PERMISSION_UNKNOWN", "存在未知的权限编码").
			WithMeta("requested", fmt.Sprint(len(codes)), "matched", fmt.Sprint(len(ids)))
	}
	var operatorID int64
	if len(operatorIDs) > 0 {
		operatorID = operatorIDs[0]
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.ListRolePermissionCodes(ctx, store.ListRolePermissionCodesParams{TenantID: tenantID, RoleID: roleID})
		if err != nil {
			return err
		}
		if err := q.ReplaceRolePermissions(ctx, store.ReplaceRolePermissionsParams{TenantID: tenantID, RoleID: roleID}); err != nil {
			return err
		}
		for _, pid := range ids {
			if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{TenantID: tenantID, RoleID: roleID, PermissionID: pid}); err != nil {
				return err
			}
		}
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "ROLE", EntityID: roleID, Action: "SET_PERMISSIONS",
			BeforeData: snapshotJSON(map[string]any{"permission_codes": before}),
			AfterData:  snapshotJSON(map[string]any{"permission_codes": codes}), OperatorID: operatorID,
		})
	})
}

// AssignEmployeeRoles 原子替换员工角色，并记录角色关系变更。
func (s *Service) AssignEmployeeRoles(ctx context.Context, tenantID, employeeID int64, roleIDs []int64, operatorIDs ...int64) error {
	var operatorID int64
	if len(operatorIDs) > 0 {
		operatorID = operatorIDs[0]
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.ListEmployeeRoleIDs(ctx, store.ListEmployeeRoleIDsParams{TenantID: tenantID, EmployeeID: employeeID})
		if err != nil {
			return err
		}
		if err := q.ReplaceEmployeeRoles(ctx, store.ReplaceEmployeeRolesParams{TenantID: tenantID, EmployeeID: employeeID}); err != nil {
			return err
		}
		for _, rid := range roleIDs {
			if err := q.AddEmployeeRole(ctx, store.AddEmployeeRoleParams{TenantID: tenantID, EmployeeID: employeeID, RoleID: rid}); err != nil {
				return err
			}
		}
		return q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
			TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: employeeID, Action: "SET_ROLES",
			BeforeData: snapshotJSON(map[string]any{"role_ids": before}),
			AfterData:  snapshotJSON(map[string]any{"role_ids": roleIDs}), OperatorID: operatorID,
		})
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

// ListRoleMembers powers approval nodes that name a role instead of a person.
// Deactivated employees are left out: they cannot act on a task anyway.
func (s *Service) ListRoleMembers(ctx context.Context, tenantID, roleID int64) ([]store.ListRoleMembersRow, error) {
	return s.q.ListRoleMembers(ctx, store.ListRoleMembersParams{TenantID: tenantID, RoleID: roleID})
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
