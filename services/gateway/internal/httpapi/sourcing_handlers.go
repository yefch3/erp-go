package httpapi

import (
	"net/http"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func (s *Server) listSourcingCases(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCases(r.Context(), &prv1.ListCasesRequest{
		Page: pageFromQuery(r), Status: r.URL.Query().Get("status"), Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSourcingCase(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.GetCase(r.Context(), &prv1.GetCaseRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSourcingCase(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateCaseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Sourcing.CreateCase(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
