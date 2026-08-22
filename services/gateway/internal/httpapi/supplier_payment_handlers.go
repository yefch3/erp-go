package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// Supplier payments: the assertion that money left, and the revisable
// judgements about what it settled.

func (s *Server) listSupplierPayments(w http.ResponseWriter, r *http.Request) {
	supplierID, _ := strconv.ParseInt(r.URL.Query().Get("supplier_id"), 10, 64)
	resp, err := s.Orders.ListSupplierPayments(r.Context(), &prv1.ListSupplierPaymentsRequest{
		Page:        pageFromQuery(r),
		SupplierId:  supplierID,
		PaymentType: r.URL.Query().Get("payment_type"),
		Keyword:     r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSupplierPayment(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetSupplierPayment(r.Context(), &prv1.GetSupplierPaymentRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSupplierPayment(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSupplierPaymentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.CreateSupplierPayment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) allocateSupplierPayment(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AllocateSupplierPaymentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.AllocateSupplierPayment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reverseSupplierPaymentAllocation(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ReverseSupplierPaymentAllocationRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.AllocationId, _ = strconv.ParseInt(chi.URLParam(r, "allocationId"), 10, 64)
	resp, err := s.Orders.ReverseSupplierPaymentAllocation(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
