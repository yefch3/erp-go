package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// 主邮箱（mail 迁移 00067）：公司的邮箱，员工只是暂时拿着用。
//
// 三条路：
//
//   - 管理员替员工分配：POST /api/employees/{id}/company-mailbox。
//     **这是有意开的第二条进邮箱凭据存储的路。** 员工自己那条（verifyMailbox）
//     的规矩「绑的永远是自己、请求体里没有绑给谁」原样不动，还有测试守着。
//     这一条只放行「管理员工账号」的权限——能开账号、重置密码的人，就能配
//     主邮箱，不另造一种权限。每次操作留痕，写下谁替谁配了哪个。
//   - 管理员看某人配的是哪个：GET，同一个地址。
//   - 员工登录后自动开锁：POST /api/mailbox/unlock-company。不问密码——员工
//     根本不知道主邮箱的密码。只对主邮箱发令牌；个人邮箱照旧过门。

// assignCompanyMailbox 替员工绑主邮箱。请求体和 verifyMailbox 一样，多的那个
// 「分给谁」在路径里。
func (s *Server) assignCompanyMailbox(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret       string `json:"secret"`
		Email        string `json:"email"`
		Provider     string `json:"provider"`
		SMTPHost     string `json:"smtpHost"`
		SMTPPort     int32  `json:"smtpPort"`
		SMTPSecurity string `json:"smtpSecurity"`
		IMAPHost     string `json:"imapHost"`
		IMAPPort     int32  `json:"imapPort"`
		IMAPSecurity string `json:"imapSecurity"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON")
		return
	}
	employeeID := idFromPath(r)
	// 先问一句这个人在不在本公司。邮件服务没有员工表，它只认一个数字；不问的
	// 话，一个打错的 id 会把一个真邮箱的密码存到一个不存在的人名下。
	if _, err := s.Directory.GetEmployee(r.Context(), &iamv1.GetEmployeeRequest{Id: employeeID}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	op, _ := grpcx.OperatorFromContext(r.Context())
	email := strings.ToLower(strings.TrimSpace(body.Email))
	// 限流按「操作人」和「操作人 + 目标地址」各计一次，和 verifyMailbox 同一套：
	// 管理员输错密码也是在拿真邮箱试密码。
	perActor := fmt.Sprintf("t%d.e%d.assign", op.TenantID, op.EmployeeID)
	perTarget := fmt.Sprintf("t%d.e%d.assign.%s", op.TenantID, op.EmployeeID, email)
	for _, who := range []string{perActor, perTarget} {
		if wait, blocked := s.Throttle.Blocked(r.Context(), throttleMailVerify, who); blocked {
			s.writeTooManyAttempts(w, wait)
			return
		}
	}
	resp, err := s.Emails.AssignCompanyMailbox(r.Context(), &mailv1.AssignCompanyMailboxRequest{
		EmployeeId: employeeID,
		Secret:     body.Secret, Email: email, Provider: body.Provider,
		SmtpHost: body.SMTPHost, SmtpPort: body.SMTPPort, SmtpSecurity: body.SMTPSecurity,
		ImapHost: body.IMAPHost, ImapPort: body.IMAPPort, ImapSecurity: body.IMAPSecurity,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !resp.GetOk() {
		if resp.GetHostRejected() {
			var longest time.Duration
			for _, who := range []string{perActor, perTarget} {
				if wait, spent := s.Throttle.Failed(r.Context(), throttleMailVerify, who); spent && wait > longest {
					longest = wait
				}
			}
			if longest > 0 {
				s.writeTooManyAttempts(w, longest)
				return
			}
		}
		s.writeError(w, http.StatusForbidden, "MAIL_VERIFY_FAILED", resp.GetDetail())
		return
	}
	for _, who := range []string{perActor, perTarget} {
		s.Throttle.Passed(r.Context(), throttleMailVerify, who)
	}
	// 回的形状和 verifyMailbox 一样（accountId / email / detail），前端那份
	// 表单两处共用；这里没有 token——分配的是别人的箱，开锁的令牌不该落在
	// 管理员的浏览器里。
	writeUnlockJSON(w, map[string]any{
		"accountId": resp.GetAccountId(),
		"email":     resp.GetEmail(),
		"detail":    resp.GetDetail(),
	})
}

// getCompanyMailbox：某人现在拿着的主邮箱。没有就是 {mailbox: null}。
func (s *Server) getCompanyMailbox(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetCompanyMailbox(r.Context(), &mailv1.GetCompanyMailboxRequest{
		EmployeeId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// unlockCompanyMailbox：登录的这个人有主邮箱的话，发一把它的开锁令牌。
//
// 不问密码。主邮箱的密码是管理员输的，员工不知道也不该知道；「他能登进 ERP」
// 就是他能用这个箱的全部凭证。只发**这一个箱**的令牌——个人邮箱照旧要过门，
// 一把管一箱的锁（UnlockStore）让这件事成为可能。
//
// 回的形状和 verifyMailbox 一样，前端 adoptVerification 原样收。
func (s *Server) unlockCompanyMailbox(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	// RetryLogin：这个箱记着登录失败的话，mail 那边先拿存着的密码试一次再回答。
	// 后台不再替被拒的箱去试，员工打开邮箱页的这一下是它能自己恢复的时机；不试
	// 的话下面那个 409 会把人挡在门外，恢复就只剩管理员重新填密码。
	resp, err := s.Emails.GetCompanyMailbox(r.Context(), &mailv1.GetCompanyMailboxRequest{RetryLogin: true})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	box := resp.GetMailbox()
	if box == nil {
		s.writeError(w, http.StatusNotFound, "MAIL_NO_COMPANY_MAILBOX", "这个账号没有配主邮箱")
		return
	}
	// 密码失效了（被改、被服务商锁）：开锁也收不到信，别让人以为好着。
	// 这时该管理员重新填密码，员工自己填不了。
	if box.GetNeedsReauth() || !box.GetIsActive() {
		s.writeError(w, http.StatusConflict, "MAIL_COMPANY_MAILBOX_REAUTH",
			"主邮箱当前登录不上，请联系管理员重新设置密码")
		return
	}
	if s.Unlock == nil {
		s.writeError(w, http.StatusInternalServerError, "MAIL_UNLOCK_STORE", "无法保存验证状态，请重试")
		return
	}
	tok, exp, err := s.Unlock.Grant(r.Context(), op.TenantID, op.EmployeeID, box.GetAccountId())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "MAIL_UNLOCK_STORE", "无法保存验证状态，请重试")
		return
	}
	type minted struct {
		AccountID int64  `json:"accountId"`
		Email     string `json:"email"`
		Token     string `json:"token"`
	}
	writeUnlockJSON(w, map[string]any{
		"token":     tok,
		"tokens":    []minted{{AccountID: box.GetAccountId(), Email: box.GetEmail(), Token: tok}},
		"expiresIn": exp,
		"accountId": box.GetAccountId(),
		"email":     box.GetEmail(),
	})
}
