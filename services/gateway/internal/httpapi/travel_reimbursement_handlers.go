package httpapi

import (
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"net/http"
)

func (s *Server) listTravelReimbursements(w http.ResponseWriter, r *http.Request) {
	all, err := s.hasPermission(r, "procurement:reimbursement:manage")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Orders.ListTravelReimbursements(r.Context(), &prv1.ListTravelReimbursementsRequest{IncludeAll: all, Status: r.URL.Query().Get("status")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createTravelReimbursement(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateTravelReimbursementRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.CreateTravelReimbursement(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateTravelReimbursement(w http.ResponseWriter, r *http.Request) {
	req := &prv1.UpdateTravelReimbursementRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.UpdateTravelReimbursement(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) submitTravelReimbursement(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.SubmitTravelReimbursement(r.Context(), &prv1.SubmitTravelReimbursementRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) presignTravelReimbursementFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PresignTravelReimbursementFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.PresignTravelReimbursementFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) attachTravelReimbursementFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AttachTravelReimbursementFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.AttachTravelReimbursementFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) markTravelReimbursementPaid(w http.ResponseWriter, r *http.Request) {
	req := &prv1.MarkTravelReimbursementPaidRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.MarkTravelReimbursementPaid(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) reverseTravelReimbursementPayment(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReverseTravelReimbursementPaymentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.ReverseTravelReimbursementPayment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
