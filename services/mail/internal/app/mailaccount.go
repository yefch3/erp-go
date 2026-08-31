package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
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

// ForAccount resolves and decrypts one **mailbox's** credentials.
//
// This is the only path in the service that turns secret_enc back into a
// usable string, and it is reachable only from the sending worker and the
// IMAP sync — never from anything that answers an HTTP request.
//
// 参数是账号 id，不是员工 id。从前是员工 id——在「一人一箱」下两者等价，
// 而那个前提正要被拿掉。按员工查的那一版有个不会报错的坏法：那句 SQL 是
// sqlc 的 :one，生成 QueryRow，pgx **读到第一行就返回**，一个人有两行时
// 既不报错也没有 ORDER BY，于是发信随机挑箱、同步只同步被挑中的那个，
// 另一个箱一封信都收不到，而日志里什么都没有。
//
// 主机配置从账号行上读（00042 之前在 mail_hosts 上，一家公司一份）。
func (s *Service) ForAccount(ctx context.Context, tenantID, accountID int64) (MailAccount, error) {
	if s.secrets == nil {
		return MailAccount{}, ErrNoKey
	}
	row, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, ID: accountID,
	})
	if err == pgx.ErrNoRows {
		return MailAccount{}, ErrNoMailAccount
	}
	if err != nil {
		return MailAccount{}, fmt.Errorf("读取邮箱账号失败：%w", err)
	}
	// 主机没配等于这个信箱还不能收发。和从前一样只把「确实没配」说成没配，
	// 数据库出错要如实报——把连接中断说成「设置没填」，会把人送到一个已经
	// 填好的对话框前面。
	if row.SmtpHost == "" {
		return MailAccount{}, ErrMailHostNotConfigured
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
		EmployeeID:   row.EmployeeID,
		Email:        row.Email,
		Username:     row.Username,
		Secret:       string(secret),
		Domain:       row.Domain,
		Host:         row.SmtpHost,
		Port:         int(row.SmtpPort),
		Security:     row.SmtpSecurity,
		IMAPHost:     row.ImapHost,
		IMAPPort:     int(row.ImapPort),
		IMAPSecurity: row.ImapSecurity,
	}, nil
}

// SetDefaultMailbox 换这个人写信时预选的信箱。
//
// 清旧和设新分两条语句，包在一个事务里：部分唯一索引是立即检查的，
// 一条语句里同时存在两个 TRUE 会被拒（见 ClearDefaultMailbox 的注释）。
// 事务保证外面看不到「零个默认」那一瞬。
func (s *Service) SetDefaultMailbox(ctx context.Context, tenantID, employeeID, accountID int64) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ClearDefaultMailbox(ctx, store.ClearDefaultMailboxParams{
			TenantID: tenantID, EmployeeID: employeeID,
		}); err != nil {
			return err
		}
		n, err := q.MarkDefaultMailbox(ctx, store.MarkDefaultMailboxParams{
			TenantID: tenantID, EmployeeID: employeeID, ID: accountID,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			// 事务回滚，所以原来的默认还在——不会因为选错一个 id 就把人
			// 变成"一个默认都没有"。
			return apierr.NotFound("MAIL_ACCOUNT_NOT_FOUND", "这个邮箱不在你名下")
		}
		return nil
	})
}

