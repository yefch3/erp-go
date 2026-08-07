package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// MailAccount is one mailbox, resolved for a single dial.
//
// Secret is plaintext and deliberately has no String or MarshalJSON method
// that could hide it: the protection is that this value is built inside
// ForSender, handed straight to the dialler, and never put in a struct that
// gets logged or serialised. Anything that changes that has to change this
// comment too.
type MailAccount struct {
	AccountID int64
	// PASSWORD or OAUTH. For OAUTH, Secret carries a live access token and
	// the diallers speak XOAUTH2 instead of LOGIN.
	AuthKind string
	// Whose mailbox this is. Carried so the inbound sync can stamp ownership
	// on what it downloads without a second lookup.
	EmployeeID int64
	Email      string
	Username   string
	Secret     string
	Domain     string
	// Outbound.
	Host     string
	Port     int
	Security string
	// Inbound. Same credentials, different door.
	IMAPHost     string
	IMAPPort     int
	IMAPSecurity string
}

// Login is the name to authenticate with. Most hosts want the full address;
// a few want only the local part, which is why username is stored rather
// than derived.
func (a MailAccount) Login() string {
	if a.Username != "" {
		return a.Username
	}
	return a.Email
}

// These reach a person on screen, so they say what to do rather than what is
// missing. They are in Chinese to match the rest of the service's
// user-facing errors.
var (
	// ErrNoMailAccount means the employee has not set their mailbox up yet.
	ErrNoMailAccount = errors.New("还没有保存邮箱账号，请先填写邮箱地址和授权码并保存")
	// ErrMailHostNotConfigured means nobody has entered the tenant's mail
	// host settings — the values that come off the mail host's console.
	ErrMailHostNotConfigured = errors.New("还没有配置发件服务器，请先在右侧填写 SMTP 服务器地址并保存")
)

// ForSender resolves and decrypts one employee's sending credentials.
//
// This is the only path in the service that turns secret_enc back into a
// usable string, and it is reachable only from the sending worker and the
// IMAP sync — never from anything that answers an HTTP request.
func (s *Service) ForSender(ctx context.Context, tenantID, senderID int64) (MailAccount, error) {
	if s.secrets == nil {
		return MailAccount{}, ErrNoKey
	}
	host, err := s.q.GetMailHost(ctx, tenantID)
	// Only an absent row means "not configured". Any other error is the
	// database failing, and reporting that as a missing setting sends
	// somebody to a dialog that is already filled in correctly — which is
	// exactly what a dropped connection during a restart used to do. What
	// broke has to be what gets said.
	if err == pgx.ErrNoRows {
		return MailAccount{}, ErrMailHostNotConfigured
	}
	if err != nil {
		return MailAccount{}, fmt.Errorf("读取发件服务器配置失败：%w", err)
	}
	if host.SmtpHost == "" {
		return MailAccount{}, ErrMailHostNotConfigured
	}

	row, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, EmployeeID: senderID,
	})
	if err == pgx.ErrNoRows {
		return MailAccount{}, ErrNoMailAccount
	}
	if err != nil {
		return MailAccount{}, fmt.Errorf("读取邮箱账号失败：%w", err)
	}
	if !row.IsActive {
		return MailAccount{}, errors.New("这个邮箱已被停用")
	}

	var secret []byte
	switch row.AuthKind {
	case "OAUTH":
		tok, err := s.accessTokenFor(ctx, tenantID, row.ID, row.OauthRefreshEnc)
		if err != nil {
			return MailAccount{}, err
		}
		secret = []byte(tok)
	default:
		if len(row.SecretEnc) == 0 {
			return MailAccount{}, errors.New("还没有填写授权码")
		}
		secret, err = s.secrets.Open(row.SecretEnc, AccountAAD(tenantID, row.ID))
		if err != nil {
			// Almost always a rotated or missing key. Say what to do about
			// it, because "unreadable" alone sends people to the database.
			s.log.Error("stored mailbox credential could not be decrypted",
				"account", row.ID, "key_version", row.KeyVersion, "err", err)
			return MailAccount{}, errors.New("已保存的授权码无法解密（加密密钥可能已更换），请重新填写一次授权码")
		}
	}

	return MailAccount{
		AccountID:    row.ID,
		AuthKind:     row.AuthKind,
		EmployeeID:   senderID,
		Email:        row.Email,
		Username:     row.Username,
		Secret:       string(secret),
		Domain:       host.Domain,
		Host:         host.SmtpHost,
		Port:         int(host.SmtpPort),
		Security:     host.SmtpSecurity,
		IMAPHost:     host.ImapHost,
		IMAPPort:     int(host.ImapPort),
		IMAPSecurity: host.ImapSecurity,
	}, nil
}

