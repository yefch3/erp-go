package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

// Password reset over a mailed link: the way back in for somebody locked out,
// built so no administrator ever holds a password that is not their own.
//
// Three routes are public by necessity — the person asking cannot log in,
// that is the whole situation — and each answers with care about what it
// reveals. 忘记密码 answers identically whether or not the address has an
// account: a login page that answers differently is a staff directory, the
// same principle login itself follows.

// Twenty requests a minute per source is a person retyping their address a
// few times; six hundred is a harvester. The budget shapes nothing legitimate
// — its job is to make enumeration and mail-bombing slower than useless.
const publicResetBudget = 20

func (s *Server) resetLink(token string) string {
	base := strings.TrimSuffix(s.FrontendBaseURL, "/")
	return base + "/reset?token=" + url.QueryEscape(token)
}

// resetMail is the text somebody actually receives, written whole for the
// same reason as activationMail: what is read is the message, not the parts.
func resetMail(name, link string) (string, string) {
	subject := "重置你的 ERP 登录密码"
	body := fmt.Sprintf(`%s 你好，

有人（通常是你自己）在 ERP 登录页请求了重置密码。点击下面的链接设置新密码：

%s

链接 1 小时内有效，只能使用一次。设置新密码后，这个账号在所有设备上的登录会被退出一次。

如果这不是你发起的，直接忽略这封邮件即可——没有这个链接，任何人都改不了你的密码。若这种邮件反复出现，请告知管理员。`, name, link)
	return subject, body
}

// sendResetMail delivers the link through the given sender's bound mailbox.
// The operator context is forged on purpose and the precedent is the OAuth
// callback: a login-free route that must speak to the mail service does so
// as a named employee, never as nobody.
func (s *Server) sendResetMail(r *http.Request, tenantID, senderID int64, name, email, token string) error {
	ctx := grpcx.WithOperator(r.Context(), grpcx.Operator{
		TenantID: tenantID, EmployeeID: senderID,
		IP: r.RemoteAddr, TraceID: newTraceID(),
	})
	subject, body := resetMail(name, s.resetLink(token))
	out, err := s.Emails.CreateCampaign(ctx, &mailv1.CreateCampaignRequest{
		Subject: subject,
		Body:    body,
		// Plain text, TRANSACTIONAL, no pixel — same reasoning as the
		// invitation mail beside this file.
		BodyFormat: "TEXT",
		Kind:       "TRANSACTIONAL",
		Recipients: []*mailv1.Recipient{{Name: name, Email: email}},
	})
	if err != nil {
		return err
	}
	if out.GetQueued() == 0 {
		return fmt.Errorf("reset mail suppressed for %s", email)
	}
	return nil
}

// forgotPassword is the login page's 忘记密码. The answer is the same
// sentence no matter what happened, including internal failure: the only
// thing an unauthenticated caller may learn here is "if that address has an
// account, a mail is on its way".
func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON")
		return
	}
	generic := func() {
		writeUnlockJSON(w, map[string]any{"sent": true})
	}
	resp, err := s.IAM.RequestPasswordReset(r.Context(), &iamv1.RequestPasswordResetRequest{
		Email: body.Email,
	})
	if err != nil {
		// Logged for us, invisible to the caller — a different answer on
		// error would be a probe result too.
		s.Log.Error("forgot-password: could not mint reset", "err", err)
		generic()
		return
	}
	if !resp.GetFound() {
		generic()
		return
	}
	if err := s.sendResetMail(r, resp.GetTenantId(), resp.GetSenderEmployeeId(),
		resp.GetName(), resp.GetEmail(), resp.GetToken()); err != nil {
		s.Log.Error("forgot-password: could not send reset mail",
			"employee", resp.GetEmployeeId(), "sender", resp.GetSenderEmployeeId(), "err", err)
	}
	generic()
}

// peekPasswordReset lets the reset page say a dead link is dead before it
// asks anybody to choose a password. Same contract as peekInvitation.
func (s *Server) peekPasswordReset(w http.ResponseWriter, r *http.Request) {
	resp, err := s.IAM.PeekPasswordReset(r.Context(), &iamv1.PeekPasswordResetRequest{
		Token: r.URL.Query().Get("token"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	writeUnlockJSON(w, map[string]any{"name": resp.GetName(), "email": resp.GetEmail()})
}

// redeemPasswordReset spends the link, and then ends every session the
// account holds. "I lost my password" and "somebody else may have it" are
// the same event until proven otherwise, and the freshly chosen password is
// the only credential that should survive the day.
func (s *Server) redeemPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON")
		return
	}
	resp, err := s.IAM.RedeemPasswordReset(r.Context(), &iamv1.RedeemPasswordResetRequest{
		Token: body.Token, Password: body.Password,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if s.Revocations != nil {
		if err := s.Revocations.Revoke(r.Context(), resp.GetTenantId(), resp.GetEmployeeId()); err != nil {
			// The password already changed, which is the part that matters;
			// old sessions die at latest when their tokens expire.
			s.Log.Error("reset: could not revoke sessions",
				"employee", resp.GetEmployeeId(), "err", err)
		}
	}
	writeUnlockJSON(w, map[string]any{"email": resp.GetEmail()})
}

// sendResetLinkToEmployee is the administrator pressing the button on
// somebody's behalf: same link, same mail, sent from the administrator's own
// mailbox like every invitation, and recorded in the change history under
// the administrator's name (iam writes that row).
func (s *Server) sendResetLinkToEmployee(w http.ResponseWriter, r *http.Request) {
	if !s.senderIsReady(w, r) {
		return
	}
	resp, err := s.Directory.CreatePasswordReset(r.Context(), &iamv1.CreatePasswordResetRequest{
		EmployeeId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	subject, mailBody := resetMail(resp.GetName(), s.resetLink(resp.GetToken()))
	out, err := s.Emails.CreateCampaign(r.Context(), &mailv1.CreateCampaignRequest{
		Subject: subject, Body: mailBody,
		BodyFormat: "TEXT", Kind: "TRANSACTIONAL",
		Recipients: []*mailv1.Recipient{{Name: resp.GetName(), Email: resp.GetEmail()}},
	})
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "MAIL_RESET_SEND_FAILED",
			"重置链接已生成，但邮件发送失败："+grpcMessage(err))
		return
	}
	if out.GetQueued() == 0 {
		s.writeError(w, http.StatusConflict, "MAIL_RESET_SUPPRESSED",
			"该地址未能进入发送队列，邮件未发送")
		return
	}
	writeUnlockJSON(w, map[string]any{
		"email":     resp.GetEmail(),
		"expiresAt": resp.GetExpiresAt(),
	})
}
