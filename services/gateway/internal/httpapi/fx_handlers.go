package httpapi

import (
	"encoding/json"
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

func (s *Server) fxAnomalies(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Fx.ListAnomalies(r.Context(), &fxv1.ListAnomaliesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) fxEffective(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Fx.ListEffectiveRates(r.Context(), &fxv1.ListEffectiveRatesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) fxConfirm(w http.ResponseWriter, r *http.Request) {
	var req fxv1.ConfirmEffectiveRateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&req); err != nil {
		s.writeError(w, 400, "FX_INPUT", "汇率输入无效")
		return
	}
	resp, err := s.Fx.ConfirmEffectiveRate(r.Context(), &req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
