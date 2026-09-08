package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	senderID, _ := strconv.ParseInt(r.URL.Query().Get("sender_id"), 10, 64)
	resp, err := s.Emails.ListCampaigns(r.Context(), &mailv1.ListCampaignsRequest{
		Page:     pageFromQuery(r),
		Keyword:  r.URL.Query().Get("keyword"),
		SenderId: senderID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getCampaign(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetCampaign(r.Context(), &mailv1.GetCampaignRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) previewCampaign(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.PreviewCampaignRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.PreviewCampaign(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.CreateCampaignRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// 没点名从哪个信箱发，就用**此刻解锁的那个箱**——你在哪个箱里，信就从
	// 那个箱出去。这是最符合直觉的默认，也让前端在绝大多数情况下不用操心。
	//
	// 令牌里那个箱比请求参数可信：它是验证过的（见 mailunlock.go），而参数
	// 是调用方说的。服务层还会再验一次「这个箱是不是他的」。
	if req.GetAccountId() == 0 {
		req.AccountId = unlockedAccount(r.Context())
	}
	resp, err := s.Emails.CreateCampaign(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listEmailMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	campaignID, _ := strconv.ParseInt(q.Get("campaign_id"), 10, 64)
	senderID, _ := strconv.ParseInt(q.Get("sender_id"), 10, 64)
	resp, err := s.Emails.ListMessages(r.Context(), &mailv1.ListMessagesRequest{
		Page:          pageFromQuery(r),
		CampaignId:    campaignID,
		SenderId:      senderID,
		Status:        q.Get("status"),
		AttentionOnly: q.Get("attention_only") == "true",
		Keyword:       q.Get("keyword"),
		Cursor:        q.Get("cursor"),
		// 「待处理」按信箱分：看哪个箱由令牌决定，不由调用方的参数决定。
		AccountId: unlockedAccount(r.Context()),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getEmailMessage(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetMessage(r.Context(), &mailv1.GetMessageRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) requeueEmailMessage(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.RequeueMessageRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Emails.RequeueMessage(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) abandonEmailMessage(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.AbandonMessageRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Emails.AbandonMessage(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSignatures(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListSignatures(r.Context(), &mailv1.ListSignaturesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSignature(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.CreateSignatureRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.CreateSignature(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateSignature(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.UpdateSignatureRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// The id comes from the path, not the body: two places to say which row
	// is one place too many, and the path is the one the route matched on.
	req.Id = idFromPath(r)
	resp, err := s.Emails.UpdateSignature(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteSignature(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.DeleteSignature(r.Context(), &mailv1.DeleteSignatureRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listEmailTemplates(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListEmailTemplates(r.Context(), &mailv1.ListEmailTemplatesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createEmailTemplate(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.CreateEmailTemplateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.CreateEmailTemplate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateEmailTemplate(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.UpdateEmailTemplateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// The id comes from the path, not the body — same rule as signatures.
	req.Id = idFromPath(r)
	resp, err := s.Emails.UpdateEmailTemplate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteEmailTemplate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.DeleteEmailTemplate(r.Context(), &mailv1.DeleteEmailTemplateRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSuppressions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListSuppressions(r.Context(), &mailv1.ListSuppressionsRequest{
		Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addSuppression(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.AddSuppressionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.AddSuppression(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// removeSuppression takes the address in the query string rather than the
// path: an email address contains characters that a path segment mangles.
func (s *Server) removeSuppression(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.RemoveSuppression(r.Context(), &mailv1.RemoveSuppressionRequest{
		Email: r.URL.Query().Get("email"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// listCustomerCountries and contactsInCountry are the address book grouped
// the way an exporter actually thinks about it.
//
// Both sit with the mail handlers rather than the customer ones for the same
// reason listMailingContacts does: they are shaped by what the composer needs,
// not by what a customer record is. And they carry the mail write permission,
// because that is what they are for — reading the customer list is a different
// question with a different answer.
func (s *Server) listCustomerCountries(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerCountries(r.Context(), &mdv1.ListCustomerCountriesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) contactsInCountry(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.Customers.ContactsInCountry(r.Context(), &mdv1.ContactsInCountryRequest{
		CountryCode: q.Get("code"),
		// Defaults to every contact at the customer, and the default is opt-out
		// rather than opt-in on purpose: in this trade the counterparty is a
		// company, not a person — the buyer, the shipping clerk and whoever
		// signs all expect to be on the thread. The expensive mistake is the
		// quiet one, a price update that never reached the person who decides.
		//
		// Written as "== primary" rather than "!= all" so that a caller who
		// sends nothing, or sends a value nobody anticipated, lands on the
		// documented default instead of on whichever branch the negation left.
		PrimaryOnly: q.Get("scope") == "primary",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// listMailingContacts is the address book behind the recipient picker. It
// lives with the mail handlers rather than the customer ones because it is
// shaped by what the composer needs, not by what a customer record is.
func (s *Server) listMailingContacts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var customerIDs []int64
	for _, raw := range q["customer_id"] {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			customerIDs = append(customerIDs, id)
		}
	}
	resp, err := s.Customers.ListMailingContacts(r.Context(), &mdv1.ListMailingContactsRequest{
		Keyword:     q.Get("keyword"),
		CustomerIds: customerIDs,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignMailAttachment(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.PresignAttachmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.PresignAttachment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) registerMailAttachment(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.RegisterAttachmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.RegisterAttachment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listMailAttachments(w http.ResponseWriter, r *http.Request) {
	campaignID, _ := strconv.ParseInt(r.URL.Query().Get("campaign_id"), 10, 64)
	resp, err := s.Emails.ListAttachments(r.Context(), &mailv1.ListAttachmentsRequest{
		CampaignId: campaignID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignMailImage(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.PresignImageRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.PresignImage(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) registerMailImage(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.RegisterImageRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.RegisterImage(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// importMailImage takes an address instead of a file. The mail service is
// what fetches it — the gateway would be the wrong machine to point at an
// arbitrary URL, and the guarded fetcher already lives there.
func (s *Server) importMailImage(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.ImportImageRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.ImportImage(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listMailImages(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListImages(r.Context(), &mailv1.ListImagesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) withdrawMailImage(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.WithdrawImage(r.Context(), &mailv1.WithdrawImageRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// serveMailImage is the one route in this system that is deliberately public.
//
// It exists because the fetcher is a recipient's mail client: it has no
// session, no token of ours beyond the URL, and it may come knocking weeks
// after the mail was sent. A presigned link would have expired by then and
// every mail already delivered would show a broken logo.
//
// The token is therefore the whole credential, which constrains everything
// else here: read-only, no listing, no enumeration, and a not-found for any
// token that is unknown or withdrawn.
func (s *Server) serveMailImage(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.FetchImage(r.Context(), &mailv1.FetchImageRequest{
		Token: chi.URLParam(r, "token"),
	})
	if err != nil {
		// One neutral answer for every failure — unknown token, withdrawn
		// image, storage error. Distinguishing them would let somebody probe
		// which tokens exist.
		http.NotFound(w, r)
		return
	}
	ct := resp.GetContentType()
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	// nosniff matters more than usual here: this is unauthenticated,
	// user-supplied content served from our own origin.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Mail clients and their proxies refetch aggressively; the bytes behind a
	// token never change, so let them cache hard.
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(resp.GetContent())
}

// serveMailFile 是超大附件的公开取件口。
//
// **重定向，不代理。** 走这条路的偏偏是大文件；把几百 MB 穿过网关，一个人
// 点两下就能把内存吃光。所以这里只把浏览器指到存储那条短期地址上去。
// serveMailImage 那条是读进内存再吐出来的，因为图片有 2 MB 的硬上限。
func (s *Server) serveMailFile(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.FetchAttachmentLink(r.Context(), &mailv1.FetchAttachmentLinkRequest{
		Token: chi.URLParam(r, "token"),
	})
	if err != nil || resp.GetUrl() == "" {
		// 一个中性的答案，对应服务层那个统一的错：把「没这个 token」「被
		// 撤回了」「文件没了」分开说，等于告诉试探的人哪些 token 存在过。
		http.NotFound(w, r)
		return
	}
	// 不缓存这一跳：它每次都要重新签，而签出来的地址是有期限的。被缓存住的
	// 302 会在过期之后把人送到一个 403 上去。
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Redirect(w, r, resp.GetUrl(), http.StatusFound)
}

func (s *Server) withdrawMailFileLink(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.WithdrawAttachmentLinkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.WithdrawAttachmentLink(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listMailSenders(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListSenders(r.Context(), &mailv1.ListSendersRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- drafts

func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.SaveDraftRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// 同 createCampaign：草稿也记住「我在哪个箱里写的」。
	if req.GetAccountId() == 0 {
		req.AccountId = unlockedAccount(r.Context())
	}
	resp, err := s.Emails.SaveDraft(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listDrafts(w http.ResponseWriter, r *http.Request) {
	// 草稿箱也按信箱分，和收件箱、已发送、搜索同一条理由：看哪个箱由令牌
	// 决定，不由调用方的参数决定。
	resp, err := s.Emails.ListDrafts(r.Context(), &mailv1.ListDraftsRequest{
		AccountId: unlockedAccount(r.Context()),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetDraft(r.Context(), &mailv1.GetDraftRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteDraft(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.DeleteDraft(r.Context(), &mailv1.DeleteDraftRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) sendDraft(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.SendDraft(r.Context(), &mailv1.SendDraftRequest{
		Id: idFromPath(r), ScheduledAt: r.URL.Query().Get("scheduled_at"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// The 已定时 folder: what is booked, and the two things anyone does about it.
func (s *Server) listScheduled(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListScheduled(r.Context(), &mailv1.ListScheduledRequest{
		Page:   pageFromQuery(r),
		Cursor: r.URL.Query().Get("cursor"),
		// 看哪个箱由令牌决定，和收件箱、已发送同一条理由。
		AccountId: unlockedAccount(r.Context()),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) sendScheduledNow(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.SendScheduledNow(r.Context(), &mailv1.SendScheduledNowRequest{
		CampaignId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelScheduled(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.CancelScheduled(r.Context(), &mailv1.CancelScheduledRequest{
		CampaignId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getMailHost(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetMailHost(r.Context(), &mailv1.GetMailHostRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) saveMailHost(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.SaveMailHostRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.SaveMailHost(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// 问哪个信箱的门牌。这条路由**没有** requireMailUnlock，也不能有——门本身
// 就是拿来换令牌的，还没令牌的时候也要能问。
//
// 所以这里只能收查询参数，而「这个箱是不是他的」由邮件服务判：不是他的就
// 回空壳，不回别人的。参数不可信，答案可信。
func (s *Server) getMyMailAccount(w http.ResponseWriter, r *http.Request) {
	acct, _ := strconv.ParseInt(r.URL.Query().Get("accountId"), 10, 64)
	resp, err := s.Emails.GetMyMailAccount(r.Context(), &mailv1.GetMyMailAccountRequest{
		AccountId: acct,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// listMyMailboxes 回这个人名下的全部信箱，默认的排在最前。
//
// 请求体和 RPC 里都没有「谁的」：那件事只来自登录令牌。
func (s *Server) listMyMailboxes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListMyMailboxes(r.Context(), &mailv1.ListMyMailboxesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// setDefaultMailbox 换写信时预选哪个信箱。
//
// 只带账号 id：改的永远是自己的，而「那个信箱是不是他的」由 SQL 的 WHERE
// 判定——不是他的就影响零行，服务层翻成 404 而不是默默成功。
func (s *Server) setDefaultMailbox(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.SetDefaultMailboxRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.SetDefaultMailbox(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// setKeepSentCopy 换这个信箱「发完信我们自己留不留副本」。
//
// 归属同 setDefaultMailbox，由 SQL 的 WHERE 判。不要 requireMailUnlock：
// 这是一条设置，不读任何邮件内容，而要求先解锁才能改，等于让「已发送里
// 有两封」的人先去输一遍授权码才能把它关掉。
func (s *Server) setKeepSentCopy(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.SetKeepSentCopyRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.SetKeepSentCopy(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// unbindMailbox 断开一个信箱：凭据清掉、不再收发，**历史邮件原样留着**。
//
// 归属同上，由 SQL 的 WHERE 判。这里也不加 requireMailUnlock——解绑是「我不
// 想再连这个箱了」，而要求先解锁它才能解绑，恰好把想断开的人挡在门外。
// 能证明自己是这个 ERP 账号的主人就够了：解绑毁不掉任何邮件。
func (s *Server) unbindMailbox(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.UnbindMailboxRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.UnbindMailbox(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// The request body carries no employee id, and neither does the RPC: whose
// mailbox this is comes from the token alone.
// serveOpenPixel answers a recipient's mail client.
//
// It always returns the same 1x1 image, whatever happens: a valid key, an
// unknown one, a service that is down. Anything else would turn this route
// into an oracle for which message keys exist.
//
// What the resulting number means is worth remembering wherever it is shown.
// Apple Mail pre-fetches remote images without the recipient opening anything,
// and Outlook blocks them even when the recipient does, so this over-reports
// and under-reports at the same time.
func (s *Server) serveOpenPixel(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key != "" {
		// Fire and forget: the image must go out at the same speed whether or
		// not the write succeeds, and a recipient must never wait on our
		// database to see their mail render.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.Emails.RecordOpen(ctx, &mailv1.RecordOpenRequest{
				MessageKey: key,
				UserAgent:  r.Header.Get("User-Agent"),
				Ip:         clientIP(r),
			})
		}()
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Must not be cached: a cached pixel would report the first open and then
	// go silent, which is the opposite of what it is for.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openPixel)
}

// A 1x1 transparent GIF. GIF rather than PNG because a few older clients
// still refuse to render a 1x1 PNG, and being fetched is the entire point.
var openPixel, _ = base64.StdEncoding.DecodeString(
	"R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.Index(v, ","); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) listInbound(w http.ResponseWriter, r *http.Request) {
	// 看哪个信箱，**由令牌决定，不由调用方的参数决定**。
	//
	// 令牌现在是一个箱一把（见 mailunlock.go）。用参数的话，退出 A 之后，
	// 拿还活着的 B 的令牌配一个 accountId=A 照样读得到 A 的信——退出就
	// 白退了。
	//
	// 0 表示不限：换版本之前发出去、还没到期的旧令牌，以及一个箱都没绑的
	// 人。那时回全部，和改动之前一样。
	acct := unlockedAccount(r.Context())
	resp, err := s.Emails.ListInbound(r.Context(), &mailv1.ListInboundRequest{
		Page:      pageFromQuery(r),
		Keyword:   r.URL.Query().Get("keyword"),
		View:      r.URL.Query().Get("view"),
		Cursor:    r.URL.Query().Get("cursor"),
		AccountId: acct,
		SortBy:    r.URL.Query().Get("sort_by"),
		SortDir:   r.URL.Query().Get("sort_dir"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getMailThread(w http.ResponseWriter, r *http.Request) {
	// id 是「从哪一封信点进来的」，决定读哪个信箱那一份。旧前端不发，
	// 那时 0 表示不限定——部署顺序是后端先发前端后发，这条路必须留着。
	//
	// 这里可以留参数：它指的是**具体某一封信**，而那封信本来就要属于
	// 调用者（服务层按 owner_id 卡）。和 accountId 不同——后者是"给我看
	// 这个箱的全部信"，那句话必须由令牌说了算。
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	resp, err := s.Emails.GetMailThread(r.Context(), &mailv1.GetMailThreadRequest{
		ThreadKey: r.URL.Query().Get("key"), MessageId: id,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markInbound(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.MarkInboundRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// The id comes from the URL, whatever the body claims.
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Emails.MarkInbound(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) purgeInbound(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Emails.PurgeInbound(r.Context(), &mailv1.PurgeInboundRequest{
		Id: id, WholeThread: r.URL.Query().Get("whole_thread") == "true",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markViewRead(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.MarkViewRead(r.Context(), &mailv1.MarkViewReadRequest{
		View: r.URL.Query().Get("view"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.EmptyTrash(r.Context(), &mailv1.EmptyTrashRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) emptyJunk(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.EmptyJunk(r.Context(), &mailv1.EmptyJunkRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getInbound(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Emails.GetInbound(r.Context(), &mailv1.GetInboundRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) convertInboundToExcel(w http.ResponseWriter, r *http.Request) {
	op, _ := grpcx.OperatorFromContext(r.Context())
	who := fmt.Sprintf("t%d.e%d", op.TenantID, op.EmployeeID)
	if wait, limited := s.Throttle.Limited(r.Context(), throttleMailExcel, who); limited {
		secs := int(wait.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		s.writeError(w, http.StatusTooManyRequests, "MAIL_EXCEL_RATE_LIMITED", "Excel 转换请求过于频繁，请稍后再试")
		return
	}
	req := &mailv1.StartInboundExcelConversionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// URL ownership wins over any body field, as with every other mail action.
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	// 邮件侧已经识别客户或公司模板时可传明确的模板 ID；尚未接入识别规则的
	// 旧调用继续使用默认模板。无论哪种方式，身份和列快照都随任务持久化。
	var template *prv1.GetInquiryTemplateResponse
	var err error
	if req.GetInquiryTemplateId() > 0 {
		template, err = s.InquiryTemplates.GetInquiryTemplate(r.Context(), &prv1.GetInquiryTemplateRequest{Id: req.GetInquiryTemplateId()})
	} else {
		fallback, fallbackErr := s.InquiryTemplates.GetDefaultInquiryTemplate(r.Context(), &prv1.GetDefaultInquiryTemplateRequest{})
		err = fallbackErr
		if fallback != nil {
			template = &prv1.GetInquiryTemplateResponse{Template: fallback.GetTemplate()}
		}
	}
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.InquiryTemplateId = template.GetTemplate().GetId()
	req.InquiryTemplateCode = template.GetTemplate().GetTemplateCode()
	req.InquiryTemplateVersion = template.GetTemplate().GetVersion()
	fields := append([]*prv1.InquiryTemplateField(nil), template.GetTemplate().GetFields()...)
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].GetSortOrder() < fields[j].GetSortOrder() })
	for _, field := range fields {
		req.TemplateColumns = append(req.TemplateColumns, &mailv1.InquiryColumnSpec{
			FieldKey: field.GetFieldKey(), DisplayName: field.GetDisplayName(),
			DataType: field.GetDataType(), IsRequired: field.GetIsRequired(),
			DefaultValue: field.GetDefaultValue(),
		})
	}
	resp, err := s.Emails.StartInboundExcelConversion(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getInboundExcelJob(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "jobId"), 10, 64)
	resp, err := s.Emails.GetInboundExcelConversionJob(r.Context(), &mailv1.GetInboundExcelConversionJobRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) syncMailbox(w http.ResponseWriter, r *http.Request) {
	// Paced per person. The sync fleet already collapses simultaneous presses
	// into one run, but that says nothing about one press a second — and each
	// press that does get through is a real IMAP conversation with somebody
	// else's server. A stuck auto-refresh needs no malice to saturate a
	// mailbox's own connection.
	op, _ := grpcx.OperatorFromContext(r.Context())
	if wait, ok := s.Limits.AllowSync(r.Context(), op.TenantID, op.EmployeeID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
		s.writeError(w, http.StatusTooManyRequests, "MAIL_SYNC_TOO_OFTEN",
			fmt.Sprintf("收信太频繁了，%d 秒后再试", int(wait.Seconds())))
		return
	}
	// 收哪个箱由令牌决定，和收件箱、已发送同一个口径。点 立即收信 是在问
	// 「客户回了没有」，问的是眼前这个箱。
	resp, err := s.Emails.SyncMailbox(r.Context(), &mailv1.SyncMailboxRequest{
		AccountId: unlockedAccount(r.Context()),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listMailboxSent(w http.ResponseWriter, r *http.Request) {
	// 看哪个信箱发出去的，**由令牌决定**——和收件箱同一个理由：参数是调用方
	// 说的，令牌是验过的。退出了 A 之后不该还能拿 B 的令牌翻 A 的已发送。
	resp, err := s.Emails.ListMailboxSent(r.Context(), &mailv1.ListMailboxSentRequest{
		Page:      pageFromQuery(r),
		Keyword:   r.URL.Query().Get("keyword"),
		Cursor:    r.URL.Query().Get("cursor"),
		AccountId: unlockedAccount(r.Context()),
		SortBy:    r.URL.Query().Get("sort_by"),
		SortDir:   r.URL.Query().Get("sort_dir"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// searchMail crosses folders, so unlike the list handlers it has no view.
//
// Every parameter it needs comes off the query string, which is the shape
// that has now twice gone wrong here — ?cursor= was added to the proto, the
// service and the browser and never read out of the URL, so every page
// returned the first. cursor_test.go covers the list handlers; this one is
// in it too.
func (s *Server) searchMail(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.Emails.SearchMail(r.Context(), &mailv1.SearchMailRequest{
		Keyword: q.Get("keyword"),
		Cursor:  q.Get("cursor"),
		Page:    pageFromQuery(r),
		// 搜哪个箱由令牌决定，和收件箱、已发送同一条理由。
		AccountId: unlockedAccount(r.Context()),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// excelUsage 出智能转换的用量账（计量）。
//
// 这条路由和其它邮件路由不同：不要求邮箱解锁。解锁证明的是「键盘前这个人
// 是这个邮箱的主人」，而这份账里没有任何信件内容——只有次数和 token 数。
// 拿邮箱解锁去守一份不含邮件的报表，只会逼人为了看账去解锁邮箱。
func (s *Server) excelUsage(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ExcelUsage(r.Context(), &mailv1.ExcelUsageRequest{
		Month: r.URL.Query().Get("month"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// previewInboundAttachment 把一个办公文档附件转成 PDF 并返回可显示的地址。
//
// 两个 id 都从路径里取：附件必须属于路径里那封信，而那封信必须属于调用的人。
// 这一条在邮件服务里判，不在这里——网关只转发身份。
func (s *Server) previewInboundAttachment(w http.ResponseWriter, r *http.Request) {
	inboundID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	attachmentID, _ := strconv.ParseInt(chi.URLParam(r, "attachmentId"), 10, 64)
	resp, err := s.Emails.PreviewInboundAttachment(r.Context(),
		&mailv1.PreviewInboundAttachmentRequest{
			InboundId: inboundID, AttachmentId: attachmentID,
		})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// downloadInboundAttachments 把一封信的附件打成压缩包送下去。
//
// 响应体照搬 exportMailThread 的写法：同样是「一个文件，直接存盘，不缓存、
// 不猜类型」。GET 而不是 POST，因为它不改任何状态，浏览器也能直接当链接用。
func (s *Server) downloadInboundAttachments(w http.ResponseWriter, r *http.Request) {
	inboundID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Emails.DownloadInboundAttachments(r.Context(),
		&mailv1.DownloadInboundAttachmentsRequest{InboundId: inboundID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachmentDisposition(resp.GetFileName()))
	w.Header().Set("Content-Length", strconv.Itoa(len(resp.GetContent())))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.GetContent())
}

// ---------------------------------------------------------- 自建文件夹

func (s *Server) listMailFolders(w http.ResponseWriter, r *http.Request) {
	accountID, _ := strconv.ParseInt(r.URL.Query().Get("account_id"), 10, 64)
	resp, err := s.Emails.ListMailFolders(r.Context(), &mailv1.ListMailFoldersRequest{AccountId: accountID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createMailFolder(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.CreateMailFolderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.CreateMailFolder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) renameMailFolder(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.RenameMailFolderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Emails.RenameMailFolder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteMailFolder(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Emails.DeleteMailFolder(r.Context(), &mailv1.DeleteMailFolderRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) moveInbound(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.MoveInboundRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Emails.MoveInbound(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// moveInboundBatch 是列表里勾选多封之后的「移动到」。
func (s *Server) moveInboundBatch(w http.ResponseWriter, r *http.Request) {
	req := &mailv1.MoveInboundBatchRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Emails.MoveInboundBatch(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
