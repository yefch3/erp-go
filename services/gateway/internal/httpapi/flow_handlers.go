package httpapi

import (
	"net/http"

	apv1 "github.com/sgao19/erp-go/gen/go/erp/approval/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
)

// ---------------------------------------------------------------- flows

func (s *Server) listFlows(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Approval.ListDefinitions(r.Context(), &apv1.ListDefinitionsRequest{
		BizType: r.URL.Query().Get("biz_type"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getFlow(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Approval.GetDefinition(r.Context(), &apv1.GetDefinitionRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) saveFlow(w http.ResponseWriter, r *http.Request) {
	req := &apv1.SaveDefinitionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Approval.SaveDefinition(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- scopes

func (s *Server) listDataScopes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Access.ListDataScopes(r.Context(), &iamv1.ListDataScopesRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setDataScope(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.SetDataScopeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if req.Scope == nil {
		req.Scope = &iamv1.DataScope{}
	}
	req.Scope.RoleId = idFromPath(r)
	resp, err := s.Access.SetDataScope(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deleteFlowBand(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Approval.DeleteBand(r.Context(), &apv1.DeleteBandRequest{
		BizType:   r.URL.Query().Get("biz_type"),
		MinAmount: r.URL.Query().Get("min_amount"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
