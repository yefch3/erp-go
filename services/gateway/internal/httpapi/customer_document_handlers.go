package httpapi

import (
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"google.golang.org/grpc"
)

func (s *Server) listCustomerDocuments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerDocuments(r.Context(), &mdv1.ListCustomerDocumentsRequest{CustomerId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) saveCustomerDocument(w http.ResponseWriter, r *http.Request) {
	// Check before accepting bytes as well as inside the storage service.
	if _, err := s.Customers.GetCustomer(r.Context(), &mdv1.GetCustomerRequest{Id: idFromPath(r)}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(11 << 20); err != nil {
		s.writeError(w, 400, "MD_DOCUMENT_UPLOAD_INVALID", "上传失败，文件不能超过 10 MB")
		return
	}
	if r.MultipartForm != nil {
		defer func() {
			if err := r.MultipartForm.RemoveAll(); err != nil && s.Log != nil {
				s.Log.Warn("customer upload temporary file cleanup failed", "err", err)
			}
		}()
	}
	days, err := strconv.ParseInt(r.FormValue("remindDays"), 10, 32)
	if err != nil {
		s.writeError(w, 400, "MD_DOCUMENT_DAYS_INVALID", "提前提醒天数无效")
		return
	}
	replaces := int64(0)
	if value := r.FormValue("replacesId"); value != "" {
		replaces, err = strconv.ParseInt(value, 10, 64)
		if err != nil || replaces < 0 {
			s.writeError(w, 400, "MD_DOCUMENT_ID_INVALID", "资料版本无效")
			return
		}
	}
	req := &mdv1.SaveCustomerDocumentRequest{CustomerId: idFromPath(r), ReplacesId: replaces, Title: r.FormValue("title"), Remark: r.FormValue("remark"), ExpiresOn: r.FormValue("expiresOn"), RemindDays: int32(days), ReminderEnabled: r.FormValue("reminderEnabled") == "true"}
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		req.FileName = header.Filename
		req.Content, err = io.ReadAll(io.LimitReader(file, (10<<20)+1))
		if err != nil {
			s.writeError(w, 400, "MD_DOCUMENT_UPLOAD_INVALID", "读取文件失败")
			return
		}
	} else if err != http.ErrMissingFile {
		s.writeError(w, 400, "MD_DOCUMENT_UPLOAD_INVALID", "读取文件失败")
		return
	}
	resp, err := s.Customers.SaveCustomerDocument(r.Context(), req, grpc.MaxCallSendMsgSize(12<<20))
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) getCustomerDocumentFile(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(chi.URLParam(r, "revisionId"), 10, 64)
	if err != nil || revision <= 0 {
		s.writeError(w, 400, "MD_DOCUMENT_ID_INVALID", "资料编号无效")
		return
	}
	resp, err := s.Customers.GetCustomerDocumentFile(r.Context(), &mdv1.GetCustomerDocumentFileRequest{CustomerId: idFromPath(r), RevisionId: revision}, grpc.MaxCallRecvMsgSize(12<<20))
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	disposition := "attachment"
	if r.URL.Query().Get("preview") == "1" && documentPreviewType(resp.ContentType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", resp.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": resp.FileName}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	_, _ = w.Write(resp.Content)
}
func documentPreviewType(kind string) bool {
	switch kind {
	case "application/pdf", "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

func (s *Server) deleteCustomerDocument(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(chi.URLParam(r, "revisionId"), 10, 64)
	if err != nil || revision <= 0 {
		s.writeError(w, 400, "MD_DOCUMENT_ID_INVALID", "附件编号无效")
		return
	}
	resp, err := s.Customers.DeleteCustomerDocument(r.Context(), &mdv1.DeleteCustomerDocumentRequest{CustomerId: idFromPath(r), RevisionId: revision})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
