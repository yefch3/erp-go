package httpapi

import (
	"net/http"
	"strconv"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
)

func boolFromQuery(r *http.Request, key string) bool {
	value, _ := strconv.ParseBool(r.URL.Query().Get(key))
	return value
}

func (s *Server) checkSupplierDuplicates(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.CheckSupplierDuplicates(r.Context(), &mdv1.CheckSupplierDuplicatesRequest{
		Name: r.URL.Query().Get("name"), TaxId: r.URL.Query().Get("tax_id"), Email: r.URL.Query().Get("email"), ExcludeId: int64FromQuery(r, "exclude_id"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSupplier(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.GetSupplier(r.Context(), &mdv1.GetSupplierRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateSupplier(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateSupplierRequest{Id: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Suppliers.UpdateSupplier(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deactivateSupplier(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.DeactivateSupplierRequest{Id: idFromPath(r), Reason: r.URL.Query().Get("reason")}
	if r.ContentLength > 0 && !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Suppliers.DeactivateSupplier(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) activateSupplier(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.ActivateSupplierRequest{Id: idFromPath(r), Reason: r.URL.Query().Get("reason")}
	if r.ContentLength > 0 && !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Suppliers.ActivateSupplier(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getSupplierDeactivationImpact(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.GetSupplierDeactivationImpact(r.Context(), &mdv1.GetSupplierDeactivationImpactRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSupplierCountries(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSupplierCountries(r.Context(), &mdv1.ListSupplierCountriesRequest{IncludeInactive: boolFromQuery(r, "include_inactive")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) importSuppliers(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.ImportSuppliersRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Suppliers.ImportSuppliers(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSupplierContacts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSupplierContacts(r.Context(), &mdv1.ListSupplierContactsRequest{SupplierId: idFromPath(r), IncludeInactive: boolFromQuery(r, "include_inactive")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createSupplierContact(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateSupplierContactRequest{SupplierId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.SupplierId = idFromPath(r)
	resp, err := s.Suppliers.CreateSupplierContact(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateSupplierContact(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateSupplierContactRequest{SupplierId: idFromPath(r), Id: relationID(r, "contactId")}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.SupplierId, req.Id = idFromPath(r), relationID(r, "contactId")
	resp, err := s.Suppliers.UpdateSupplierContact(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deactivateSupplierContact(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.DeactivateSupplierContact(r.Context(), &mdv1.DeactivateSupplierContactRequest{SupplierId: idFromPath(r), Id: relationID(r, "contactId")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSupplierOwners(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSupplierOwners(r.Context(), &mdv1.ListSupplierOwnersRequest{SupplierId: idFromPath(r), IncludeInactive: boolFromQuery(r, "include_inactive")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createSupplierOwner(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateSupplierOwnerRequest{SupplierId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.SupplierId = idFromPath(r)
	resp, err := s.Suppliers.CreateSupplierOwner(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateSupplierOwner(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateSupplierOwnerRequest{SupplierId: idFromPath(r), Id: relationID(r, "ownerId")}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.SupplierId, req.Id = idFromPath(r), relationID(r, "ownerId")
	resp, err := s.Suppliers.UpdateSupplierOwner(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deactivateSupplierOwner(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.DeactivateSupplierOwner(r.Context(), &mdv1.DeactivateSupplierOwnerRequest{SupplierId: idFromPath(r), Id: relationID(r, "ownerId")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listSupplierChanges(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListSupplierChanges(r.Context(), &mdv1.ListSupplierChangesRequest{SupplierId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
