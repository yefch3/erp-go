package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/grpc/status"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
)

// Activation: the three routes that turn an employee record into an account.
//
// The orchestration lives here rather than in either service because it spans
// both and neither may call the other. mail already depends on iam — it asks
// who an employee is and what they may see — so an iam that called mail would
// close a cycle. The gateway holds a client for each and is the only place
// that legitimately knows about both.

// inviteEmployee mints the link and mails it.
//
// The two halves are not one transaction and cannot be: the mail leaves our
// process. The order is chosen so that the failure mode is harmless — the
// token is minted first because the mail needs it, and a token nobody was
// sent opens nothing. Re-inviting replaces it.
func (s *Server) inviteEmployee(w http.ResponseWriter, r *http.Request) {
	if !s.senderIsReady(w, r) {
		return
	}
	inv, name, err := s.inviteOne(r, idFromPath(r))
	if err != nil {
		s.writeInviteFailure(w, err)
		return
	}
	writeUnlockJSON(w, map[string]any{
		"email": inv.GetEmail(),
		"name":  name,
		// When the link dies, so the page can say it rather than making
		// somebody count seven days forward.
		"expiresAt": inv.GetExpiresAt(),
	})
}

// inviteBatch is the same act, repeated, with one report at the end.
//
// Sequential rather than concurrent, and deliberately so. Every one of these
// is a real message leaving one person's real mailbox, and mail hosts rate
// limit per account — Gmail and 263 both do. Firing eighty sends at once is
// the fastest way to have the whole company's sending address throttled on
// the day it is first used.
func (s *Server) inviteBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EmployeeIDs []int64 `json:"employeeIds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "GATEWAY_BAD_JSON", "请求体不是合法的 JSON")
		return
	}
	if len(body.EmployeeIDs) == 0 {
		s.writeError(w, http.StatusBadRequest, "IAM_INVITE_NOBODY", "没有选中任何人")
		return
	}
	if !s.senderIsReady(w, r) {
		return
	}

	type outcome struct {
		EmployeeID int64  `json:"employeeId"`
		Email      string `json:"email"`
		Name       string `json:"name"`
		Sent       bool   `json:"sent"`
		Reason     string `json:"reason,omitempty"`
	}
	results := make([]outcome, 0, len(body.EmployeeIDs))
	sent := 0
	for _, id := range body.EmployeeIDs {
		inv, name, err := s.inviteOne(r, id)
		if err != nil {
			// Never abandons the rest. Half the reasons an invitation fails
			// are about that one person — no address, already activated, a
			// bounced mailbox — and stopping on the first would make a batch
			// of eighty as slow to work through as eighty single clicks.
			results = append(results, outcome{EmployeeID: id, Reason: inviteReason(err)})
			continue
		}
		results = append(results, outcome{
			EmployeeID: id, Email: inv.GetEmail(), Name: name, Sent: true,
		})
		sent++
	}
	writeUnlockJSON(w, map[string]any{
		"sent":    sent,
		"failed":  len(results) - sent,
		"results": results,
	})
}

// senderIsReady answers whether this administrator can send at all, before
// anything is written.
//
// Every mail this system emits leaves through the sender's own bound mailbox,
// so an admin who has not signed in to theirs would queue invitations that
// the worker silently fails to deliver hours later. "邀请已发送" when nothing
// will ever be sent is the expensive kind of lie — and in a batch it is
// eighty of them.
func (s *Server) senderIsReady(w http.ResponseWriter, r *http.Request) bool {
	// 不点名：问的是「他有没有绑箱」，默认箱答得了。
	acct, err := s.Emails.GetMyMailAccount(r.Context(), &mailv1.GetMyMailAccountRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return false
	}
	if !acct.GetAccount().GetHasSecret() {
		s.writeError(w, http.StatusPreconditionFailed, "MAIL_SENDER_NOT_BOUND",
			"你还没有绑定自己的邮箱，无法发送邀请邮件。请先到「邮箱」页面登录。")
		return false
	}
	return true
}

// inviteFailure carries a reason a single invitation did not go out, so the
// batch can report per-person outcomes while the single-employee route can
// still answer with a status code.
type inviteFailure struct {
	status int
	code   string
	reason string
	err    error
}

func (e inviteFailure) Error() string { return e.reason }
func (e inviteFailure) Unwrap() error { return e.err }

func inviteReason(err error) string {
	var f inviteFailure
	if errors.As(err, &f) {
		return f.reason
	}
	return err.Error()
}

func (s *Server) writeInviteFailure(w http.ResponseWriter, err error) {
	var f inviteFailure
	if errors.As(err, &f) {
		if f.status == 0 {
			s.writeGRPCError(w, f.err)
			return
		}
		s.writeError(w, f.status, f.code, f.reason)
		return
	}
	s.writeGRPCError(w, err)
}

// inviteOne is the whole act for one person: mint, then mail.
func (s *Server) inviteOne(r *http.Request, employeeID int64) (*iamv1.InviteEmployeeResponse, string, error) {
	inv, err := s.Directory.InviteEmployee(r.Context(), &iamv1.InviteEmployeeRequest{
		EmployeeId: employeeID,
	})
	if err != nil {
		return nil, "", inviteFailure{reason: statusMessage(err), err: err}
	}
	subject, body := activationMail(inv.GetName(), s.activationLink(inv.GetToken()))
	out, err := s.Emails.CreateCampaign(r.Context(), &mailv1.CreateCampaignRequest{
		Subject: subject,
		Body:    body,
		// Plain text on purpose. The worker injects an open-tracking pixel
		// into every HTML mail, and an invitation is not something to watch
		// an employee open. A bare link also survives every client identically
		// and gives a spam filter nothing to dislike.
		BodyFormat: "TEXT",
		// TRANSACTIONAL: a send whose outcome cannot be determined must not be
		// treated like a newsletter nobody will miss.
		Kind:       "TRANSACTIONAL",
		Recipients: []*mailv1.Recipient{{Name: inv.GetName(), Email: inv.GetEmail()}},
	})
	if err != nil {
		// The token exists and the mail does not. Said plainly rather than
		// dressed up, because the fix is to try again once the mailbox works
		// — and a retry is safe, it replaces this link rather than adding one.
		return nil, "", inviteFailure{
			status: http.StatusBadGateway, code: "MAIL_INVITE_SEND_FAILED",
			reason: "激活链接已生成，但邮件发送失败：" + err.Error(), err: err,
		}
	}
	if out.GetQueued() == 0 {
		// Nothing entered the queue. The only way that happens for a single
		// valid address is the suppression list — the address bounced before,
		// or somebody unsubscribed it. Worth naming: it is the difference
		// between "wait for the mail" and "this mailbox does not work".
		reason := "该地址未能进入发送队列"
		if sup := out.GetSuppressed(); len(sup) > 0 {
			reason = fmt.Sprintf("该地址在拒收名单中（%s），邮件未发送", sup[0].GetReason())
		}
		return nil, "", inviteFailure{
			status: http.StatusConflict, code: "MAIL_INVITE_SUPPRESSED", reason: reason,
		}
	}
	return inv, inv.GetName(), nil
}

// statusMessage pulls the service's own words out of a gRPC error. What iam
// says about why somebody cannot be invited — no address, wrong domain,
// already active — is more useful than anything this layer could paraphrase.
func statusMessage(err error) string {
	return status.Convert(err).Message()
}

// importEmployees previews or applies a pasted block.
//
// One route for both, distinguished by dryRun in the body, because they are
// one operation seen twice: iam runs the same validation either way and only
// enters a transaction when asked to. Two routes would be two chances for the
// preview and the commit to disagree.
func (s *Server) importEmployees(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.ImportEmployeesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Directory.ImportEmployees(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// activationLink builds the address the employee will click.
//
// url.QueryEscape rather than concatenation: the token is base64url, which is
// URL-safe by construction, but a token format is a thing that changes and an
// escaping bug found later would look like "some invitations just do not work".
func (s *Server) activationLink(token string) string {
	base := strings.TrimSuffix(s.FrontendBaseURL, "/")
	return base + "/activate?token=" + url.QueryEscape(token)
}

// activationMail is the text somebody actually receives.
//
// Written as a whole message rather than assembled from fragments, because the
// thing being got right is how it reads to a person who was not expecting it:
// who is writing, why, what to do, and what happens if they ignore it.
func activationMail(name, link string) (string, string) {
	subject := "启用你的 ERP 账号"
	body := fmt.Sprintf(`%s 你好，

