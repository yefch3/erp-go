package httpapi

import (
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"net/http"
)

func (s *Server) getCustomerBasic(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.GetCustomerBasic(r.Context(), &mdv1.GetCustomerBasicRequest{CustomerId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) saveCustomerBasic(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.SaveCustomerBasicRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CustomerId = idFromPath(r)
	resp, err := s.Customers.SaveCustomerBasic(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
