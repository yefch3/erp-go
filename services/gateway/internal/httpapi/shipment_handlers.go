package httpapi

import (
	"net/http"
	"strconv"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
)

func (s *Server) listShipments(w http.ResponseWriter, r *http.Request) {
	contractID, _ := strconv.ParseInt(r.URL.Query().Get("contract_id"), 10, 64)
	resp, err := s.Shipments.ListShipments(r.Context(), &exv1.ListShipmentsRequest{
		Page:       pageFromQuery(r),
		Status:     r.URL.Query().Get("status"),
		Keyword:    r.URL.Query().Get("keyword"),
		ContractId: contractID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShipment(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.GetShipment(r.Context(), &exv1.GetShipmentRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createShipment(w http.ResponseWriter, r *http.Request) {
	req := &exv1.CreateShipmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Shipments.CreateShipment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShipment(w http.ResponseWriter, r *http.Request) {
	req := &exv1.UpdateShipmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipments.UpdateShipment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmSailing(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.ConfirmSailing(r.Context(), &exv1.ConfirmSailingRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markShipmentArrived(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.MarkArrived(r.Context(), &exv1.MarkArrivedRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelShipment(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.CancelShipment(r.Context(), &exv1.CancelShipmentRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listContractVessels(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.ListContractVessels(r.Context(),
		&exv1.ListContractVesselsRequest{ContractId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
