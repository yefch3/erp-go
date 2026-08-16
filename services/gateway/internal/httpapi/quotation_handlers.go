package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
)

func (s *Server) listQuotations(w http.ResponseWriter, r *http.Request) {
	customerID, _ := strconv.ParseInt(r.URL.Query().Get("customer_id"), 10, 64)
	resp, err := s.Quotations.ListQuotations(r.Context(), &exv1.ListQuotationsRequest{
		Page:            pageFromQuery(r),
		Keyword:         r.URL.Query().Get("keyword"),
		CustomerId:      customerID,
		Status:          r.URL.Query().Get("status"),
		WithoutContract: r.URL.Query().Get("without_contract") == "true",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getQuotationWorkbook(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Quotations.GetQuotationWorkbook(r.Context(), &exv1.GetQuotationWorkbookRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(resp.GetFileName()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.GetFileData())
}

func (s *Server) getQuotationPDF(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Quotations.GetQuotationPdf(r.Context(), &exv1.GetQuotationPdfRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(resp.GetFileName()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.GetFileData())
}

func (s *Server) getQuotation(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Quotations.GetQuotation(r.Context(), &exv1.GetQuotationRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createQuotation(w http.ResponseWriter, r *http.Request) {
	req := &exv1.CreateQuotationRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveCustomer(r.Context(), req.GetCustomerId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Quotations.CreateQuotation(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateQuotation(w http.ResponseWriter, r *http.Request) {
	req := &exv1.UpdateQuotationRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if _, err := s.resolveActiveCustomer(r.Context(), req.GetCustomerId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Quotations.UpdateQuotation(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) sendQuotation(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Quotations.SendQuotation(r.Context(), &exv1.SendQuotationRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) respondQuotation(w http.ResponseWriter, r *http.Request) {
	req := &exv1.RespondQuotationRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Quotations.RespondQuotation(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelQuotation(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Quotations.CancelQuotation(r.Context(), &exv1.CancelQuotationRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
