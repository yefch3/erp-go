package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// OpenAccount gives an employee who has none a login. Employees without an
// account are normal - a warehouse worker may never open the system.
func (s *Service) OpenAccount(ctx context.Context, tenantID, employeeID int64, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", apierr.Invalid("IAM_ACCOUNT_FIELDS_REQUIRED", "用户名和初始密码必填")
	}
	emp, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return "", err
	}
	// The employee is loaded before the password is judged, because who they
	// are is part of the judgement: a password built out of their own address
	// or name is the first thing anybody guessing would try.
	if err := checkPasswordStrength(password, emp.Email, emp.Name, emp.Code, username); err != nil {
		return "", err
	}
	if _, err := s.q.GetUserByEmployee(ctx, store.GetUserByEmployeeParams{TenantID: tenantID, EmployeeID: employeeID}); err == nil {
		return "", apierr.Conflict("IAM_ACCOUNT_EXISTS", "该员工已有登录账号")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	if _, err := s.q.CreateUser(ctx, store.CreateUserParams{
		TenantID: tenantID, EmployeeID: employeeID, Username: username, PasswordHash: hash,
	}); err != nil {
		return "", translateUnique(err, "IAM_USERNAME_TAKEN", "用户名已存在")
	}
	return username, nil
}

// ResetPassword is the administrator path: it proves nothing about the old
// password because the administrator does not know it.
func (s *Service) ResetPassword(ctx context.Context, tenantID, employeeID int64, newPassword string) error {
	if err := s.checkPassword(ctx, tenantID, employeeID, newPassword); err != nil {
		return err
	}
	return s.setPassword(ctx, tenantID, employeeID, newPassword)
}

// ChangePassword is the self-service path: the current password is the proof
// of identity, so a stolen session alone cannot lock the owner out.
func (s *Service) ChangePassword(ctx context.Context, tenantID, employeeID int64, oldPassword, newPassword string) error {
	if err := s.checkPassword(ctx, tenantID, employeeID, newPassword); err != nil {
		return err
	}
	user, err := s.q.GetUserByEmployee(ctx, store.GetUserByEmployeeParams{TenantID: tenantID, EmployeeID: employeeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("IAM_ACCOUNT_NOT_FOUND", "当前用户没有登录账号")
		}
		return err
	}
	if !VerifyPassword(user.PasswordHash, oldPassword) {
		return apierr.Unauthorized("IAM_PASSWORD_WRONG", "当前密码不正确")
	}
	return s.setPassword(ctx, tenantID, employeeID, newPassword)
}

// checkPassword judges a password knowing whose it is.
//
// The extra read is worth one round trip on an action somebody takes once a
// year: the address and the name are exactly what a guess is built from, and
// refusing them is most of what a strength check can usefully do.
func (s *Service) checkPassword(ctx context.Context, tenantID, employeeID int64, password string) error {
	emp, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
	if err != nil {
		// Not fatal: a password judged without the identity context is still
		// judged on length and on the common-password list. Refusing to set a
		// password because a lookup failed would be the worse trade.
		return checkPasswordStrength(password)
	}
	return checkPasswordStrength(password, emp.Email, emp.Name, emp.Code)
}

func (s *Service) setPassword(ctx context.Context, tenantID, employeeID int64, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	rows, err := s.q.UpdatePassword(ctx, store.UpdatePasswordParams{
		TenantID: tenantID, EmployeeID: employeeID, PasswordHash: hash,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return apierr.NotFound("IAM_ACCOUNT_NOT_FOUND", "该员工没有登录账号")
	}
	return nil
}

// ListAccounts maps employee id -> username so a list can show who can log in
// without a query per row.
func (s *Service) ListAccounts(ctx context.Context, tenantID int64) (map[int64]string, error) {
	rows, err := s.q.ListEmployeeAccounts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.EmployeeID] = r.Username
	}
	return out, nil
}
