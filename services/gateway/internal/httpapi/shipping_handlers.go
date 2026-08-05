package httpapi

import (
	"net/http"

	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

func (s *Server) getShippingStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetModuleStatus(r.Context(), &shippingv1.GetModuleStatusRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippingSchedules(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.Shipping.ListSchedules(r.Context(), &shippingv1.ListSchedulesRequest{
		Keyword: q.Get("keyword"), Status: q.Get("status"), PortOfLoading: q.Get("port_of_loading"), PortOfDischarge: q.Get("port_of_discharge"),
		EtdFrom: q.Get("etd_from"), EtdTo: q.Get("etd_to"), EtaFrom: q.Get("eta_from"), EtaTo: q.Get("eta_to"), Page: pageFromQuery(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingSchedule(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetSchedule(r.Context(), &shippingv1.GetScheduleRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.CreateScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Shipping.CreateSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingScheduleStatus(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateScheduleStatusRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateScheduleStatus(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.CancelScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.CancelSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
