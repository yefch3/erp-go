package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	ntv1 "github.com/sgao19/erp-go/gen/go/erp/notification/v1"
)

func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	senderID, _ := strconv.ParseInt(r.URL.Query().Get("sender_id"), 10, 64)
	resp, err := s.Emails.ListCampaigns(r.Context(), &ntv1.ListCampaignsRequest{
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
	resp, err := s.Emails.GetCampaign(r.Context(), &ntv1.GetCampaignRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) previewCampaign(w http.ResponseWriter, r *http.Request) {
	req := &ntv1.PreviewCampaignRequest{}
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
	req := &ntv1.CreateCampaignRequest{}
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
	resp, err := s.Emails.ListMessages(r.Context(), &ntv1.ListMessagesRequest{
		Page:          pageFromQuery(r),
		CampaignId:    campaignID,
		SenderId:      senderID,
		Status:        q.Get("status"),
		AttentionOnly: q.Get("attention_only") == "true",
		Keyword:       q.Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getEmailMessage(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetMessage(r.Context(), &ntv1.GetMessageRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) requeueEmailMessage(w http.ResponseWriter, r *http.Request) {
	req := &ntv1.RequeueMessageRequest{}
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
	req := &ntv1.AbandonMessageRequest{}
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
	resp, err := s.Emails.ListSignatures(r.Context(), &ntv1.ListSignaturesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSignature(w http.ResponseWriter, r *http.Request) {
	req := &ntv1.CreateSignatureRequest{}
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
	resp, err := s.Emails.DeleteSignature(r.Context(), &ntv1.DeleteSignatureRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSuppressions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListSuppressions(r.Context(), &ntv1.ListSuppressionsRequest{
		Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addSuppression(w http.ResponseWriter, r *http.Request) {
	req := &ntv1.AddSuppressionRequest{}
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
	resp, err := s.Emails.RemoveSuppression(r.Context(), &ntv1.RemoveSuppressionRequest{
		Email: r.URL.Query().Get("email"),
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
	req := &ntv1.PresignAttachmentRequest{}
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
	req := &ntv1.RegisterAttachmentRequest{}
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
	resp, err := s.Emails.ListAttachments(r.Context(), &ntv1.ListAttachmentsRequest{
		CampaignId: campaignID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignMailImage(w http.ResponseWriter, r *http.Request) {
	req := &ntv1.PresignImageRequest{}
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
	req := &ntv1.RegisterImageRequest{}
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

func (s *Server) listMailImages(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.ListImages(r.Context(), &ntv1.ListImagesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) withdrawMailImage(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.WithdrawImage(r.Context(), &ntv1.WithdrawImageRequest{Id: idFromPath(r)})
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
	resp, err := s.Emails.FetchImage(r.Context(), &ntv1.FetchImageRequest{
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
	resp, err := s.Emails.ListSenders(r.Context(), &ntv1.ListSendersRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- drafts

func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request) {
	req := &ntv1.SaveDraftRequest{}
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
	resp, err := s.Emails.ListDrafts(r.Context(), &ntv1.ListDraftsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.GetDraft(r.Context(), &ntv1.GetDraftRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteDraft(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.DeleteDraft(r.Context(), &ntv1.DeleteDraftRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) sendDraft(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Emails.SendDraft(r.Context(), &ntv1.SendDraftRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
