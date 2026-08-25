package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/iam/internal/store"
)

// Password resets: the way back in for somebody who cannot log in, built so
// that no administrator ever holds a working password that is not their own.
//
// The direct ResetPassword in account.go survives for exactly one population:
// username accounts with no mailbox, where a link has nowhere to go — and
// even there the password an administrator typed now opens only the
// change-password form (users.must_change_password). Everybody else gets a
// link, and the link is the same trust machinery as activation: proof of
// control of the mailbox on record, not a claim by whoever pressed a button.

const (
	// One hour, not seven days like an invitation. An invitation waits for
	// somebody who was not expecting it; a reset answers somebody staring at
	// their inbox right now. A shorter life is pure gain.
	resetTTL = time.Hour

	resetTokenBytes = 32
)

var (
	errResetBadToken = apierr.Invalid("IAM_RESET_BAD_TOKEN",
		"重置链接无效，请重新申请")
	errResetUsed = apierr.Invalid("IAM_RESET_USED",
		"该重置链接已经使用过，如非本人操作请立即联系管理员")
	errResetExpired = apierr.Invalid("IAM_RESET_EXPIRED",
		"重置链接已过期，请重新申请")
	errResetAddressChanged = apierr.Invalid("IAM_RESET_ADDRESS_CHANGED",
		"该账号的邮箱地址已变更，此链接不再有效")
	errResetUnavailable = apierr.Invalid("IAM_RESET_UNAVAILABLE",
		"账号当前不可重置密码，请联系管理员")
	errResetNoAccount = apierr.Invalid("IAM_RESET_NO_ACCOUNT",
		"该员工没有可重置的登录账号")
	errResetNoMailbox = apierr.Invalid("IAM_RESET_NO_MAILBOX",
		"该员工没有已验证的邮箱，请使用「重置密码」直接设置")
)

// PasswordReset is what the caller needs to send the mail. The token appears
// here once and is never readable again — storage holds only its hash.
type PasswordReset struct {
	Token      string
	Email      string
	Name       string
	TenantID   int64
	EmployeeID int64
	// Whose bound mailbox should carry the mail. The person's inviter when
	// one exists; the person themselves otherwise (the bootstrap admin was
	// never invited but does have a mailbox — their own).
	SenderID  int64
	ExpiresAt time.Time
}

// CreatePasswordReset is the administrator path: same link the login page
// mints, but on somebody's behalf and recorded as such in the change history.
func (s *Service) CreatePasswordReset(ctx context.Context, tenantID, employeeID, requestedBy int64) (PasswordReset, error) {
	emp, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PasswordReset{}, apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return PasswordReset{}, err
	}
	if emp.Status != "ACTIVE" {
		return PasswordReset{}, errResetUnavailable
	}
	// A link is only worth sending to a mailbox this system has proved the
	// person controls. Unverified means un-activated, and the tool for that
	// is the invitation, which carries the same proof for the same reason.
	if !emp.EmailVerifiedAt.Valid || strings.TrimSpace(emp.Email) == "" {
		return PasswordReset{}, errResetNoMailbox
	}
	if _, err := s.q.GetUserByEmployee(ctx, store.GetUserByEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PasswordReset{}, errResetNoAccount
		}
		return PasswordReset{}, err
	}
	reset, err := s.mintPasswordReset(ctx, tenantID, employeeID, emp.Email, emp.Name, requestedBy)
	if err != nil {
		return PasswordReset{}, err
	}
	// The audit row is the deterrent: an administrator can start a reset for
	// anybody, and everybody can see who started one for whom.
	s.recordAccountEvent(ctx, tenantID, employeeID, requestedBy, "PASSWORD_RESET_LINK_SENT")
	return reset, nil
}

