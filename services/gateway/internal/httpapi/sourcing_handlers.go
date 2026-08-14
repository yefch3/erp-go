package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
)

func (s *Server) listSourcingCases(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCases(r.Context(), &prv1.ListCasesRequest{
		Page: pageFromQuery(r), Status: r.URL.Query().Get("status"), Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSourcingCase(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.GetCase(r.Context(), &prv1.GetCaseRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSourcingCase(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateCaseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Sourcing.CreateCase(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmSourcingLines(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ConfirmLinesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.ConfirmLines(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reviewSourcingLine(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReviewLineRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	req.LineId, _ = strconv.ParseInt(chi.URLParam(r, "lineId"), 10, 64)
	if req.GetDecision() == "CONFIRMED" {
		product, err := s.Catalog.GetProduct(r.Context(), &pdv1.GetProductRequest{Id: req.GetProductId()})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		if product.GetProduct().GetStatus() != "ACTIVE" {
			s.writeError(w, http.StatusConflict, "SC_PRODUCT_INACTIVE", "所选产品已停用")
			return
		}
		req.UomId = product.GetProduct().GetBaseUomId()
		if req.GetSkuId() != 0 {
			found := false
			for _, sku := range product.GetSkus() {
				if sku.GetId() == req.GetSkuId() && sku.GetStatus() == "ACTIVE" {
					found = true
					break
				}
			}
			if !found {
				s.writeError(w, http.StatusBadRequest, "SC_SKU_INVALID", "所选 SKU 不属于当前产品或已停用")
				return
			}
		}
	}
	resp, err := s.Sourcing.ReviewLine(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createFactoryRFQ(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateFactoryRfqRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateFactoryRfq(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listFactoryRFQs(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListFactoryRfqs(r.Context(), &prv1.ListFactoryRfqsRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSupplierQuote(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSupplierQuoteRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryRfqId = idFromPath(r)
	resp, err := s.Sourcing.CreateSupplierQuote(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSupplierQuoteComparison(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListSupplierQuoteComparison(r.Context(), &prv1.ListSupplierQuoteComparisonRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
