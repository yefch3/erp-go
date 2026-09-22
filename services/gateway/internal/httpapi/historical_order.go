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
	if _, err := s.resolveActiveSupplier(r.Context(), req.GetSupplierId()); err != nil {
		s.writeGRPCError(w, err)
		return
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
