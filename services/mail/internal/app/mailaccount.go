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

// ForSender 从「谁发的」找到「用哪个信箱」，再走 ForAccount。
//
// 00047 之后发信路径不再走它：队列自己记着 account_id，smtp.go 直接
// ForAccount。剩下的唯一用处是那条兜底——00047 之前入队、account_id 还是 0
// 的那些行（见 smtp.go 里的分支）。队列排空之后它就没人调了。
//
// 用它的时候要知道它答的是什么：**「这个人的默认信箱」，不是「这封信本来
// 要从哪个信箱发」**。所以只有在后者确实答不上来时才该用它。
func (s *Service) ForSender(ctx context.Context, tenantID, senderID int64) (MailAccount, error) {
	accountID, err := s.defaultAccountIDFor(ctx, tenantID, senderID)
	if err != nil {
		return MailAccount{}, err
	}
	return s.ForAccount(ctx, tenantID, accountID)
}

// defaultAccountIDFor 找这个人「用来发信」的那个信箱。
//
// 「多个候选」曾经是要吵出来的事——第一期到第三期之间，出站队列还没有
// account_id，绑了两个箱的人发信会随机挑一个，而那不会报任何错。00047 之后
// 队列自己记着从哪个箱发，这里退回它本来的意思：**没点名时用哪一个**。
// 那是个正常问题，不是警告，所以不再打日志。
//
// 排序按 is_default DESC——默认箱是人选的，id 顺序是随机的历史。回填
// （00047:47）用的是同一个排序，两处必须一致，否则回填填的和运行时算的
// 不是一个箱。
func (s *Service) defaultAccountIDFor(ctx context.Context, tenantID, employeeID int64) (int64, error) {
	rows, err := s.q.ListMailAccountsForEmployee(ctx, store.ListMailAccountsForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		return 0, fmt.Errorf("读取邮箱账号失败：%w", err)
	}
	// **跳过解绑的。** 列表要把它们带回来（左栏还得显示那一行），而「用哪个
	// 箱发信」不能落在一个已经没有凭据的箱上——那样发信会在最后一步失败，
	// 而失败的样子是「信发不出去」，跟发件人选错了长得一模一样。
	for _, r := range rows {
		if !r.UnboundAt.Valid {
			return r.ID, nil
		}
	}
	// 全解绑了，等于一个能用的箱都没有。
	return 0, ErrNoMailAccount
}

// RecordFailure notes a credential-level problem on the account so the
// settings page can show it. Best effort: failing to record why a send failed
// must not turn into a second failure.
//
// authProblem 说这次失败是不是凭据的问题。它决定页面上那颗「重新登录邮箱」
// 出不出现，所以由知道原因的调用方给，不从 msg 的文字里猜。
func (s *Service) RecordFailure(ctx context.Context, tenantID, accountID int64, msg string, authProblem bool) {
	// 按字符截，不按字节：切开一个中文会留下无效的 UTF-8，而 Postgres 的
	// text 列拒收（22021），于是这句"记一下哪里出错了"自己也失败了。
	msg = truncateUTF8(msg, 500)
	if err := s.q.MarkMailAccountFailed(ctx, store.MarkMailAccountFailedParams{
		TenantID: tenantID, ID: accountID, LastError: msg, AuthFailed: authProblem,
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
		TenantID: tenantID, ID: accountID, LastError: "", AuthFailed: false,
	}); err != nil {
		s.log.Warn("could not clear mailbox failure", "account", accountID, "err", err)
	}
}
