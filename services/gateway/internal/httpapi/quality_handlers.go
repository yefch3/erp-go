package httpapi

import (
	"net/http"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
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
func (s *Server) decideQualityRelease(w http.ResponseWriter, r *http.Request) {
	req := &prv1.DecideQualityReleaseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.DecideQualityRelease(r.Context(), req)
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