// translateMailboxTaken 把 UNIQUE (tenant_id, email) 的违反翻成人话。
//
// 这条约束在 00043 里被有意留下了：一个信箱只能属于一个人。两个人绑同一个
// 地址，谁都说不清那封信该算谁的，两边的授权码还会互相覆盖。撞上时给的是
// 数据库的 23505，对着填表的人说这个等于什么都没说。
func translateMailboxTaken(err error) error {
	taken := apierr.Conflict("MAIL_ADDRESS_TAKEN",
		"这个邮箱地址已经被本公司的另一个人绑定了")
	// 两种到达方式：
	//
	//  · 23505 —— UPDATE 一行去撞别人已经占着的地址；
	//  · ErrNoRows —— INSERT 撞了地址，而 DO UPDATE 的 WHERE 因为那一行
	//    属于别人而不匹配，于是既没插也没更，RETURNING 空手而归。
	//    **这条比看起来重要**：没有那个 WHERE 的话，这里会是"成功"，
	//    而代价是别人的信箱被划走。
	if errors.Is(err, pgx.ErrNoRows) {
		return taken
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		strings.Contains(pgErr.ConstraintName, "email") {
		return taken
	}
	return err
}

// ForSender 是发信路径专用的过渡入口：从「谁发的」找到「用哪个信箱」，
// 再走 ForAccount。
//
// 出站队列（email_messages）今天只记 sender_id，不记 account_id，所以这一步
// 反查躲不掉。第三期给队列加上 account_id 之后，provider.Accounts 接口改成
// 直接收账号 id，这个方法和 defaultAccountIDFor 一起删。
//
// 在那之前它有个必须知道的性质：**它答的是「这个人的默认信箱」，不是「这封
// 信本来要从哪个信箱发」**。一封排队中的信重试时，如果这个人期间改了默认
// 信箱，重试会从另一个地址发出去。第三期就是为了消掉这件事。
func (s *Service) ForSender(ctx context.Context, tenantID, senderID int64) (MailAccount, error) {
	accountID, err := s.defaultAccountIDFor(ctx, tenantID, senderID)
	if err != nil {
		return MailAccount{}, err
	}
	return s.ForAccount(ctx, tenantID, accountID)
}

// defaultAccountIDFor 找这个人「用来发信」的那个信箱。
//
// **这是第一期的过渡桥。** 凭据已经改成按账号取了，而出站队列还没有
// account_id（那是第三期的事），所以发信这一侧暂时还得从人反查回信箱。
// 今天 mail_accounts 上的 UNIQUE (tenant_id, employee_id) 保证答案唯一。
//
// 第二期放开那条约束、而第三期还没给队列加上 account_id 的那段时间里，
// 这里会真的有多个候选。**那种情况必须吵出来**：从前按员工取单行的写法
// 在这里是 sqlc 的 :one，pgx 读到第一行就返回、不报错，于是发信随机挑箱，
// 另一个信箱看起来好好的、其实一封都发不出去，日志里一个字都没有。
func (s *Service) defaultAccountIDFor(ctx context.Context, tenantID, employeeID int64) (int64, error) {
	rows, err := s.q.ListMailAccountsForEmployee(ctx, store.ListMailAccountsForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		return 0, fmt.Errorf("读取邮箱账号失败：%w", err)
	}
	if len(rows) == 0 {
		return 0, ErrNoMailAccount
	}
	if len(rows) > 1 {
		s.log.Warn("这个人名下有多个信箱，而发信路径还没有账号维度——先用他的默认信箱。"+
			"出站队列要在第三期带上 account_id，那之前一封排队中的信重试时"+
			"可能从另一个地址发出去",
			"tenant", tenantID, "employee", employeeID, "accounts", len(rows))
	}
	// 查询按 is_default DESC 排序，所以第一行就是默认信箱——不是"id 最小的
	// 那个"。这一点是刻意的：默认信箱是人选的，而 id 顺序是随机的历史。
	return rows[0].ID, nil
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
	//
	// 这里必须先问「这个人有没有信箱」再决定改还是加。00043 之后
	// UpsertMailAccountShell 的冲突键是**地址**，所以直接调它、填一个新地址
	// 会新增一行；而设置页上的「改邮箱地址」要的是"这一个信箱换个地址"。
	// 在只有一个信箱的年代这两件事看起来一样，现在不一样了——不分开的话，
	// 改完地址的人会多出一个信箱，而发信仍然走原来那个（它还是默认）。
	prev, prevErr := s.q.ListMailAccountsForEmployee(ctx, store.ListMailAccountsForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	var id int64
	var err error
	switch {
	case prevErr == nil && len(prev) > 0:
		id = prev[0].ID
		if !strings.EqualFold(prev[0].Email, email) {
			if err := s.q.UpdateMailAccountAddress(ctx, store.UpdateMailAccountAddressParams{
				TenantID: tenantID, ID: id, Email: email, Username: username,
			}); err != nil {
				return translateMailboxTaken(err)
			}
		}
	default:
		id, err = s.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
			TenantID: tenantID, EmployeeID: employeeID, Email: email, Username: username,
		})
		if err != nil {
			return translateMailboxTaken(err)
		}
	}
	if prevErr == nil && len(prev) > 0 && prev[0].Email != "" && !strings.EqualFold(prev[0].Email, email) {
		s.log.Info("mailbox rebound to a different address, clearing its synced mail",
			"employee", employeeID, "was", prev[0].Email, "now", email)
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
// hostRejected marks an error as the mail host's answer rather than ours.
//
// The two look identical to a caller — both are just a failed verification —
// but only one of them spent a real login against Gmail or 263, and only that
// one should cost the person an attempt. Everything this service decides on
// its own (no address, no host configured, an undecryptable stored code)
// never reached the host at all.
type hostRejected struct{ err error }

func (e hostRejected) Error() string { return e.err.Error() }
func (e hostRejected) Unwrap() error { return e.err }

// FromMailHost reports whether the host is what refused.
func FromMailHost(err error) bool {
	var t hostRejected
	return errors.As(err, &t)
}

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
		return "", hostRejected{err}
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
	full, err := s.ForAccount(ctx, tenantID, row.ID)
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
		return "", hostRejected{err}
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
