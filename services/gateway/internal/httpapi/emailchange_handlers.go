package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
)

// 改登录邮箱：四条路由。
//
// 和激活那三条（invite_handlers.go）同样的分工，同样的理由——编排放在这里，
// 因为它横跨 iam 和 mail 而两者不能互相调用：mail 已经依赖 iam，反向的边会成环。
// 网关是唯一合法地同时认识两者的地方。
//
// 这一组和激活的区别只有一个，但很重要：**改邮箱是对一个已经能登录的账号动手。**
// 所以多了两件激活不需要的事——给旧地址也发一封「有人在改你的登录邮箱」，
// 以及生效之后把这个人的会话全部踢掉。

// requestEmailChange 记下变更、发确认信到新地址、再知会旧地址。
//
// 三步不是一个事务，也不可能是——信离开了我们的进程。顺序是挑过的：
// 先铸钥匙（信里要用），再发确认信，最后知会旧地址。中途失败的后果依次是
// 「一把没人收到的钥匙，打不开任何门」和「变更已发起但对方还没被知会」，
// 都比反过来轻。
func (s *Server) requestEmailChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NewEmail string `json:"newEmail"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	// 和邀请一样先问「这个管理员发得出信吗」。发不出去却回一句「已发送」，
	// 是那种要过几个小时才被发现的谎——而这次代价更大：管理员会以为
	// 员工马上要换地址登录了。
	if !s.senderIsReady(w, r) {
		return
	}
	id := idFromPath(r)
	c, err := s.Directory.RequestEmailChange(r.Context(), &iamv1.RequestEmailChangeRequest{
		EmployeeId: id, NewEmail: body.NewEmail,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}

	subject, text := emailChangeMail(c.GetName(), c.GetOldEmail(), c.GetNewEmail(),
		s.emailChangeLink(c.GetToken()))
	out, sendErr := s.Emails.CreateCampaign(r.Context(), &mailv1.CreateCampaignRequest{
		Subject: subject, Body: text,
		// 纯文本，和邀请信同样的理由：worker 会往每封 HTML 信里注入一个
		// 打开追踪像素，而这封信不是用来盯着员工看的。
		BodyFormat: "TEXT",
		Kind:       "TRANSACTIONAL",
		Recipients: []*mailv1.Recipient{{Name: c.GetName(), Email: c.GetNewEmail()}},
	})
	if sendErr != nil {
		s.writeError(w, http.StatusBadGateway, "MAIL_EMAIL_CHANGE_SEND_FAILED",
			"变更已记录，但确认邮件发送失败："+sendErr.Error()+
				"（员工的登录邮箱没有改变，可以稍后重新发送）")
		return
	}
	if out.GetQueued() == 0 {
		reason := "新地址未能进入发送队列"
		if sup := out.GetSuppressed(); len(sup) > 0 {
			reason = fmt.Sprintf("新地址在拒收名单中（%s），邮件未发送", sup[0].GetReason())
		}
		s.writeError(w, http.StatusConflict, "MAIL_EMAIL_CHANGE_SUPPRESSED",
			reason+"（员工的登录邮箱没有改变）")
		return
	}

	// 知会旧地址。**这是这套流程里唯一防「悄悄搬走别人账号」的东西**：
	// 确认信只到得了新地址，所以真正被换掉登录方式的那个人，除非收到这一封，
	// 否则一无所知。
	//
	// 尽力而为，不因为它失败就回错——变更信已经发出去了，这里报错会让管理员
	// 以为整件事没成、然后再点一次。失败写进日志。
	noticed := s.noticeOldAddress(r, c)
	writeUnlockJSON(w, map[string]any{
		"newEmail":  c.GetNewEmail(),
		"oldEmail":  c.GetOldEmail(),
		"name":      c.GetName(),
		"expiresAt": c.GetExpiresAt(),
		// 页面据此决定要不要提醒管理员「旧地址没收到通知，请当面知会一声」。
		"oldAddressNotified": noticed,
	})
}

// noticeOldAddress 给旧地址发一封「有人在改你的登录邮箱」。
func (s *Server) noticeOldAddress(r *http.Request, c *iamv1.RequestEmailChangeResponse) bool {
	subject, text := emailChangeNotice(c.GetName(), c.GetOldEmail(), c.GetNewEmail())
	out, err := s.Emails.CreateCampaign(r.Context(), &mailv1.CreateCampaignRequest{
		Subject: subject, Body: text, BodyFormat: "TEXT", Kind: "TRANSACTIONAL",
		Recipients: []*mailv1.Recipient{{Name: c.GetName(), Email: c.GetOldEmail()}},
	})
	if err != nil || out.GetQueued() == 0 {
		s.Log.Warn("email change requested but the old address was not notified",
			"employee", c.GetName(), "old", c.GetOldEmail(), "err", err)
		return false
	}
	return true
}

// cancelEmailChange 作废一把还没被点开的钥匙。
func (s *Server) cancelEmailChange(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Directory.CancelEmailChange(r.Context(), &iamv1.CancelEmailChangeRequest{
		EmployeeId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// peekEmailChange 让确认页在问「确认吗」之前就知道链接还有没有用。
//
// 无需登录，是必然而不是疏忽：收到信的人此刻多半没登录，而且他要确认的
// 恰恰是登录方式本身。
func (s *Server) peekEmailChange(w http.ResponseWriter, r *http.Request) {
	resp, err := s.IAM.PeekEmailChange(r.Context(), &iamv1.PeekEmailChangeRequest{
		Token: r.URL.Query().Get("token"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// confirmEmailChange 兑换链接，然后把这个人的会话全部踢掉。
func (s *Server) confirmEmailChange(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.ConfirmEmailChangeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.IAM.ConfirmEmailChange(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// 登录身份变了，旧 token 还能用，等于换了锁没换钥匙。
	//
	// 放在落库**之后**，和停用员工那里同样的顺序（iam_handlers.go）：反过来的话，
	// 一旦落库失败，就白白把一个什么都没变的人踢下了线。
	//
	// 尽力而为——邮箱已经改成功了，把这一步的失败报成整件事的失败，会让人以为
	// 要重来一次，而重来一次这个链接已经用过了。真踢不掉的补救是员工列表上的
	// 「结束登录」按钮，它就在那儿。
	if s.Revocations != nil {
		if rErr := s.Revocations.Revoke(r.Context(), resp.GetTenantId(), resp.GetEmployeeId()); rErr != nil {
			s.Log.Error("login email changed but the old sessions were not revoked",
				"employee", resp.GetEmployeeId(), "err", rErr)
		}
	}
	// 不发新会话。点开这封信证明的是「这个信箱是我的」，不是「我是这个账号的
	// 主人」——后者靠密码。把链接变成一条进门的路，正是它绝不能变成的东西。
	s.writeProto(w, resp)
}

// emailChangeLink 是员工要点的那个地址。
//
// url.QueryEscape 而不是拼接：token 是 base64url，本来就是 URL 安全的，
// 但 token 的格式是会变的东西，将来某天冒出来的转义 bug 看起来会像
// 「有些链接就是打不开」。
func (s *Server) emailChangeLink(token string) string {
	base := strings.TrimSuffix(s.FrontendBaseURL, "/")
	return base + "/email-change?token=" + url.QueryEscape(token)
}

// emailChangeMail 是新地址收到的那封信。
//
// 整封写出来而不是拼片段，因为要写对的是「一个没预料到这封信的人读起来是什么感觉」：
// 谁在写、为什么、要做什么、不做会怎样。
func emailChangeMail(name, oldEmail, newEmail, link string) (string, string) {
	subject := "确认你的新登录邮箱"
	body := fmt.Sprintf(`%s 你好，

