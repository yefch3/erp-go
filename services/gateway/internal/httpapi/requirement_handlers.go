package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
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

func (s *Server) listExecutionSupplierQuotes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Requirements.ListExecutionSupplierQuotes(r.Context(), &prv1.ListExecutionSupplierQuotesRequest{RequirementId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// resolveExecutionSupplier lets a buyer type a factory name while preserving
// a real supplier master-data identity for the purchase order and payment.
// Exact active matches are reused; a new lightweight GENERAL supplier is
// created only when the typed name does not already exist.
func (s *Server) resolveExecutionSupplier(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name         string `json:"name"`
		Currency     string `json:"currency"`
		PaymentTerms string `json:"payment_terms"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_JSON", "请求格式不正确")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Name == "" {
		s.writeError(w, http.StatusBadRequest, "EXECUTION_SUPPLIER_NAME_REQUIRED", "请输入工厂名称")
		return
	}
	listed, err := s.Suppliers.ListSuppliers(r.Context(), &mdv1.ListSuppliersRequest{
		Page: &commonv1.PageRequest{Page: 1, PageSize: 200}, Keyword: input.Name, Status: "ACTIVE",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	for _, supplier := range listed.GetSuppliers() {
		for _, candidate := range []string{supplier.GetName(), supplier.GetNameZh(), supplier.GetNameEn(), supplier.GetShortName(), supplier.GetCode()} {
			if strings.EqualFold(strings.TrimSpace(candidate), input.Name) {
				s.writeProto(w, &mdv1.CreateSupplierResponse{Supplier: supplier})
				return
			}
		}
	}
	created, err := s.Suppliers.CreateSupplier(r.Context(), &mdv1.CreateSupplierRequest{
		Name: input.Name, NameZh: input.Name, Currency: input.Currency,
		PaymentTerm: input.PaymentTerms, BusinessTypes: []string{"GENERAL"},
		Remark: "由采购实单询价录入",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, created)
}

func (s *Server) saveExecutionSupplierQuote(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SaveExecutionSupplierQuoteRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.RequirementId = idFromPath(r)
	if _, err := s.resolveActiveSupplier(r.Context(), req.GetSupplierId()); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Requirements.SaveExecutionSupplierQuote(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteExecutionSupplierQuote(w http.ResponseWriter, r *http.Request) {
	quoteID, _ := strconv.ParseInt(chi.URLParam(r, "quoteId"), 10, 64)
	resp, err := s.Requirements.DeleteExecutionSupplierQuote(r.Context(), &prv1.DeleteExecutionSupplierQuoteRequest{RequirementId: idFromPath(r), Id: quoteID})
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
