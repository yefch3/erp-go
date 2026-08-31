package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 绑定信箱：从「填了一个地址和一串授权码」到「库里多了一个能收发的信箱」。
//
// **这条路以前不接受地址。** 地址取自登录令牌，`verifyMailbox` 的请求体里
// 故意没有 email 字段，源码注释写着 "must never become one again"，还有一条
// 读源码的测试守着。那条约束防的是「以 alice@thecompany.com 登录，却绑一个
// 私人信箱」——下游全都不一致：ERP 说是这个人发的，客户看到的是另一个地址。
//
// 现在业务口径改了：**一个人可以绑多个信箱，而且不必是公司域名的**
// （ERP 账号是 263 的人，邮箱这边可以只绑 Gmail）。所以那条约束退役，
// 换上的是这几条：
//
//   - 必须已登录，绑的永远是**自己**名下（employeeID 来自令牌，不是请求体）
//   - 一个地址只能属于一个人（UNIQUE (tenant_id, email)，撞上说人话）
//   - 必须**先真的登录成功**，才允许写进库——错的授权码不会覆盖能用的凭据
//   - 收发服务器由**服务端**的服务商表决定，不收调用方给的主机名
//     （见 mailproviders.go：收了就等于任何员工都能让邮件服务带着凭据去连
//     任意 host:port）
//   - 每一次绑定都留痕，成功和失败都记（见 00045）
//
// 老约束「地址等于登录地址」换来的是「地址属于谁、谁绑的、什么时候」全部
// 可查——前者靠一个字段不存在，后者靠一张表。

// hostRejected 标记「这是邮件服务器说的不行」，不是我们自己说的。
//
// 对调用方来说两者长得一样——都是一次验证失败——但只有一种真的花掉了一次
// 对 Gmail 或 263 的登录，也只有那一种该扣这个人的尝试次数。这个服务自己
// 判定的（没填地址、没配服务器、存着的码解不开）根本没出过我们的网。
type hostRejected struct{ err error }

func (e hostRejected) Error() string { return e.err.Error() }
func (e hostRejected) Unwrap() error { return e.err }

// FromMailHost reports whether the host is what refused.
func FromMailHost(err error) bool {
	var t hostRejected
	return errors.As(err, &t)
}

// BindRequest 是绑定/复验一个信箱要的全部输入。
//
// 用结构体而不是一串位置参数：这里有七个字段，其中五个是可选的主机覆盖，
// 位置参数版本第一次加字段就会有人填错顺序而且编译得过。
type BindRequest struct {
	// Email 是要绑的地址。空表示「复验已经绑好的那个」（Google 那条路）。
	Email string
	// Provider 是服务商代号（见 mailproviders.go）。空表示按地址后缀猜；
	// 猜不出来就落回这家公司自己的收发服务器配置。
	Provider string
	// Secret 是现在输入的授权码。空表示复验，不是「用存着的那个再登一次」
	// ——验存着的只能证明系统知道它，不能证明键盘前的人知道。
	Secret string

	// 「其他」那一档才用得上：服务商不在表里时员工自己填的主机。
	// 走 validateCustomHost，只放行公网域名和标准邮件端口。
	SMTPHost     string
	SMTPPort     int32
	SMTPSecurity string
	IMAPHost     string
	IMAPPort     int32
	IMAPSecurity string
}

// BindResult 告诉调用方绑上的到底是哪一个信箱。
//
// 回地址和 id，是因为地址现在由调用方给：请求里写的和真正绑上的可能不同
// （大小写、首尾空格），而界面要显示「你绑好了哪一个」。Detail 是给人看的
// 一句话，空表示没有额外要说的。
type BindResult struct {
	AccountID int64
	Email     string
	Detail    string
}

// bindAction 是留痕里的动作名。
const (
	bindActionBind    = "BIND"
	bindActionRebind  = "REBIND"
	bindActionFailed  = "FAILED"
	bindActionDefault = "DEFAULT"
)

