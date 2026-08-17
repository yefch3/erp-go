package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
)

func (s *Server) previewOrderImport(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PreviewOrderImportRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if req.GetSourceType() == "" {
		req.SourceType = "MAIL_EXCEL"
	}
	if req.GetSourceType() == "MAIL_EXCEL" {
		if req.GetSourceMailId() <= 0 {
			s.writeGRPCError(w, apierr.Invalid("PO_IMPORT_SOURCE_REQUIRED", "缺少来源邮件"))
			return
		}
		mail, err := s.Emails.GetInbound(r.Context(), &mailv1.GetInboundRequest{Id: req.GetSourceMailId()})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		if req.GetSourceAttachmentId() != 0 {
			found := false
			for _, attachment := range mail.GetMail().GetAttachments() {
				if attachment.GetId() == req.GetSourceAttachmentId() {
					found = true
					break
				}
			}
			if !found {
				s.writeGRPCError(w, apierr.Invalid("PO_IMPORT_ATTACHMENT_NOT_FOUND", "来源附件不存在"))
				return
			}
		}
	}
	resp, err := s.Orders.PreviewOrderImport(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) previewPurchaseTemplateImport(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PreviewPurchaseTemplateImportRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.PreviewPurchaseTemplateImport(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmOrderImport(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ConfirmOrderImportRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ImportToken = strings.TrimSpace(chi.URLParam(r, "importToken"))
	if _, err := s.resolveActiveSupplier(r.Context(), req.GetSupplierId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Orders.ConfirmOrderImport(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
