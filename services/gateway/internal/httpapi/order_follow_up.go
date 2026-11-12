package httpapi

import (
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"net/http"
)

func (s *Server) addOrderFollowUp(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AddOrderFollowUpRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.AddOrderFollowUp(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
