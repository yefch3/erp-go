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

// Activation: turning an employee record into an account somebody can log in
// to, by proving the company mailbox on it is real and theirs.
//
// The rest of the system takes "this person works here" from
// employees.email_verified_at, and this file is the only thing that sets it
// outside the bootstrap seed. So it is worth stating what the proof actually
// is and is not. It is: somebody read a mail sent to an address on a domain
// the company owns, and opened the link in it. It is not: a claim by an
// administrator, who can type any address into an employee row.
//
// That distinction is the whole reason for the round trip through the mail
// host. An administrator typing alice@othercompany.com creates a row that
// cannot be activated, because the link goes to a mailbox the company does not
// control and the domain check refuses it before the mail is even sent.

const (
	// Seven days. Long enough for somebody on leave to come back to it, short
	// enough that the pile of live keys does not grow without bound. The token
	// is not the only thing protecting the account — reading the mail requires
	// the mailbox — so the window can be measured in days rather than minutes.
	invitationTTL = 7 * 24 * time.Hour

	// 256 bits. The token is guarded by entropy and nothing else: the route
	// that redeems it is unauthenticated by necessity, so it cannot be rate
	// limited by identity, and there is nobody to lock out.
	invitationTokenBytes = 32
)

var (
	errInviteNoAddress = apierr.Invalid("IAM_INVITE_NO_ADDRESS",
		"该员工还没有公司邮箱地址，请先填写")
	errInviteForeignDomain = apierr.Invalid("IAM_INVITE_FOREIGN_DOMAIN",
		"邮箱不属于本公司的域名，无法发送激活邮件")
	errInviteAlreadyActive = apierr.Conflict("IAM_INVITE_ALREADY_ACTIVE",
		"该员工已激活，如需重设密码请使用重置密码")

	// The link failed, and the four ways it can fail are told apart on
	// purpose. "Try again" is useless advice for three of them, and the person
	// reading it has no other channel to ask.
	errActivateBadToken = apierr.Invalid("IAM_ACTIVATE_BAD_TOKEN",
		"激活链接无效，请向管理员索取新的邀请")
	errActivateUsed = apierr.Invalid("IAM_ACTIVATE_USED",
		"该激活链接已经使用过，请直接登录")
	errActivateExpired = apierr.Invalid("IAM_ACTIVATE_EXPIRED",
		"激活链接已过期，请向管理员索取新的邀请")
	errActivateAddressChanged = apierr.Invalid("IAM_ACTIVATE_ADDRESS_CHANGED",
		"该账号的邮箱地址已变更，此链接不再有效")
	errActivateNotEmployable = apierr.Invalid("IAM_ACTIVATE_UNAVAILABLE",
		"账号当前不可激活，请联系管理员")
)

// Invitation is what the caller needs in order to send the mail. The token is
// in it exactly once, on this return path: it is never read back from storage,
// because storage holds only its hash.
type Invitation struct {
	Token     string
	Email     string
	Name      string
	ExpiresAt time.Time
}

// InviteEmployee mints a one-time link for somebody who has not activated yet.
//
// It does not send anything. Sending is the caller's job because every mail
// this system emits leaves through some employee's own bound mailbox, and iam
// has no mail client — nor could it have one, since the mail service already
// depends on iam and the reverse edge would close a cycle.
func (s *Service) InviteEmployee(ctx context.Context, tenantID, employeeID, invitedBy int64) (Invitation, error) {
	emp, err := s.q.GetEmployee(ctx, store.GetEmployeeParams{TenantID: tenantID, ID: employeeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invitation{}, apierr.NotFound("IAM_EMP_NOT_FOUND", "员工不存在")
		}
		return Invitation{}, err
	}
	if emp.Status != "ACTIVE" {
		return Invitation{}, apierr.Invalid("IAM_INVITE_NOT_ACTIVE", "该员工已离职或停用，无法邀请")
	}
	addr := strings.ToLower(strings.TrimSpace(emp.Email))
	if addr == "" {
		return Invitation{}, errInviteNoAddress
	}
	at := strings.LastIndex(addr, "@")
	if at < 1 || at == len(addr)-1 {
		return Invitation{}, errInviteNoAddress
	}
	// A link is only proof if it goes somewhere the company can read. An
	// address on a domain we do not own proves the person controls a personal
	// mailbox, which is not the question being asked.
	owned, err := s.q.IsTenantDomain(ctx, store.IsTenantDomainParams{
		Domain: addr[at+1:], TenantID: tenantID,
	})
	if err != nil {
		return Invitation{}, err
	}
	if !owned {
		return Invitation{}, errInviteForeignDomain
	}
	// Already activated. Re-inviting would work — the mail goes to their own
	// mailbox — but it would be a password reset wearing an invitation's
	// clothes, arrived at by clicking a button labelled 邀请. ResetPassword
	// exists for that and says what it does.
	if emp.EmailVerifiedAt.Valid {
		return Invitation{}, errInviteAlreadyActive
	}

	raw := make([]byte, invitationTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Invitation{}, fmt.Errorf("invite: token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	expires := time.Now().Add(invitationTTL)

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// Replace, never accumulate. Same transaction as the insert so a
		// failure here cannot leave the employee with no live link at all.
		if _, err := q.DeleteLiveInvitations(ctx, store.DeleteLiveInvitationsParams{
			TenantID: tenantID, EmployeeID: employeeID,
		}); err != nil {
			return err
		}
		_, err := q.CreateInvitation(ctx, store.CreateInvitationParams{
			TenantID: tenantID, EmployeeID: employeeID, Email: addr,
			TokenHash: sum[:],
			ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
			InvitedBy: invitedBy,
		})
		return err
	})
	if err != nil {
		return Invitation{}, err
	}
	s.log.Info("invitation issued",
		"tenant_id", tenantID, "employee_id", employeeID, "invited_by", invitedBy)
	return Invitation{Token: token, Email: addr, Name: emp.Name, ExpiresAt: expires}, nil
}

