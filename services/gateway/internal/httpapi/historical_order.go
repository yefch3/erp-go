package httpapi

import (
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"net/http"
)

func (s *Server) saveHistoricalOrder(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SaveHistoricalOrderRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// Historical orders may be recorded before their supplier is known. A
	// supplied ID still follows the same master-data access checks as before.
	if req.GetSupplierId() != 0 {
		if _, err := s.resolveActiveSupplier(r.Context(), req.GetSupplierId()); err != nil {
			s.writeGRPCError(w, err)
			return
		}
	}
	for _, line := range req.Lines {
		line.ProductCode = ""
		line.UomId = 0
		if line.ProductId != 0 {
			product, err := s.Catalog.GetProduct(r.Context(), &pdv1.GetProductRequest{Id: line.ProductId})
			if err != nil {
				s.writeGRPCError(w, err)
				return
			}
			p := product.GetProduct()
			if p.GetStatus() != "ACTIVE" {
				s.writeError(w, http.StatusConflict, "PO_PRODUCT_INACTIVE", "所选产品已停用")
				return
			}
			line.ProductName = p.GetName()
			line.ProductCode = p.GetCode()
			if line.Uom == "" {
				line.Uom = p.GetBaseUomCode()
			}
			if line.Uom == p.GetBaseUomCode() {
				line.UomId = p.GetBaseUomId()
			}
		}
	}
	resp, err := s.Orders.SaveHistoricalOrder(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// 历史补录单的采购员：候选名单和换人（#643）。规则都在采购服务里——谁看得见
// 这张单、谁接得住——网关只把着「维护采购订单」这项权限，不另做一套判断。
func (s *Server) listOrderBuyerCandidates(w http.ResponseWriter, r *http.Request) {
	response, err := s.Orders.ListOrderBuyerCandidates(r.Context(), &prv1.ListOrderBuyerCandidatesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, response)
}

func (s *Server) changeOrderBuyer(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ChangeOrderBuyerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	response, err := s.Orders.ChangeOrderBuyer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, response)
}

func (s *Server) updateHistoricalShippingContext(w http.ResponseWriter, r *http.Request) {
	req := &prv1.UpdateHistoricalShippingContextRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	response, err := s.Orders.UpdateHistoricalShippingContext(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, response)
}