// RequestPasswordResetByEmail is the login page's 忘记密码. The bool answers
// "was a link minted", and it is for the gateway alone — the person on the
// other end hears the same sentence either way, because whether an address
// has an account here is exactly the thing an unauthenticated stranger must
// not be able to test. (The same principle, in the same words, as login's
// wrong-address/wrong-password indistinguishability.)
func (s *Service) RequestPasswordResetByEmail(ctx context.Context, email string) (PasswordReset, bool, error) {
	addr := strings.ToLower(strings.TrimSpace(email))
	if addr == "" || !strings.Contains(addr, "@") {
		return PasswordReset{}, false, nil
	}
	u, err := s.q.GetUserByEmail(ctx, addr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PasswordReset{}, false, nil
		}
		return PasswordReset{}, false, err
	}
	// The same refusals as login, silently: an account that could not log in
	// with the right password gains nothing from a reset link, and a link
	// mailed for a suspended account is one more live key for no reason.
	if u.TenantStatus != "ACTIVE" || u.EmployeeStatus != "ACTIVE" ||
		u.Status == "DISABLED" || !u.EmailVerifiedAt.Valid {
		return PasswordReset{}, false, nil
	}
	reset, err := s.mintPasswordReset(ctx, u.TenantID, u.EmployeeID, addr, u.EmployeeName, 0)
	if err != nil {
		return PasswordReset{}, false, err
	}
	s.recordAccountEvent(ctx, u.TenantID, u.EmployeeID, u.EmployeeID, "PASSWORD_RESET_REQUESTED")
	return reset, true, nil
}

func (s *Service) mintPasswordReset(ctx context.Context, tenantID, employeeID int64, email, name string, requestedBy int64) (PasswordReset, error) {
	raw := make([]byte, resetTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return PasswordReset{}, fmt.Errorf("reset: token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	expires := time.Now().Add(resetTTL)

	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.DeleteLivePasswordResets(ctx, store.DeleteLivePasswordResetsParams{
			TenantID: tenantID, EmployeeID: employeeID,
		}); err != nil {
			return err
		}
		_, err := q.CreatePasswordReset(ctx, store.CreatePasswordResetParams{
			TenantID: tenantID, EmployeeID: employeeID, Email: email,
			TokenHash:   sum[:],
			ExpiresAt:   pgtype.Timestamptz{Time: expires, Valid: true},
			RequestedBy: requestedBy,
		})
		return err
	})
	if err != nil {
		return PasswordReset{}, err
	}

	// The mail's sender: the person's inviter, else themselves. Resolved here
	// because only iam knows the invitation history; acted on by the gateway,
	// which owns mail dispatch.
	sender := employeeID
	if inviter, err := s.q.LatestInviterOf(ctx, store.LatestInviterOfParams{
		TenantID: tenantID, EmployeeID: employeeID,
	}); err == nil && inviter > 0 {
		sender = inviter
	}
	s.log.Info("password reset link issued",
		"tenant_id", tenantID, "employee_id", employeeID, "requested_by", requestedBy)
	return PasswordReset{
		Token: token, Email: email, Name: name,
		TenantID: tenantID, EmployeeID: employeeID,
		SenderID: sender, ExpiresAt: expires,
	}, nil
}

// PeekPasswordReset reads a link without spending it, so the page can say a
// dead link is dead before asking anybody to choose a password.
func (s *Service) PeekPasswordReset(ctx context.Context, token string) (InvitationTarget, error) {
	row, err := s.usablePasswordReset(ctx, token)
	if err != nil {
		return InvitationTarget{}, err
	}
	return InvitationTarget{Name: row.EmployeeName, Email: row.Email}, nil
}

