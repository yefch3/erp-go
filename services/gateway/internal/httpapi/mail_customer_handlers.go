package httpapi

import (
	mailv1 "github.com/sgao19/erp-go/gen/go/erp/mail/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"net/http"
)

// Mail authorization is checked on every lookup and write, before masterdata
// is called. The browser cannot substitute an inbound id in the JSON body.
func (s *Server) authorizeMailCustomer(w http.ResponseWriter, r *http.Request) bool {
	_, err := s.Emails.GetInbound(r.Context(), &mailv1.GetInboundRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return false
	}
	return true
}
func (s *Server) matchMailCustomer(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeMailCustomer(w, r) {
		return
	}
	v, err := s.Customers.MatchMailCustomer(r.Context(), &mdv1.MatchMailCustomerRequest{CompanyName: r.URL.Query().Get("company_name"), Email: r.URL.Query().Get("email")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, v)
}
func (s *Server) getMailCustomerLink(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeMailCustomer(w, r) {
		return
	}
	v, err := s.Customers.GetMailCustomerLink(r.Context(), &mdv1.GetMailCustomerLinkRequest{InboundId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, v)
}
func (s *Server) saveMailCustomer(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeMailCustomer(w, r) {
		return
	}
	req := &mdv1.SaveMailCustomerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.InboundId = idFromPath(r)
	v, err := s.Customers.SaveMailCustomer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, v)
}
