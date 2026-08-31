package httpapi

import (
	"net/http"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func (s *Server) createSalesPlan(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSalesPlanRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateSalesPlan(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSalesPlans(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListSalesPlans(r.Context(), &prv1.ListSalesPlansRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addCustomerFeedback(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AddCustomerFeedbackRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.AddCustomerFeedback(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listCustomerFeedback(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCustomerFeedback(r.Context(), &prv1.ListCustomerFeedbackRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSalesProcurementRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSalesProcurementReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateSalesProcurementRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createShippingRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateShippingReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateShippingRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippingReworks(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListShippingReworks(r.Context(), &prv1.ListShippingReworksRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listMyShippingReworks(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListMyShippingReworks(r.Context(), &prv1.ListMyShippingReworksRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) resolveShippingRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ResolveShippingReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Sourcing.ResolveShippingRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
