package httpapi

import (
	"encoding/json"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"net/http"
)

func (s *Server) contractWorkflow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string          `json:"action"`
		Reason   string          `json:"reason"`
		Data     json.RawMessage `json:"data"`
		Revision int64           `json:"revision"`
		ID       int64           `json:"id"`
	}
	if r.Method == http.MethodGet {
		body.Action = "get"
	} else if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 3*1024*1024)).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_BODY", "请求格式无效")
		return
	}
	id := idFromPath(r)
	if id == 0 {
		id = body.ID
	}
	out, err := s.Contracts.ContractWorkflow(r.Context(), &exv1.ContractWorkflowRequest{Id: id, Action: body.Action, Reason: body.Reason, DataJson: string(body.Data), Revision: body.Revision})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: json.RawMessage(out.GetDataJson())})
}
