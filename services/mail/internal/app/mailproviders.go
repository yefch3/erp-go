package app

import (
	"fmt"
	"net"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// 邮件服务商预设：从员工填的地址，推出该用哪台收发服务器。
//
// **为什么这张表在服务端。** 绑定的动作是「拿着这个授权码去登录那台主机」。
// 主机名如果由调用方给，任何一个登录了的员工都能让邮件服务带着凭据去连
// 任意 host:port——内网地址也在内。前端会有一份同样代号的清单（每家怎么
// 开 IMAP、去哪儿拿授权码），但那份只管**显示**；主机名以这张表为准，代
// 号对不上就直接拒，不会静默用一个猜出来的值。
//
// 表里的值抄自各家自己的帮助页。个人邮箱和企业邮是**两套不同的主机**
// （163.com 和企业邮 qiye.163.com 不通用），所以分开列。
type MailProvider struct {
	// Code 是前后端共用的标识。前端的清单用同一串代号，对不上时服务端
	// 报错而不是猜——两边各改一半的那种漂移，宁可当场炸。
	Code string
	// Domains 是「填了这个后缀就认得出是这家」的域名。企业邮用自己的域名
	// （me@sunrise.com 可能托管在腾讯企业邮上），认不出来，所以那几家的
	// Domains 是空的——只能由员工在表单里挑。
	Domains []string
	// Domain 是这次绑定实际用的发信域名，从要绑的地址里取。表里的条目不
	// 填它——同一家服务商托管着无数个域名。
	Domain string

	SMTPHost     string
	SMTPPort     int32
	SMTPSecurity string
	IMAPHost     string
	IMAPPort     int32
	IMAPSecurity string

	// NeedsOAuth 的服务商不能用密码绑：它们已经关掉了 IMAP 的基本认证，
	// 拿密码去登只会得到一句和「密码错了」长得一模一样的拒绝。与其让员工
	// 反复试，不如在绑之前就说清楚该走哪扇门。
	NeedsOAuth bool
}

// mailProviders 是全部认得的服务商。顺序就是前端下拉框的顺序：企业邮在前
// （公司自己的箱是主要用途），个人邮箱在后。
var mailProviders = []MailProvider{
	{
		Code: "p263",
		// 263 企业邮的域名是各家公司自己的，认不出来。
		SMTPHost: "smtp.263.net", SMTPPort: 465, SMTPSecurity: "SSL",
		IMAPHost: "imap.263.net", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:     "tencent",
		SMTPHost: "smtp.exmail.qq.com", SMTPPort: 465, SMTPSecurity: "SSL",
		IMAPHost: "imap.exmail.qq.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:     "ali",
		SMTPHost: "smtp.qiye.aliyun.com", SMTPPort: 465, SMTPSecurity: "SSL",
		IMAPHost: "imap.qiye.aliyun.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code: "netease",
		// 网易企业邮的 SMTP over SSL 是 994，不是常见的 465——照它自己
		// 的帮助页写。
		SMTPHost: "smtp.qiye.163.com", SMTPPort: 994, SMTPSecurity: "SSL",
		IMAPHost: "imap.qiye.163.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:    "gmail",
		Domains: []string{"gmail.com", "googlemail.com"},
		// Google 早就关了「用账号密码登 IMAP」。这里留着主机配置是因为
		// 应用专用密码（App Password）仍然走这条路；而开了两步验证才能
		// 生成应用专用密码，所以前端要把这句提示写出来。
		SMTPHost: "smtp.gmail.com", SMTPPort: 587, SMTPSecurity: "STARTTLS",
		IMAPHost: "imap.gmail.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:     "qq",
		Domains:  []string{"qq.com", "vip.qq.com", "foxmail.com"},
		SMTPHost: "smtp.qq.com", SMTPPort: 465, SMTPSecurity: "SSL",
		IMAPHost: "imap.qq.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:     "netease163",
		Domains:  []string{"163.com"},
		SMTPHost: "smtp.163.com", SMTPPort: 465, SMTPSecurity: "SSL",
		IMAPHost: "imap.163.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:     "netease126",
		Domains:  []string{"126.com"},
		SMTPHost: "smtp.126.com", SMTPPort: 465, SMTPSecurity: "SSL",
		IMAPHost: "imap.126.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:     "sina",
		Domains:  []string{"sina.com", "sina.cn"},
		SMTPHost: "smtp.sina.com", SMTPPort: 465, SMTPSecurity: "SSL",
		IMAPHost: "imap.sina.com", IMAPPort: 993, IMAPSecurity: "SSL",
	},
	{
		Code:    "outlook",
		Domains: []string{"outlook.com", "hotmail.com", "live.com", "msn.com"},
		// 微软 2024 年起关掉了个人账号的 IMAP 基本认证，密码和应用专用
		// 密码**都不行**，只剩 OAuth。主机配置照留，等哪天接上微软的
		// OAuth 就能用；在那之前 NeedsOAuth 让绑定入口把话说在前面，
		// 而不是让员工对着一句「授权码错误」猜半天。
		SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587, SMTPSecurity: "STARTTLS",
		IMAPHost: "outlook.office365.com", IMAPPort: 993, IMAPSecurity: "SSL",
		NeedsOAuth: true,
	},
}

// ProviderCodeOther 是「都不是上面这些」那一档：主机由员工自己填。
//
// 老板要的「包括其他的」就是它。这一档比其他档危险，因为主机名回到了调用
// 方手里——所以它单独走 validateCustomHost，只放行公网主机名和标准邮件端口。
const ProviderCodeOther = "other"

// MailProviderByCode 按代号取预设。第二个返回值是「认不认得这个代号」。
func MailProviderByCode(code string) (MailProvider, bool) {
	code = strings.TrimSpace(strings.ToLower(code))
	for _, p := range mailProviders {
		if p.Code == code {
			return p, true
		}
	}
	return MailProvider{}, false
}

// MailProviderForAddress 按地址后缀猜服务商，猜不出来返回 false。
//
// 只对个人邮箱有用。企业邮用各家自己的域名，从 me@sunrise.com 看不出它托
// 管在腾讯还是 263，那种情况必须由员工在表单里挑——猜错的代价是拿着凭据
// 去登了另一家的服务器。
func MailProviderForAddress(email string) (MailProvider, bool) {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return MailProvider{}, false
	}
	domain := strings.TrimSpace(strings.ToLower(email[at+1:]))
	if domain == "" {
		return MailProvider{}, false
	}
	for _, p := range mailProviders {
		for _, d := range p.Domains {
			if d == domain {
				return p, true
			}
		}
	}
	return MailProvider{}, false
}

