package httpapi

import (
	"net/http"
	"strconv"

	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
)

// ---------------------------------------------------------------- catalog

func (s *Server) listCategories(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.ListCategories(r.Context(), &pdv1.ListCategoriesRequest{
		Status: r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.CreateCategoryRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Catalog.CreateCategory(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.UpdateCategoryRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Catalog.UpdateCategory(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateCategory(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.DeactivateCategory(r.Context(), &pdv1.DeactivateCategoryRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listUoms(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.ListUoms(r.Context(), &pdv1.ListUomsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createUom(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.CreateUomRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Catalog.CreateUom(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- products

func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	categoryID, _ := strconv.ParseInt(r.URL.Query().Get("category_id"), 10, 64)
	resp, err := s.Catalog.ListProducts(r.Context(), &pdv1.ListProductsRequest{
		Page:       pageFromQuery(r),
		Keyword:    r.URL.Query().Get("keyword"),
		CategoryId: categoryID,
		Status:     r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getProduct(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.GetProduct(r.Context(), &pdv1.GetProductRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createProduct(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.CreateProductRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Catalog.CreateProduct(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateProduct(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.UpdateProductRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Catalog.UpdateProduct(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateProduct(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.DeactivateProduct(r.Context(), &pdv1.DeactivateProductRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) activateProduct(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.ActivateProduct(r.Context(), &pdv1.ActivateProductRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSkus(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.ListSkus(r.Context(), &pdv1.ListSkusRequest{ProductId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSku(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.CreateSkuRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ProductId = idFromPath(r)
	resp, err := s.Catalog.CreateSku(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateSku(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Catalog.DeactivateSku(r.Context(), &pdv1.DeactivateSkuRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- attachments

func (s *Server) presignUpload(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.PresignUploadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ProductId = idFromPath(r)
	// The response carries a URL straight to object storage: file bytes never
	// pass through this process.
	resp, err := s.Attachments.PresignUpload(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) registerAttachment(w http.ResponseWriter, r *http.Request) {
	req := &pdv1.RegisterRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ProductId = idFromPath(r)
	resp, err := s.Attachments.Register(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listAttachments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Attachments.ListAttachments(r.Context(), &pdv1.ListAttachmentsRequest{
		ProductId: idFromPath(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) removeAttachment(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Attachments.Remove(r.Context(), &pdv1.RemoveRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
