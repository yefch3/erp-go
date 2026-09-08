package httpapi

import (
	"encoding/json"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"google.golang.org/grpc"
	"io"
	"net/http"
)

func (s *Server) customerOffer(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8*1024*1024))
	if err != nil || !json.Valid(data) {
		s.writeError(w, 400, "OFFER_INPUT", "报价输入无效")
		return
	}
	result, err := s.Quotations.CustomerOffer(r.Context(), &exv1.CustomerOfferRequest{CommandJson: string(data)}, grpc.MaxCallRecvMsgSize(16*1024*1024))
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: json.RawMessage(result.GetResultJson())})
}