// InvitationTarget is what an activation page may know before anybody has
// proved anything: who the link is for, so they can see they opened the right
// one. Nothing here is a secret — whoever holds the token was sent it.
type InvitationTarget struct {
	Name  string
	Email string
}

// PeekInvitation answers "does this link still work" without redeeming it.
//
// It exists so the page can say 链接已过期 before asking for a password rather
// than after. Same checks as Activate, minus the ones that need a password —
// deliberately sharing one function so the two cannot drift into disagreeing
// about what a usable link is.
func (s *Service) PeekInvitation(ctx context.Context, token string) (InvitationTarget, error) {
	inv, err := s.usableInvitation(ctx, token)
	if err != nil {
		return InvitationTarget{}, err
	}
	return InvitationTarget{Name: inv.EmployeeName, Email: inv.Email}, nil
}

// ActivateAccount redeems the link: it sets the password the person chose and
// stamps the mailbox as proved.
//
// Returns the address, because the caller's next move is to log in and it
// should not have to ask the person to retype what we already know.
func (s *Service) ActivateAccount(ctx context.Context, token, password string) (string, error) {
	inv, err := s.usableInvitation(ctx, token)
	if err != nil {
		return "", err
	}
	if err := checkPasswordStrength(password); err != nil {
		return "", err
	}
	// Hashing happens here and not a line earlier. This route is
	// unauthenticated by necessity, and argon2id is 64 MiB and a few
	// milliseconds of CPU per call — running it before the token is known to
	// be good would make an open endpoint that burns a gigabyte of memory for
	// anybody who sends sixteen requests.
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// The token is spent first, and the row count is the answer to the
		// race. Two clicks on the same link land here together; the UPDATE's
		// own WHERE decides which one is the activation.
		n, err := q.ConsumeInvitation(ctx, inv.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			return errActivateUsed
		}
		// The stamp, and the only thing in the system that means "this
		// mailbox was proved to exist and to be theirs".
		if err := q.SetEmployeeEmailVerified(ctx, store.SetEmployeeEmailVerifiedParams{
			TenantID: inv.TenantID, ID: inv.EmployeeID, Email: inv.Email,
		}); err != nil {
			return err
		}
		return q.UpsertUserPassword(ctx, store.UpsertUserPasswordParams{
			TenantID: inv.TenantID, EmployeeID: inv.EmployeeID,
			// username is no longer the login identity and is on its way out,
			// but the column is NOT NULL UNIQUE. The address keeps it
			// unambiguous, matching what the bootstrap seed writes.
			Username: inv.Email, PasswordHash: hash,
		})
	})
	if err != nil {
		return "", err
	}
	s.log.Info("account activated",
		"tenant_id", inv.TenantID, "employee_id", inv.EmployeeID)
	return inv.Email, nil
}

// usableInvitation resolves a token and refuses every reason it might not be
// good, in an order chosen so that nothing expensive runs for a bad token.
func (s *Service) usableInvitation(ctx context.Context, token string) (store.GetInvitationByTokenRow, error) {
	var zero store.GetInvitationByTokenRow
	token = strings.TrimSpace(token)
	if token == "" {
		return zero, errActivateBadToken
	}
	sum := sha256.Sum256([]byte(token))
	inv, err := s.q.GetInvitationByToken(ctx, sum[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, errActivateBadToken
		}
		return zero, err
	}
	if err := invitationFault(inv, time.Now()); err != nil {
		return zero, err
	}
	return inv, nil
}

// invitationFault names the reason a resolved link cannot be redeemed, or nil.
//
// Split out from the lookup, and given the clock rather than reading it, so
// every refusal can be exercised without a database and without waiting seven
// days for one of them.
func invitationFault(inv store.GetInvitationByTokenRow, now time.Time) error {
	if inv.UsedAt.Valid {
		return errActivateUsed
	}
	if now.After(inv.ExpiresAt.Time) {
		return errActivateExpired
	}
	// The address moved after the link was sent. The token proves control of
	// the mailbox it was mailed to, and that is no longer this account's
	// address — so redeeming it would stamp a verification that was never
	// earned for the address it would be recorded against.
	if !strings.EqualFold(strings.TrimSpace(inv.CurrentEmail), inv.Email) {
		return errActivateAddressChanged
	}
	if inv.TenantStatus != "ACTIVE" || inv.EmployeeStatus != "ACTIVE" {
		// Suspended company, or somebody who left between the invitation and
		// the click. One message for both: the person on the other end cannot
		// act on the difference, and spelling out "your company is suspended"
		// to an unauthenticated caller says more than it needs to.
		return errActivateNotEmployable
	}
	if inv.EmailVerifiedAt.Valid {
		// Activated by some other route while this link was in flight. Not an
		// error state worth its own message — from the person's side the
		// account works, which is what "already used" tells them.
		return errActivateUsed
	}
	return nil
}