// VerifyMailSecret 证明键盘前的人控制着这个信箱——办法是拿他刚输入的东西
// 去真的登录一次收信服务器。登录成功，这一对才被允许写进库。
func (s *Service) VerifyMailSecret(
	ctx context.Context, tenantID, employeeID int64, in BindRequest,
) (BindResult, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	secret := strings.TrimSpace(in.Secret)

	// 空授权码 = 「复验已经绑好的那个」，也就是 Google 那扇门：它没有码可输。
	//
	// 从前判断这件事看的是**地址**为空，那在地址由网关从令牌填进来之后就
	// 永远为假了——每一次 OAuth 之后的复验都掉进下面那个分支，要一个没人
	// 有的密码。绑定成功而门不开，两件事都是真的，人却看不懂。
	// 授权码是更好的信号：它就是那个人输了或没输的东西。
	if secret == "" {
		return s.verifyBound(ctx, tenantID, employeeID, email)
	}
	if email == "" {
		return BindResult{}, apierr.Invalid("MAIL_ADDRESS_REQUIRED", "请输入邮箱地址")
	}
	if !strings.Contains(email, "@") || strings.HasSuffix(email, "@") {
		return BindResult{}, apierr.Invalid("MAIL_ADDRESS_INVALID", "邮箱地址格式不对")
	}

	hosts, err := s.resolveHosts(ctx, tenantID, email, in)
	if err != nil {
		s.recordBinding(ctx, tenantID, employeeID, 0, email, in.Provider,
			bindActionFailed, err.Error())
		return BindResult{}, err
	}

	if s.mailbox == nil {
		// 没有真的邮件通道（开发环境的假 provider）：没什么可验的，照存，
		// 门也开。这条分支是有意留的——本地开发不该被迫连真的邮件服务器。
		res, err := s.storeBinding(ctx, tenantID, employeeID, email, "", secret, hosts, in.Provider)
		if err != nil {
			return BindResult{}, err
		}
		res.Detail = "邮件通道未启用，已保存账号"
		return res, nil
	}

	// 同一个地址重填授权码时，保留原来那个自定义登录名——有些主机要的是
	// local part 而不是全地址，清掉就登不上了。换一个地址是新的信箱，
	// 从头开始，用地址本身登录。
	username := ""
	if prev, err := s.q.GetMailAccountByEmail(ctx, store.GetMailAccountByEmailParams{
		TenantID: tenantID, Email: email,
	}); err == nil && prev.EmployeeID == employeeID {
		username = prev.Username
	}

	if err := s.mailbox.VerifyLogin(ctx, MailAccount{
		EmployeeID: employeeID,
		Email:      email, Username: username, Secret: secret,
		IMAPHost:     hosts.IMAPHost,
		IMAPPort:     int(hosts.IMAPPort),
		IMAPSecurity: hosts.IMAPSecurity,
	}); err != nil {
		s.recordBinding(ctx, tenantID, employeeID, 0, email, in.Provider,
			bindActionFailed, err.Error())
		return BindResult{}, hostRejected{err}
	}

	// 活着证明过了，现在才允许落库。顺序不能反：先存后验的话，一次输错的
	// 授权码会把本来能用的凭据覆盖掉，或者把一个 Google 绑定毁掉。
	return s.storeBinding(ctx, tenantID, employeeID, email, username, secret, hosts, in.Provider)
}

// storeBinding 把验过的这一对写进库：行、主机、密文、绿勾、留痕。
func (s *Service) storeBinding(
	ctx context.Context, tenantID, employeeID int64,
	email, username, secret string, hosts MailProvider, provider string,
) (BindResult, error) {
	if s.secrets == nil {
		return BindResult{}, ErrNoKey
	}
	// 冲突键是**地址**：同一个地址重填是更新，换一个地址是新增一个信箱。
	// 属于别人的地址整句既不插也不更，RETURNING 空手而归。
	before, beforeErr := s.q.GetMailAccountByEmail(ctx, store.GetMailAccountByEmailParams{
		TenantID: tenantID, Email: email,
	})
	id, err := s.q.UpsertMailAccountShell(ctx, store.UpsertMailAccountShellParams{
		TenantID: tenantID, EmployeeID: employeeID, Email: email, Username: username,
	})
	if err != nil {
		err = translateMailboxTaken(err)
		s.recordBinding(ctx, tenantID, employeeID, 0, email, provider,
			bindActionFailed, err.Error())
		return BindResult{}, err
	}
	if err := s.q.SetMailAccountHosts(ctx, store.SetMailAccountHostsParams{
		TenantID: tenantID, ID: id,
		Domain:   hosts.Domain,
		SmtpHost: hosts.SMTPHost, SmtpPort: hosts.SMTPPort, SmtpSecurity: hosts.SMTPSecurity,
		ImapHost: hosts.IMAPHost, ImapPort: hosts.IMAPPort, ImapSecurity: hosts.IMAPSecurity,
	}); err != nil {
		return BindResult{}, err
	}
	blob, err := s.secrets.Seal([]byte(secret), AccountAAD(tenantID, id))
	if err != nil {
		return BindResult{}, err
	}
	if err := s.q.SetMailAccountSecret(ctx, store.SetMailAccountSecretParams{
		TenantID: tenantID, ID: id, SecretEnc: blob, KeyVersion: int32(s.secrets.Version()),
	}); err != nil {
		return BindResult{}, err
	}
	s.markVerified(ctx, tenantID, id)

	action := bindActionBind
	if beforeErr == nil && before.ID == id {
		action = bindActionRebind
	}
	s.recordBinding(ctx, tenantID, employeeID, id, email, provider, action, "")
	return BindResult{AccountID: id, Email: email}, nil
}

