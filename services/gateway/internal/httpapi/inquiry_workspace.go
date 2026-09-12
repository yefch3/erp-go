package httpapi

import (
	"encoding/json"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"google.golang.org/grpc"
	"io"
	"net/http"
	"net/url"
)

// The procurement service validates a closed command schema and independently
// enforces action permissions, ownership, scope and tenant on every request.
func (s *Server) inquiryWorkspace(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 12*1024*1024))
	if err != nil || !json.Valid(data) {
		s.writeError(w, 400, "INQUIRY_INPUT", "询盘输入无效")
		return
	}
	resp, err := s.Sourcing.InquiryWorkspace(r.Context(), &prv1.InquiryWorkspaceRequest{CommandJson: string(data)}, grpc.MaxCallRecvMsgSize(16*1024*1024))
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: json.RawMessage(resp.GetResultJson())})
}

// Stop legacy workflows before any mail/file/export side effects at the edge.
func (s *Server) retiredInquiryAction(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, http.StatusConflict, "INQUIRY_WORKFLOW_REPLACED", "此询价流程已取消，请进入客户询盘或部门询价页面")
}

// Every download reuses source visibility. A copied URL does not grant access.
func (s *Server) inquiryFile(w http.ResponseWriter, r *http.Request) {
	data, _ := json.Marshal(map[string]string{"action": "readFile", "id": r.URL.Query().Get("id"), "view": r.URL.Query().Get("view"), "fileKey": r.URL.Query().Get("key")})
	resp, err := s.Sourcing.InquiryWorkspace(r.Context(), &prv1.InquiryWorkspaceRequest{CommandJson: string(data)}, grpc.MaxCallRecvMsgSize(16*1024*1024))
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	var file struct {
		FileData []byte `json:"fileData"`
		FileName string `json:"fileName"`
	}
	if err = json.Unmarshal([]byte(resp.GetResultJson()), &file); err != nil {
		s.writeError(w, 500, "INQUIRY_FILE", "附件读取失败")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(file.FileName))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(file.FileData)
}
