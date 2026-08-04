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