// RecordFailure notes a credential-level problem on the account so the
// settings page can show it. Best effort: failing to record why a send failed
// must not turn into a second failure.
func (s *Service) RecordFailure(ctx context.Context, tenantID, accountID int64, msg string) {
	const max = 500
	if len(msg) > max {
		msg = msg[:max]
	}
	if err := s.q.MarkMailAccountFailed(ctx, store.MarkMailAccountFailedParams{
		TenantID: tenantID, ID: accountID, LastError: msg,
	}); err != nil {
		s.log.Warn("could not record mailbox failure", "account", accountID, "err", err)
	}
}

// clearFailure wipes a recorded problem once the mailbox works again.
//
// Only touches last_error, never verified_at: whether the credential was ever
// verified is a different fact from whether the last sync went through, and
// conflating them would let a working poll masquerade as a fresh sign-in.
func (s *Service) clearFailure(ctx context.Context, tenantID, accountID int64) {
	if err := s.q.MarkMailAccountFailed(ctx, store.MarkMailAccountFailedParams{
		TenantID: tenantID, ID: accountID, LastError: "",
	}); err != nil {
		s.log.Warn("could not clear mailbox failure", "account", accountID, "err", err)
	}
}

// SaveMailAccount stores one employee's own mailbox settings.
//
// senderID is always the caller: there is deliberately no parameter for whose
// account this is. Nobody sets somebody else's authorisation code, including
// an administrator, because the only way to have one is to have been given it
// by the person it belongs to.
func (s *Service) SaveMailAccount(ctx context.Context, tenantID, employeeID int64, email, username, secret string) error {
	if s.secrets == nil {
		return ErrNoKey
	}
	// Same reasoning as the OAuth rebind: pointing this account at a different
	// address makes every stored message and UID meaningless, so the old
	// mailbox's synced data goes with the old binding.
	prev, prevErr := s.q.GetMyMailAccount(ctx, store.GetMyMailAccountParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	id, err := s.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
		TenantID: tenantID, EmployeeID: employeeID, Email: email, Username: username,
	})
	if err != nil {
		return err
	}
	if prevErr == nil && prev.Email != "" && !strings.EqualFold(prev.Email, email) {
		s.log.Info("mailbox rebound to a different address, clearing its synced mail",
			"employee", employeeID, "was", prev.Email, "now", email)
		_ = s.q.DeleteInboundForAccount(ctx, store.DeleteInboundForAccountParams{TenantID: tenantID, AccountID: id})
		_ = s.q.DeleteSyncStateForAccount(ctx, store.DeleteSyncStateForAccountParams{TenantID: tenantID, AccountID: id})
		s.sentFolders.Delete(fmt.Sprintf("sent:%d", id))
		s.sentFolders.Delete(fmt.Sprintf("junk:%d", id))
	}
	// An empty secret means "leave the stored one alone" — so that changing
	// a username does not require retyping the code, and so the settings form
	// never has to round-trip the secret to the browser to preserve it.
	if secret == "" {
		return nil
	}
	blob, err := s.secrets.Seal([]byte(secret), AccountAAD(tenantID, id))
	if err != nil {
		return err
	}
	return s.q.SetMailAccountSecret(ctx, store.SetMailAccountSecretParams{
		TenantID: tenantID, ID: id, SecretEnc: blob, KeyVersion: int32(s.secrets.Version()),
	})
}

