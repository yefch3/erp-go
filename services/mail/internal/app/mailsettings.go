package app

import (
	"context"
	"errors"
	"strings"

	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// MailHostSettings is the tenant's connection configuration. No secrets, so
// it can be read back to a browser freely.
type MailHostSettings struct {
	Domain       string
	SMTPHost     string
	SMTPPort     int32
	SMTPSecurity string
	IMAPHost     string
	IMAPPort     int32
	IMAPSecurity string
	HourlyQuota  int32
	DailyQuota   int32
}

// MailAccountView is what the settings page shows about somebody's own
// mailbox. HasSecret rather than the secret: a value that is never sent to a
// browser cannot be leaked by one.
type MailAccountView struct {
	// ID 是这个信箱本身。一个人可以有好几个（00043），所以界面要能指名
	// 道姓地说「切到这个」「把这个设成默认」——光有地址不够，地址是可以
	// 长得很像的。
	ID         int64
	Email      string
	Username   string
	AuthKind   string
	HasSecret  bool
	VerifiedAt string
	// AuthFailed 说 LastError 是不是凭据问题——横幅上「重新登录」只认它。
	AuthFailed bool
	LastError  string
	IsActive   bool
	// IsDefault 是「写信时预选哪一个」。和登录地址无关。
	IsDefault bool
	// 这个信箱自己的收发服务器（00042 之后长在信箱行上）。界面上要显示
	// 「你这个箱走的是 imap.gmail.com」，不然跨服务商时人分不清哪个是哪个。
	SMTPHost string
	IMAPHost string
	// 发完信我们要不要自己往这个箱的已发送里留一份。**解析后的结果**：没人
	// 表过态时等于按主机猜出来的那个值，所以界面显示的永远是「实际会发生
	// 什么」，而不是一个空。见 MailAccount.ShouldKeepSentCopy。
	KeepSentCopy bool
	// Unread 是左侧那个角标：这个箱里有多少封没读。
	//
	// 多信箱之后这个数字是**切换的理由**——不显示的话，另一个箱里躺着客户
	// 的回信，人没有任何提示会切过去，直到下次偶然点开。收件箱那个总数不
	// 顶用：它现在只算当前这个箱（00043 之后按箱筛），说的正好是你已经在
	// 看的那一个。
	Unread int64
	// LastReadAt 是上次有人看这个箱的时间，空表示从没看过。
	LastReadAt string
	// UnboundAt 是解绑时间，空表示还绑着。
	//
	// 解绑 ≠ 删除：这一行还在，历史邮件照样读得到，只是不再收发。界面据此
	// 标成「已解绑」并收起收发相关的动作。
	UnboundAt string
}

var validSecurity = map[string]bool{"SSL": true, "STARTTLS": true, "NONE": true}

// normaliseDomain accepts what people actually type.
//
// A field labelled "domain" invites a whole address, and the only visible
// consequence of one is a malformed Message-ID like <key@lina@sunrise.com>,
// which nobody notices until deliverability quietly suffers. Fixing the input
// is cheaper than rejecting it.
func normaliseDomain(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func (s *Service) GetMailHost(ctx context.Context, tenantID int64) (MailHostSettings, error) {
	row, err := s.q.GetMailHost(ctx, tenantID)
	if err != nil {
		// Nothing configured yet is a normal state, not an error: the
		// settings page has to render before anybody can fill it in.
		return MailHostSettings{
			SMTPPort: 465, SMTPSecurity: "SSL",
			IMAPPort: 993, IMAPSecurity: "SSL",
			HourlyQuota: 100, DailyQuota: 500,
		}, nil
	}
	return MailHostSettings{
		Domain: row.Domain, SMTPHost: row.SmtpHost, SMTPPort: row.SmtpPort,
		SMTPSecurity: row.SmtpSecurity, IMAPHost: row.ImapHost, IMAPPort: row.ImapPort,
		IMAPSecurity: row.ImapSecurity, HourlyQuota: row.HourlyQuota, DailyQuota: row.DailyQuota,
	}, nil
}

func (s *Service) SaveMailHost(ctx context.Context, tenantID int64, in MailHostSettings) error {
	in.SMTPSecurity = strings.ToUpper(strings.TrimSpace(in.SMTPSecurity))
	in.IMAPSecurity = strings.ToUpper(strings.TrimSpace(in.IMAPSecurity))
	if !validSecurity[in.SMTPSecurity] || !validSecurity[in.IMAPSecurity] {
		return errors.New("加密方式只能是 SSL、STARTTLS 或 NONE")
	}
	if in.SMTPHost == "" {
		return errors.New("请填写 SMTP 服务器地址")
	}
	if in.SMTPPort <= 0 || in.SMTPPort > 65535 || in.IMAPPort <= 0 || in.IMAPPort > 65535 {
		return errors.New("端口必须在 1 到 65535 之间")
	}
	if in.HourlyQuota <= 0 || in.DailyQuota <= 0 {
		return errors.New("发送上限必须大于 0")
	}
	in.Domain = normaliseDomain(in.Domain)
	if err := s.q.UpsertMailHost(ctx, store.UpsertMailHostParams{
		TenantID: tenantID, Domain: in.Domain,
		SmtpHost: in.SMTPHost, SmtpPort: in.SMTPPort, SmtpSecurity: in.SMTPSecurity,
		ImapHost: in.IMAPHost, ImapPort: in.IMAPPort, ImapSecurity: in.IMAPSecurity,
		HourlyQuota: in.HourlyQuota, DailyQuota: in.DailyQuota,
	}); err != nil {
		return err
	}
	// **不再刷到已绑定的信箱上。**
	//
	// 00042 刚把主机搬到 mail_accounts 那会儿，这里有一句
	// SyncAccountHostsFromTenant：那时每个人只有一个箱、而且只可能是公司
	// 那一家，所以"改了公司配置就刷给所有人"和"改了自己的配置"是同一件事。
	//
	// 一个人能绑别家服务商的信箱之后，那一句就成了**破坏性**的：管理员在
	// 这个页面点一次保存，全公司每个人的 Gmail、163、QQ 信箱的服务器地址
	// 会被一起刷成公司那一套。之后那些箱收发全停，报的是认证失败，
	// 而管理员刚做的事和这个结果之间没有任何提示连着。
	//
	// mail_hosts 从此只剩一个角色：**新建信箱时的默认值模板**——认不出
	// 服务商时 resolveHosts 落回它，UpsertMailAccountShell 插入时种下它。
	// 改它只影响以后新绑的箱，不动已经绑好的。
	s.log.Info("mail host settings saved (template for new mailboxes only)", "tenant", tenantID)
	return nil
}

// ListMyMailboxes 是这个人名下的**全部**信箱，默认的排在最前。
//
// 取代了从前那个按 employee_id 取单行的 GetMyMailAccount。那一句在一人一箱
// 下没问题；放开之后它是 sqlc 的 :one，pgx 读到第一行就返回、不报「多行」
// 错，也没有 ORDER BY——「我的邮箱」会随机指向两个箱之一，绿勾、同步故障
// 横幅、reauth 跳哪扇门全都跟着随机，而且一个字都不报。
func (s *Service) ListMyMailboxes(ctx context.Context, tenantID, employeeID int64) ([]MailAccountView, error) {
	rows, err := s.q.ListMailAccountsForEmployee(ctx, store.ListMailAccountsForEmployeeParams{
		TenantID: tenantID, EmployeeID: employeeID,
	})
	if err != nil {
		return nil, err
	}
	// 每个箱的未读数一次问完，不是一个箱一次。这三个数字总是一起显示的。
	//
	// 读不到就当全是 0：角标少一个数字，比整块信箱列表打不开好——而列表
	// 打不开的话左边那排就没了，人连切换都做不到。
	unread := make(map[int64]int64, len(rows))
	counts, err := s.q.CountUnreadByMailbox(ctx, store.CountUnreadByMailboxParams{
		TenantID: tenantID, OwnerID: employeeID,
	})
	if err != nil {
		s.log.Warn("could not count unread per mailbox", "employee", employeeID, "err", err)
	}
	for _, c := range counts {
		unread[c.AccountID] = c.Unread
	}
	out := make([]MailAccountView, 0, len(rows))
	for _, row := range rows {
		v := MailAccountView{
			ID: row.ID, Email: row.Email, Username: row.Username,
			AuthKind: row.AuthKind, LastError: row.LastError, AuthFailed: row.AuthFailed,
			IsActive: row.IsActive, IsDefault: row.IsDefault,
			SMTPHost: row.SmtpHost, IMAPHost: row.ImapHost,
			HasSecret: s.hasCredential(ctx, tenantID, row.ID),
			Unread:    unread[row.ID],
			// 解析后的值，不是库里那个可空列：没人表过态时显示按主机猜出来
			// 的那个，所以界面上永远是「实际会发生什么」，不是一个空。
			KeepSentCopy: MailAccount{
				Host: row.SmtpHost, KeepSentCopy: row.KeepSentCopy,
			}.ShouldKeepSentCopy(),
		}
		if row.VerifiedAt.Valid {
			v.VerifiedAt = row.VerifiedAt.Time.Format("2006-01-02 15:04")
		}
		if row.LastReadAt.Valid {
			v.LastReadAt = row.LastReadAt.Time.Format("2006-01-02 15:04")
		}
		if row.UnboundAt.Valid {
			v.UnboundAt = row.UnboundAt.Time.Format("2006-01-02 15:04")
		}
		out = append(out, v)
	}
	return out, nil
}

// hasCredential 单独读一次密文列，而不是把它选进上面那句。
//
// 让「设置页读的那句查询」**根本没有能力**返回凭据，值得多一次往返。
//
// 两列都看：凭据只会在其中一列，不会同时在两列——密码账号填 secret_enc，
// Google 账号填 oauth_refresh_enc 而 secret_enc 是空的。只看第一列的话，
// 全公司用 Google 登录的人都会被答成「没有凭据」，而那句话在一个邮箱明明
// 好用的人看来就是「你还没设置邮箱」。
func (s *Service) hasCredential(ctx context.Context, tenantID, accountID int64) bool {
	sec, err := s.q.GetMailAccountSecret(ctx, store.GetMailAccountSecretParams{
		TenantID: tenantID, ID: accountID,
	})
	if err != nil {
		return false
	}
	return len(sec.SecretEnc) > 0 || len(sec.OauthRefreshEnc) > 0
}

// GetMyMailAccount 回一个信箱的门牌：地址、登录名、用密码还是用 Google。
//
// accountID = 0 回默认箱。「他有没有绑箱」这类问题问默认箱就够。
//
// 点了名就回那一个——解锁门要用它。绑了两个箱的人被挡在 163 那个门前，
// 门上却写着默认的 QQ 地址，人就会把 163 的授权码填进去；而那句注释
// （「在一个没写名字的框里输密码，正是把密码输错地方的方式」）本来就是
// 为了防这件事，多信箱之后它反倒成了错的名字。
//
// 点了名但不是他的箱：回空壳，不是别人的箱，也不报错。门上什么都不写，
// 好过写错一个名字；报错则会变成一个拿 id 探别人信箱的口子。
//
// 一个都没有时也回空壳而不是报错：设置页要能显示「还没绑」。
func (s *Service) GetMyMailAccount(ctx context.Context, tenantID, employeeID, accountID int64) (MailAccountView, error) {
	boxes, err := s.ListMyMailboxes(ctx, tenantID, employeeID)
	if err != nil || len(boxes) == 0 {
		return MailAccountView{IsActive: true}, nil
	}
	if accountID > 0 {
		for _, b := range boxes {
			if b.ID == accountID {
				return b, nil
			}
		}
		return MailAccountView{IsActive: true}, nil
	}
	return boxes[0], nil
}
