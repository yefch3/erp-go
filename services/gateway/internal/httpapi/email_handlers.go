package httpapi

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
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

func (s *Server) deleteSignature(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.DeleteSignature(r.Context(), &mailv1.DeleteSignatureRequest{Id: idFromPath(r)})
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
	resp, err := s.Emails.SaveDraft(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listDrafts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListDrafts(r.Context(), &mailv1.ListDraftsRequest{})
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

func (s *Server) getMyMailAccount(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetMyMailAccount(r.Context(), &mailv1.GetMyMailAccountRequest{})
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
	resp, err := s.Emails.ListInbound(r.Context(), &mailv1.ListInboundRequest{
		Page:    pageFromQuery(r),
		Keyword: r.URL.Query().Get("keyword"),
		View:    r.URL.Query().Get("view"),
		Cursor:  r.URL.Query().Get("cursor"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getMailThread(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetMailThread(r.Context(), &mailv1.GetMailThreadRequest{
		ThreadKey: r.URL.Query().Get("key"),
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

func (s *Server) syncMailbox(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.SyncMailbox(r.Context(), &mailv1.SyncMailboxRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listMailboxSent(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListMailboxSent(r.Context(), &mailv1.ListMailboxSentRequest{
		Page:    pageFromQuery(r),
		Keyword: r.URL.Query().Get("keyword"),
		Cursor:  r.URL.Query().Get("cursor"),
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
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