// VerifyMailSecret proves the caller controls their mailbox, by logging in to
// the mail host with what they just typed.
//
// The typed credentials, deliberately not the stored ones: verifying storage
// would prove the system knows them, which it always does. The question this
// answers is whether the person at the keyboard does.
//
// With an email, this is the password sign-in — which is also the binding.
// The order is the invariant that matters: verify FIRST, and only a
// successful live login is allowed to touch storage. A mistyped code must
// never overwrite a working credential, and a failed password attempt must
// never destroy a Google binding. (Both happened when this saved first.)
//
// An empty detail with a nil error means verified. A non-empty detail with a
// nil error means there is nothing to verify — no mailbox bound, or no live
// mail channel — and the gate should open rather than lock somebody out of a
// page that holds nothing of theirs.
func (s *Service) VerifyMailSecret(ctx context.Context, tenantID, employeeID int64, email, secret string) (string, error) {
	row, err := s.q.GetMyMailAccount(ctx, store.GetMyMailAccountParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	bound := err == nil && row.Email != ""

	// An empty secret means "use what is already stored" — the Google door,
	// which has no code to type.
	//
	// It used to be an empty *address* that meant this, and that broke the
	// moment the address stopped being something callers supply: the gateway
	// now always sends the address from the session, so the no-address form
	// became unreachable and every post-OAuth verification fell through to
	// the branch below and demanded a password nobody has. Binding worked and
	// the gate stayed shut — a confusing pair, because the success and the
	// failure were both true.
	//
	// The secret is the better signal anyway. It is the thing the person did
	// or did not type, and it cannot be overloaded by a change somewhere else.
	email = strings.TrimSpace(email)
	if strings.TrimSpace(secret) == "" {
		return s.verifyBound(ctx, tenantID, employeeID, row, bound)
	}
	if email == "" {
		return "", errors.New("请输入邮箱地址")
	}
	if s.mailbox == nil {
		// No live mail channel (dev provider): nothing to verify against, so
		// the typed pair is stored as-is and the gate opens.
		if err := s.SaveMailAccount(ctx, tenantID, employeeID, email, "", secret); err != nil {
			return "", err
		}
		return "邮件通道未启用，已保存账号", nil
	}
	host, err := s.q.GetMailHost(ctx, tenantID)
	if err != nil && err != pgx.ErrNoRows {
		return "", fmt.Errorf("读取收件服务器配置失败：%w", err)
	}
	if err == pgx.ErrNoRows || host.ImapHost == "" {
		return "", ErrMailHostNotConfigured
	}

	// A stored custom login name survives while the address is unchanged; a
	// new address starts clean and logs in with the address itself.
	username := ""
	if bound && strings.EqualFold(row.Email, email) {
		username = row.Username
	}
	if err := s.mailbox.VerifyLogin(ctx, MailAccount{
		EmployeeID: employeeID,
		Email:      email, Username: username, Secret: secret,
		IMAPHost:     host.ImapHost,
		IMAPPort:     int(host.ImapPort),
		IMAPSecurity: host.ImapSecurity,
	}); err != nil {
		return "", err
	}

	// Proven live — only now may it become the stored binding.
	if err := s.SaveMailAccount(ctx, tenantID, employeeID, email, username, secret); err != nil {
		return "", err
	}
	s.markVerified(ctx, tenantID, employeeID)
	return "", nil
}

// verifyBound handles the no-address form of verification: proving a Google
// grant is still alive, or passing an unbound account through.
func (s *Service) verifyBound(ctx context.Context, tenantID, employeeID int64, row store.GetMyMailAccountRow, bound bool) (string, error) {
	if !bound {
		return "此账号未绑定邮箱，无需验证", nil
	}
	if s.mailbox == nil {
		return "邮件通道未启用，无需验证", nil
	}
	// A password-bound mailbox has no stored proof worth trusting here: a
	// stored code proves only that it worked once. Reaching this with no
	// secret typed means somebody pressed the password button with an empty
	// field, and the answer says so.
	if row.AuthKind != "OAUTH" {
		return "", errors.New("请输入邮箱密码或授权码")
	}

	host, err := s.q.GetMailHost(ctx, tenantID)
	if err != nil && err != pgx.ErrNoRows {
		return "", fmt.Errorf("读取收件服务器配置失败：%w", err)
	}
	if err == pgx.ErrNoRows || host.ImapHost == "" {
		return "", ErrMailHostNotConfigured
	}
	// An OAuth binding has no code to type: the person proved themselves on
	// Google's page when they bound it, and Google can revoke the grant at
	// any time. Verifying means proving the grant is still alive by
	// authenticating with it — a revoked one fails and relocks the mailbox.
	full, err := s.ForSender(ctx, tenantID, employeeID)
	if err != nil {
		return "", err
	}
	if err := s.mailbox.VerifyLogin(ctx, MailAccount{
		AccountID: row.ID, EmployeeID: employeeID, AuthKind: "OAUTH",
		Email: row.Email, Username: row.Username, Secret: full.Secret,
		IMAPHost:     host.ImapHost,
		IMAPPort:     int(host.ImapPort),
		IMAPSecurity: host.ImapSecurity,
	}); err != nil {
		return "", err
	}
	s.markVerified(ctx, tenantID, employeeID)
	return "", nil
}

// markVerified records the green tick after a live login. Best effort:
// failing to record it must not fail the unlock.
func (s *Service) markVerified(ctx context.Context, tenantID, employeeID int64) {
	row, err := s.q.GetMyMailAccount(ctx, store.GetMyMailAccountParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		return
	}
	if err := s.q.MarkMailAccountVerified(ctx, store.MarkMailAccountVerifiedParams{
		TenantID: tenantID, ID: row.ID,
	}); err != nil {
		s.log.Warn("could not record mailbox verification", "account", row.ID, "err", err)
	}
}
