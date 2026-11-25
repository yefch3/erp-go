package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"google.golang.org/grpc"
)

func (s *Server) applyQualityInspection(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ApplyQualityInspectionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.ApplyQualityInspection(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listQualityTasks(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListQualityInspectionTasks(r.Context(), &prv1.ListQualityInspectionTasksRequest{Page: pageFromQuery(r), Tab: r.URL.Query().Get("tab"), Keyword: r.URL.Query().Get("keyword")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) getQualityTask(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetQualityInspectionTask(r.Context(), &prv1.GetQualityInspectionTaskRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) startQualityTask(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.StartQualityInspection(r.Context(), &prv1.StartQualityInspectionRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) submitQualityRound(w http.ResponseWriter, r *http.Request) {
	req := &prv1.SubmitQualityInspectionRoundRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.SubmitQualityInspectionRound(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) presignQualityFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PresignQualityInspectionFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.PresignQualityInspectionFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) registerQualityFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.RegisterQualityInspectionFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.RegisterQualityInspectionFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deleteQualityFile(w http.ResponseWriter, r *http.Request) {
	fileID, _ := strconv.ParseInt(chi.URLParam(r, "fileID"), 10, 64)
	resp, err := s.Orders.DeleteQualityInspectionFile(r.Context(), &prv1.DeleteQualityInspectionFileRequest{Id: idFromPath(r), FileId: fileID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// Unary file responses are bounded by the 64 MB upload limit. Keep large
// responses from being buffered concurrently without limit by the gateway.
var qualityDownloadSlots = make(chan struct{}, 2)

func (s *Server) readQualityTaskFile(w http.ResponseWriter, r *http.Request) {
	s.readQualityFile(w, r, idFromPath(r), 0)
}

func (s *Server) readOrderQualityFile(w http.ResponseWriter, r *http.Request) {
	taskID, _ := strconv.ParseInt(chi.URLParam(r, "taskID"), 10, 64)
	poID := idFromPath(r)
	if poID <= 0 {
		s.writeError(w, http.StatusNotFound, "QUALITY_FILE_NOT_FOUND", "质检附件不存在")
		return
	}
	s.readQualityFile(w, r, taskID, poID)
}

func (s *Server) readQualityFile(w http.ResponseWriter, r *http.Request, taskID, poID int64) {
	fileID, _ := strconv.ParseInt(chi.URLParam(r, "fileID"), 10, 64)
	if taskID <= 0 || fileID <= 0 {
		s.writeError(w, http.StatusNotFound, "QUALITY_FILE_NOT_FOUND", "质检附件不存在")
		return
	}
	select {
	case qualityDownloadSlots <- struct{}{}:
		defer func() { <-qualityDownloadSlots }()
	case <-r.Context().Done():
		return
	}
	resp, err := s.Orders.ReadQualityInspectionFile(r.Context(),
		&prv1.ReadQualityInspectionFileRequest{Id: taskID, FileId: fileID, PoId: poID},
		grpc.MaxCallRecvMsgSize(65<<20))
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", resp.GetContentType())
	w.Header().Set("Content-Disposition", attachmentDisposition(resp.GetFileName()))
	w.Header().Set("Content-Length", strconv.Itoa(len(resp.GetContent())))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(resp.GetContent())
}

func (s *Server) getOrderQualityInspections(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetOrderQualityInspections(r.Context(), &prv1.GetOrderQualityInspectionsRequest{PoId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listQualityProcurementTodos(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListQualityProcurementTodos(r.Context(), &prv1.ListQualityProcurementTodosRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) recordQualityProcurementHandling(w http.ResponseWriter, r *http.Request) {
	req := &prv1.RecordQualityProcurementHandlingRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.PoId = idFromPath(r)
	resp, err := s.Orders.RecordQualityProcurementHandling(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listQualitySourceOrders(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListQualitySourceOrders(r.Context(), &prv1.ListQualitySourceOrdersRequest{Page: pageFromQuery(r), Keyword: r.URL.Query().Get("keyword")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) getQualitySourceOrder(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.ListQualitySourceOrders(r.Context(), &prv1.ListQualitySourceOrdersRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createQualityInspection(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateQualityInspectionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.CreateQualityInspection(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) qualityAccess(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.QualityAccess(r.Context(), &prv1.QualityAccessRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateQualityBasics(w http.ResponseWriter, r *http.Request) {
	req := &prv1.UpdateQualityBasicsRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.UpdateQualityBasics(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deleteQualityTask(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.DeleteQualityTask(r.Context(), &prv1.DeleteQualityTaskRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
