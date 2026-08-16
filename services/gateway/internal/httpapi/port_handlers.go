package httpapi

import (
	"net/http"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

func (s *Server) listPorts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Ports.ListPorts(r.Context(), &mdv1.ListPortsRequest{Page: pageFromQuery(r), Keyword: r.URL.Query().Get("keyword"), CountryCode: r.URL.Query().Get("country_code"), Status: r.URL.Query().Get("status")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listPortCountries(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Ports.ListPortCountries(r.Context(), &mdv1.ListPortCountriesRequest{Status: r.URL.Query().Get("status")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) importPorts(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.ImportPortsRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Ports.ImportPorts(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) getPort(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Ports.GetPort(r.Context(), &mdv1.GetPortRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createPort(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreatePortRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Ports.CreatePort(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updatePort(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdatePortRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if req.Port != nil {
		req.Port.Id = idFromPath(r)
	}
	resp, err := s.Ports.UpdatePort(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) setPortStatus(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.SetPortStatusRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Ports.SetPortStatus(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getPortDeactivationImpact(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Ports.GetPortDeactivationImpact(r.Context(), &mdv1.GetPortDeactivationImpactRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listPortChanges(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Ports.ListPortChanges(r.Context(), &mdv1.ListPortChangesRequest{PortId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