// RedeemPasswordReset spends the link and sets the password the person chose.
// It also unlocks the account and clears must_change_password: this password
// was chosen by its owner, which is the exact condition both of those flags
// were waiting on.
func (s *Service) RedeemPasswordReset(ctx context.Context, token, password string) (PasswordReset, error) {
	row, err := s.usablePasswordReset(ctx, token)
	if err != nil {
		return PasswordReset{}, err
	}
	if err := checkPasswordStrength(password, row.Email, row.EmployeeName); err != nil {
		return PasswordReset{}, err
	}
	// Hash only after the token proved good — argon2id is 64 MiB per call and
	// this route is unauthenticated. Same order, same reason as activation.
	hash, err := HashPassword(password)
	if err != nil {
		return PasswordReset{}, err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.ConsumePasswordReset(ctx, row.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			return errResetUsed
		}
		rows, err := q.UpdatePassword(ctx, store.UpdatePasswordParams{
			TenantID: row.TenantID, EmployeeID: row.EmployeeID, PasswordHash: hash,
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			// The account disappeared between minting and redeeming.
			return errResetUnavailable
		}
		// A lockout is a deadline for guessing the OLD password; it has
		// nothing left to wait for once a new one is proven-owner-chosen.
		if err := q.ClearLoginLock(ctx, store.ClearLoginLockParams{
			TenantID: row.TenantID, EmployeeID: row.EmployeeID,
		}); err != nil {
			return err
		}
		_, err = q.SetMustChangePassword(ctx, store.SetMustChangePasswordParams{
			TenantID: row.TenantID, EmployeeID: row.EmployeeID, MustChange: false,
		})
		return err
	})
	if err != nil {
		return PasswordReset{}, err
	}
	s.recordAccountEvent(ctx, row.TenantID, row.EmployeeID, row.EmployeeID, "PASSWORD_RESET_REDEEMED")
	s.log.Info("password reset redeemed",
		"tenant_id", row.TenantID, "employee_id", row.EmployeeID)
	return PasswordReset{
		Email: row.Email, TenantID: row.TenantID, EmployeeID: row.EmployeeID,
	}, nil
}

func (s *Service) usablePasswordReset(ctx context.Context, token string) (store.GetPasswordResetByTokenRow, error) {
	var zero store.GetPasswordResetByTokenRow
	token = strings.TrimSpace(token)
	if token == "" {
		return zero, errResetBadToken
	}
	sum := sha256.Sum256([]byte(token))
	row, err := s.q.GetPasswordResetByToken(ctx, sum[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, errResetBadToken
		}
		return zero, err
	}
	if err := passwordResetFault(row, time.Now()); err != nil {
		return zero, err
	}
	return row, nil
}

// passwordResetFault names the reason a resolved link cannot be redeemed, or
// nil. Split out and handed the clock so every refusal is testable without a
// database — the same shape as invitationFault, for the same reason.
func passwordResetFault(row store.GetPasswordResetByTokenRow, now time.Time) error {
	if row.UsedAt.Valid {
		return errResetUsed
	}
	if now.After(row.ExpiresAt.Time) {
		return errResetExpired
	}
	if !strings.EqualFold(strings.TrimSpace(row.CurrentEmail), row.Email) {
		return errResetAddressChanged
	}
	if row.TenantStatus != "ACTIVE" || row.EmployeeStatus != "ACTIVE" {
		return errResetUnavailable
	}
	return nil
}

// RecordAccountEvent is the audit hook for account actions that happen
// outside iam — today, the gateway ending somebody's sessions in Redis.
func (s *Service) RecordAccountEvent(ctx context.Context, tenantID, employeeID, operatorID int64, action string) {
	s.recordAccountEvent(ctx, tenantID, employeeID, operatorID, action)
}

// recordAccountEvent writes one row of who-did-what-to-whose-account into the
// same change history the employee drawer already shows. Best-effort: the
// action it describes has already succeeded, and failing that action because
// its footprint could not be written would punish the person being helped.
func (s *Service) recordAccountEvent(ctx context.Context, tenantID, employeeID, operatorID int64, action string) {
	if err := s.q.InsertDirectoryChange(ctx, store.InsertDirectoryChangeParams{
		TenantID: tenantID, EntityType: "EMPLOYEE", EntityID: employeeID,
		Action: action, BeforeData: []byte("{}"), AfterData: []byte("{}"),
		OperatorID: operatorID,
	}); err != nil {
		s.log.Warn("could not record account event",
			"action", action, "employee_id", employeeID, "err", err)
	}
}