// resolveHosts 决定这次绑定该连哪台服务器。
//
// 三条路，从最确定到最不确定：
//
//  1. 员工在表单里挑了服务商 → 用表里的值
//  2. 挑了「其他」→ 用他填的，先过 validateCustomHost
//  3. 什么都没挑 → 按地址后缀猜（只对个人邮箱有用）；猜不出来就落回这家
//     公司自己配的那套（263、腾讯企业邮这些用公司自己的域名，看不出来）
//
// 都不成立时报错报得具体：说「请选择邮件服务商」，而不是「没配置服务器」
// ——后者会把人送到一个他没有权限打开的管理员页面。
func (s *Service) resolveHosts(
	ctx context.Context, tenantID int64, email string, in BindRequest,
) (MailProvider, error) {
	code := strings.ToLower(strings.TrimSpace(in.Provider))

	if code == ProviderCodeOther {
		if err := validateCustomHost(in.IMAPHost, in.IMAPPort); err != nil {
			return MailProvider{}, err
		}
		if err := validateCustomHost(in.SMTPHost, in.SMTPPort); err != nil {
			return MailProvider{}, err
		}
		return MailProvider{
			Domain:   domainOf(email),
			SMTPHost: strings.ToLower(strings.TrimSpace(in.SMTPHost)),
			SMTPPort: in.SMTPPort, SMTPSecurity: normalizeSecurity(in.SMTPSecurity),
			IMAPHost: strings.ToLower(strings.TrimSpace(in.IMAPHost)),
			IMAPPort: in.IMAPPort, IMAPSecurity: normalizeSecurity(in.IMAPSecurity),
		}, nil
	}

	if code != "" {
		p, ok := MailProviderByCode(code)
		if !ok {
			// 认不得的代号不猜。前端的清单和这张表用同一串代号，对不上
			// 说明两边各改了一半——那种漂移宁可当场炸。
			return MailProvider{}, apierr.Invalid("MAIL_PROVIDER_UNKNOWN",
				"认不出这个邮件服务商，请重新选择")
		}
		if p.NeedsOAuth {
			return MailProvider{}, apierr.Invalid("MAIL_PROVIDER_NEEDS_OAUTH",
				"这家服务商已经不允许用密码登录邮箱了，请改用它自己的授权登录")
		}
		p.Domain = domainOf(email)
		return p, nil
	}

	if p, ok := MailProviderForAddress(email); ok {
		if p.NeedsOAuth {
			return MailProvider{}, apierr.Invalid("MAIL_PROVIDER_NEEDS_OAUTH",
				"这家服务商已经不允许用密码登录邮箱了，请改用它自己的授权登录")
		}
		p.Domain = domainOf(email)
		return p, nil
	}

	// 落回这家公司自己配的那套。企业邮的域名是公司自己的，从地址看不出
	// 托管在哪家，所以这一步是必需的，不是兜底。
	host, err := s.q.GetMailHost(ctx, tenantID)
	if err != nil && err != pgx.ErrNoRows {
		return MailProvider{}, fmt.Errorf("读取收发服务器配置失败：%w", err)
	}
	if err == pgx.ErrNoRows || host.ImapHost == "" {
		return MailProvider{}, apierr.Invalid("MAIL_PROVIDER_REQUIRED",
			"认不出这个地址属于哪家邮件服务商，请在上面选一个")
	}
	return MailProvider{
		Domain:   host.Domain,
		SMTPHost: host.SmtpHost, SMTPPort: host.SmtpPort, SMTPSecurity: host.SmtpSecurity,
		IMAPHost: host.ImapHost, IMAPPort: host.ImapPort, IMAPSecurity: host.ImapSecurity,
	}, nil
}

// normalizeSecurity 把加密方式收进那三个合法值。数据库上有 CHECK 约束，
// 不收的话一个拼错的大小写会变成 23514，而那个错误对人毫无意义。
func normalizeSecurity(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "STARTTLS":
		return "STARTTLS"
	case "NONE":
		return "NONE"
	default:
		return "SSL"
	}
}

