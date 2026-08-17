package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func (s *Server) exportPurchaseTemplate(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ExportPurchaseTemplateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Requirements.ExportPurchaseTemplate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(resp.GetFileName()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp.GetFileData())
}

func (s *Server) listRequirements(w http.ResponseWriter, r *http.Request) {
	contractID, _ := strconv.ParseInt(r.URL.Query().Get("contract_id"), 10, 64)
	resp, err := s.Requirements.ListRequirements(r.Context(), &prv1.ListRequirementsRequest{
		Page:       pageFromQuery(r),
		Status:     r.URL.Query().Get("status"),
		ContractId: contractID,
		Keyword:    r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createRequirement(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateRequirementRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Requirements.CreateRequirement(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getRequirement(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Requirements.GetRequirement(r.Context(), &prv1.GetRequirementRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelRequirement(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CancelRequirementRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Requirements.CancelRequirement(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reopenRequirement(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Requirements.ReopenRequirement(r.Context(), &prv1.ReopenRequirementRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listRequirementOrders(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Requirements.ListRequirementOrders(r.Context(), &prv1.ListRequirementOrdersRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
