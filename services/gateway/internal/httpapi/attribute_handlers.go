package httpapi

import (
	"net/http"
	"strconv"

	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
)

func (s *Server) resolveAttributes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Attributes.ResolveAttributes(r.Context(),
		&pdv1.ResolveAttributesRequest{CategoryId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getAttributeTemplate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Attributes.GetTemplate(r.Context(),
		&pdv1.GetTemplateRequest{CategoryId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) saveAttributeTemplate(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.SaveTemplateRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CategoryId = idFromPath(r)
	resp, err := s.Attributes.SaveTemplate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setProductAttributes(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.SetProductAttributesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ProductId = idFromPath(r)
	resp, err := s.Attributes.SetProductAttributes(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) setSkuAttributes(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.SetSkuAttributesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.SkuId = idFromPath(r)
	resp, err := s.Attributes.SetSkuAttributes(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) recallCandidates(w http.ResponseWriter, r *http.Request) {
	categoryID, _ := strconv.ParseInt(r.URL.Query().Get("category_id"), 10, 64)
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 32)
	resp, err := s.Attributes.RecallCandidates(r.Context(), &pdv1.RecallCandidatesRequest{
		CategoryId:     categoryID,
		AttributesJson: r.URL.Query().Get("attributes"),
		Keyword:        r.URL.Query().Get("keyword"),
		Limit:          int32(limit),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