// MailProviderCodes 是全部代号，给测试和前端对表用。
func MailProviderCodes() []string {
	out := make([]string, 0, len(mailProviders))
	for _, p := range mailProviders {
		out = append(out, p.Code)
	}
	return out
}

// mailPorts 是允许连的端口。收窄到这几个不是洁癖：这一档的主机名来自员工，
// 不限端口的话，「绑个邮箱」就成了让服务器去敲内网任意端口的一个按钮。
//
//	993 IMAPS · 143 IMAP+STARTTLS · 465 SMTPS · 587 SMTP+STARTTLS
//	994 网易企业邮的 SMTPS
var mailPorts = map[int32]bool{143: true, 465: true, 587: true, 993: true, 994: true}

// validateCustomHost 检查员工手填的主机。
//
// 挡两件事：
//
//   - **内网。** 直接写 IP 的一律拒（127.0.0.1、169.254.169.254 这类云元数据
//     地址、10./172.16./192.168. 的内网段都在内）。只收域名，而域名会在连接
//     时才解析——解析结果指回内网仍然可能，那一层要靠出网策略挡，这里挡的是
//     最直接的那条路。
//   - **非邮件端口。** 见 mailPorts。
func validateCustomHost(host string, port int32) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return apierr.Invalid("NT_MAIL_HOST_REQUIRED", "请填写服务器地址")
	}
	if len(host) > 255 {
		return apierr.Invalid("NT_MAIL_HOST_INVALID", "服务器地址过长")
	}
	// 直接写 IP 的一律拒。云元数据地址和内网段都从这条路进来，而员工填
	// 邮件服务器本来也没有写 IP 的理由。
	if ip := net.ParseIP(host); ip != nil {
		return apierr.Invalid("NT_MAIL_HOST_INVALID",
			"服务器地址请填域名，例如 imap.example.com")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return apierr.Invalid("NT_MAIL_HOST_INVALID", "不能填本机或内网地址")
	}
	// 至少要有一个点：单标签主机名（"mail"）只在内网解析得出来。
	if !strings.Contains(lower, ".") {
		return apierr.Invalid("NT_MAIL_HOST_INVALID",
			"服务器地址请填完整域名，例如 imap.example.com")
	}
	for _, r := range lower {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '.' && r != '-' {
			return apierr.Invalid("NT_MAIL_HOST_INVALID", "服务器地址含有非法字符")
		}
	}
	if !mailPorts[port] {
		return apierr.Invalid("NT_MAIL_PORT_INVALID",
			fmt.Sprintf("端口 %d 不是常见的邮件端口，请核对服务商的说明", port))
	}
	return nil
}
