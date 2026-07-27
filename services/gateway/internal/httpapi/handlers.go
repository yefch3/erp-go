package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

// ---------------------------------------------------------------- auth

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	req := &iamv1.LoginRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.IAM.Login(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- customers

func pageFromQuery(r *http.Request) *commonv1.PageRequest {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	return &commonv1.PageRequest{Page: int32(page), PageSize: int32(size)}
}

func idFromPath(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id
}

func (s *Server) listCustomers(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomers(r.Context(), &mdv1.ListCustomersRequest{
		Page:    pageFromQuery(r),
		Keyword: r.URL.Query().Get("keyword"),
		Status:  r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createCustomer(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateCustomerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Customers.CreateCustomer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getCustomer(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.GetCustomer(r.Context(), &mdv1.GetCustomerRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateCustomer(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateCustomerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Customers.UpdateCustomer(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateCustomer(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.DeactivateCustomer(r.Context(), &mdv1.DeactivateCustomerRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- suppliers

func (s *Server) listSuppliers(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSuppliers(r.Context(), &mdv1.ListSuppliersRequest{
		Page:    pageFromQuery(r),
		Keyword: r.URL.Query().Get("keyword"),
		Status:  r.URL.Query().Get("status"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSupplier(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateSupplierRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Suppliers.CreateSupplier(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// ---------------------------------------------------------------- options / numbering

func (s *Server) listOptions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Options.ListOptions(r.Context(), &mdv1.ListOptionsRequest{
		Category: r.URL.Query().Get("category"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) nextNumber(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.NextNumberRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Numbering.NextNumber(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) activateCustomer(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ActivateCustomer(r.Context(), &mdv1.ActivateCustomerRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
