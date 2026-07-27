package httpapi

import (
	"net/http"
	"strconv"

	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
)

func (s *Server) fxLatest(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Fx.GetLatestRate(r.Context(), &fxv1.GetLatestRateRequest{
		QuoteCurrency: r.URL.Query().Get("currency"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) fxRates(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	resp, err := s.Fx.ListRates(r.Context(), &fxv1.ListRatesRequest{
		QuoteCurrency: r.URL.Query().Get("currency"),
		Days:          int32(days),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) fxSetManual(w http.ResponseWriter, r *http.Request) {
	req := &fxv1.SetManualRateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Fx.SetManualRate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) fxAnomalies(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Fx.ListAnomalies(r.Context(), &fxv1.ListAnomaliesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
