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

func (s *Server) listSupplierDocuments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSupplierDocuments(r.Context(), &mdv1.ListSupplierDocumentsRequest{SupplierId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) saveSupplierDocument(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Suppliers.GetSupplier(r.Context(), &mdv1.GetSupplierRequest{Id: idFromPath(r)}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(11 << 20); err != nil {
		s.writeError(w, 400, "MD_SUPPLIER_DOCUMENT_UPLOAD_INVALID", "上传失败，文件不能超过 10 MB")
		return
	}
	if r.MultipartForm != nil {
		defer func() {
			if err := r.MultipartForm.RemoveAll(); err != nil && s.Log != nil {
				s.Log.Warn("supplier upload temporary file cleanup failed", "err", err)
			}
		}()
	}
	days, err := strconv.ParseInt(r.FormValue("remindDays"), 10, 32)
	if err != nil {
		s.writeError(w, 400, "MD_SUPPLIER_DOCUMENT_DAYS_INVALID", "提醒天数无效")
		return
	}
	replaces := int64(0)
	if value := r.FormValue("replacesId"); value != "" {
		replaces, err = strconv.ParseInt(value, 10, 64)
		if err != nil || replaces < 0 {
			s.writeError(w, 400, "MD_SUPPLIER_DOCUMENT_ID_INVALID", "资料版本无效")
			return
		}
	}
	req := &mdv1.SaveSupplierDocumentRequest{SupplierId: idFromPath(r), ReplacesId: replaces, Title: r.FormValue("title"), Remark: r.FormValue("remark"), ExpiresOn: r.FormValue("expiresOn"), RemindDays: int32(days), ReminderEnabled: r.FormValue("reminderEnabled") == "true"}
	file, header, err := r.FormFile("file")
	if err == nil {
		defer func() { _ = file.Close() }()
		req.FileName = header.Filename
		req.Content, err = io.ReadAll(io.LimitReader(file, (10<<20)+1))
		if err != nil {
			s.writeError(w, 400, "MD_SUPPLIER_DOCUMENT_UPLOAD_INVALID", "读取文件失败")
			return
		}
	} else if err != http.ErrMissingFile {
		s.writeError(w, 400, "MD_SUPPLIER_DOCUMENT_UPLOAD_INVALID", "读取文件失败")
		return
	}
	resp, err := s.Suppliers.SaveSupplierDocument(r.Context(), req, grpc.MaxCallSendMsgSize(12<<20))
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) getSupplierDocumentFile(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(chi.URLParam(r, "revisionId"), 10, 64)
	if err != nil || revision <= 0 {
		s.writeError(w, 400, "MD_SUPPLIER_DOCUMENT_ID_INVALID", "资料编号无效")
		return
	}
	resp, err := s.Suppliers.GetSupplierDocumentFile(r.Context(), &mdv1.GetSupplierDocumentFileRequest{SupplierId: idFromPath(r), RevisionId: revision}, grpc.MaxCallRecvMsgSize(12<<20))
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
func (s *Server) deleteSupplierDocument(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(chi.URLParam(r, "revisionId"), 10, 64)
	if err != nil || revision <= 0 {
		s.writeError(w, 400, "MD_SUPPLIER_DOCUMENT_ID_INVALID", "附件编号无效")
		return
	}
	resp, err := s.Suppliers.DeleteSupplierDocument(r.Context(), &mdv1.DeleteSupplierDocumentRequest{SupplierId: idFromPath(r), RevisionId: revision})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
