package httpapi

import (
	"net/http"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// The supplier reconciliation view: derived numbers only, nothing to write.

func (s *Server) listSupplierStatements(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListSupplierStatements(r.Context(), &prv1.ListSupplierStatementsRequest{
		Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSupplierStatement(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetSupplierStatement(r.Context(), &prv1.GetSupplierStatementRequest{
		SupplierId: idFromPath(r),
		Currency:   r.URL.Query().Get("currency"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