// verifyBound 是「不输授权码」那条路：证明一个 Google 授权还活着，或者
// 干脆告诉一个还没绑过的人没什么可验的。
//
// email 为空时验默认信箱。**按地址取，不按人取**——一个人有两个信箱时，
// 按人取的那句 SQL 是 sqlc 的 :one，pgx 读到第一行就返回、不报错，于是
// 验的可能是另一个箱，而绿勾会盖到它头上。
func (s *Service) verifyBound(
	ctx context.Context, tenantID, employeeID int64, email string,
) (BindResult, error) {
	const unbound = "此账号未绑定邮箱，无需验证"
	var accountID int64
	if email != "" {
		got, err := s.q.GetMailAccountByEmail(ctx, store.GetMailAccountByEmailParams{
			TenantID: tenantID, Email: email,
		})
		// 别人绑走的地址在这里也走「没绑过」：复验是只读的，说得再细一点
		// 就成了「这个地址在这家公司有没有人用」的探测口。
		if err != nil || got.EmployeeID != employeeID {
			return BindResult{Detail: unbound}, nil
		}
		accountID = got.ID
	} else {
		id, err := s.defaultAccountIDFor(ctx, tenantID, employeeID)
		if err != nil {
			return BindResult{Detail: unbound}, nil
		}
		accountID = id
	}
	row, err := s.q.GetMailAccountByID(ctx, store.GetMailAccountByIDParams{
		TenantID: tenantID, ID: accountID,
	})
	if err != nil || row.EmployeeID != employeeID {
		return BindResult{Detail: unbound}, nil
	}

	if s.mailbox == nil {
		return BindResult{AccountID: row.ID, Email: row.Email, Detail: "邮件通道未启用，无需验证"}, nil
	}
	// 密码绑的信箱在这里没有值得信的存档凭证：存着的码只能证明它曾经能用。
	// 走到这里而没输码，说明有人对着空输入框按了密码那个按钮，就这么说。
	if row.AuthKind != "OAUTH" {
		return BindResult{}, errors.New("请输入邮箱密码或授权码")
	}
	if row.ImapHost == "" {
		return BindResult{}, ErrMailHostNotConfigured
	}

	// OAuth 绑定没有码可输：那个人当初在 Google 的页面上证明过自己，而
	// Google 随时可以撤销。复验就是拿它去认证一次——被撤销的会失败，
	// 门重新锁上。
	full, err := s.ForAccount(ctx, tenantID, row.ID)
	if err != nil {
		return BindResult{}, err
	}
	if err := s.mailbox.VerifyLogin(ctx, MailAccount{
		AccountID: row.ID, EmployeeID: employeeID, AuthKind: "OAUTH",
		Email: row.Email, Username: row.Username, Secret: full.Secret,
		IMAPHost:     row.ImapHost,
		IMAPPort:     int(row.ImapPort),
		IMAPSecurity: row.ImapSecurity,
	}); err != nil {
		return BindResult{}, hostRejected{err}
	}
	s.markVerified(ctx, tenantID, row.ID)
	return BindResult{AccountID: row.ID, Email: row.Email}, nil
}

// markVerified 记下那个绿勾。尽力而为：记不上不该让解锁失败。
//
// 参数是**账号 id**。从前是员工 id，然后在里面按员工取单行——一个人两个
// 信箱时，验 A 箱成功会把绿勾盖到 B 箱上，而 B 箱的凭据可能已经坏了。
func (s *Service) markVerified(ctx context.Context, tenantID, accountID int64) {
	if err := s.q.MarkMailAccountVerified(ctx, store.MarkMailAccountVerifiedParams{
		TenantID: tenantID, ID: accountID,
	}); err != nil {
		s.log.Warn("could not record mailbox verification", "account", accountID, "err", err)
	}
}

// recordBinding 写一行留痕。尽力而为，但**失败也记**——只记成功的话，
// 反复拿别人的地址试探正好是看不见的那一半。
func (s *Service) recordBinding(
	ctx context.Context, tenantID, employeeID, accountID int64,
	email, provider, action, detail string,
) {
	const maxDetail = 500
	if len(detail) > maxDetail {
		detail = detail[:maxDetail]
	}
	var acct *int64
	if accountID > 0 {
		acct = &accountID
	}
	if err := s.q.RecordMailBinding(ctx, store.RecordMailBindingParams{
		TenantID: tenantID, EmployeeID: employeeID, AccountID: acct,
		Email: email, Provider: strings.ToLower(strings.TrimSpace(provider)),
		Action: action, Detail: detail,
	}); err != nil {
		s.log.Warn("could not record mailbox binding", "employee", employeeID, "err", err)
	}
}