公司的 ERP 系统已经为你开通了账号。请点击下面的链接设置密码，之后就可以用这个邮箱地址登录：

%s

链接 7 天内有效，只能使用一次。

设置好密码后，你会用**这个邮箱地址**作为登录账号——系统里所有对外发出的邮件也都会从这个邮箱发出。

如果你并不知道这封邮件是怎么回事，请忽略它，或者直接回复问一下。链接没有被点开之前，账号是无法登录的。`, name, link)
	return subject, body
}

// peekInvitation lets the activation page know whether the link still works,
// and who it is for, before it asks anybody to choose a password.
//
// Unauthenticated by necessity: the person opening it has no account yet —
// having one is what they are here to arrange.
func (s *Server) peekInvitation(w http.ResponseWriter, r *http.Request) {
	resp, err := s.IAM.PeekInvitation(r.Context(), &iamv1.PeekInvitationRequest{
		Token: r.URL.Query().Get("token"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// activateAccount redeems the link.
//
// Not throttled, and that is a decision rather than an omission. The other two
// open routes are metered because what they guard is guessable — an address, a
// mailbox code. This one guards 256 bits of our own randomness, so there is
// nothing to guess at, and there is no identity to meter by until the token
// resolves. What the route does need is to not be an expensive thing to call
// for free, and iam handles that: it validates the token before it hashes a
// password.
func (s *Server) activateAccount(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.ActivateAccountRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.IAM.ActivateAccount(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// No session is issued here. Activation proves the mailbox; logging in
	// proves the password, and having just set it the person can. Minting a
	// token from the link instead would make the link itself a way in, which
	// is exactly what it must not be once it has been read out of a mailbox.
	s.writeProto(w, resp)
}
