package httpapi

import (
	"net/http"
	"strconv"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// Supplier invoices: the third leg of the three-way match, entered by hand
// from the paper the factory sent.

func (s *Server) listSupplierInvoices(w http.ResponseWriter, r *http.Request) {
	supplierID, _ := strconv.ParseInt(r.URL.Query().Get("supplier_id"), 10, 64)
	resp, err := s.Orders.ListSupplierInvoices(r.Context(), &prv1.ListSupplierInvoicesRequest{
		Page:        pageFromQuery(r),
		SupplierId:  supplierID,
		Status:      r.URL.Query().Get("status"),
		MatchStatus: r.URL.Query().Get("match_status"),
		Keyword:     r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSupplierInvoice(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetSupplierInvoice(r.Context(), &prv1.GetSupplierInvoiceRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSupplierInvoice(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSupplierInvoiceRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Orders.CreateSupplierInvoice(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) voidSupplierInvoice(w http.ResponseWriter, r *http.Request) {
	req := &prv1.VoidSupplierInvoiceRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Orders.VoidSupplierInvoice(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) matchSupplierInvoice(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.MatchSupplierInvoice(r.Context(), &prv1.MatchSupplierInvoiceRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// The scan of the paper: presign hands the browser an upload URL, attach
// records the uploaded key. The browser PUTs to object storage directly —
// the file bytes never pass through the gateway.

func (s *Server) presignSupplierInvoiceFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.PresignSupplierInvoiceFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.InvoiceId = idFromPath(r)
	resp, err := s.Orders.PresignSupplierInvoiceFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) attachSupplierInvoiceFile(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AttachSupplierInvoiceFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.InvoiceId = idFromPath(r)
	resp, err := s.Orders.AttachSupplierInvoiceFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