公司的 ERP 系统里，你的登录邮箱要从 %s 改成这个地址（%s）。请点击下面的链接确认：

%s

链接 3 天内有效，只能使用一次。

**点开之前什么都不会变**——你现在仍然用 %s 登录，密码不变。
点开之后，登录邮箱就换成 %s，密码还是原来那个，但你会被退出登录，需要用新邮箱重新登录一次。

如果你并不知道这封邮件是怎么回事，请不要点击链接，并联系公司管理员。`,
		name, oldEmail, newEmail, link, oldEmail, newEmail)
	return subject, body
}

// emailChangeNotice 是旧地址收到的那封信。
//
// 这一封里**没有链接**，是故意的。它的作用是让本人知道有这件事，不是让他去点。
// 放一个链接进来等于把「知会」变成第二把钥匙，而这封信寄往的正是那个可能
// 已经不归他管的信箱。
func emailChangeNotice(name, oldEmail, newEmail string) (string, string) {
	subject := "有人正在修改你的 ERP 登录邮箱"
	body := fmt.Sprintf(`%s 你好，

公司的 ERP 系统里，有人发起了把你的登录邮箱从这个地址（%s）改成 %s 的申请。

确认链接已经发到 %s。在那边点开之前，**你这边一切照旧**——继续用 %s 登录，密码不变。

如果这就是你自己安排的，忽略这封信即可。
如果你并不知情，请立刻联系公司管理员，让他取消这次变更。`,
		name, oldEmail, newEmail, newEmail, oldEmail)
	return subject, body
}
