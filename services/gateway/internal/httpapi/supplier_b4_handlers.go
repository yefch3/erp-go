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

func (s *Server) checkFactoryDuplicates(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.CheckFactoryDuplicates(r.Context(), &mdv1.CheckFactoryDuplicatesRequest{
		SupplierId: int64FromQuery(r, "supplier_id"), Name: r.URL.Query().Get("name"), Address: r.URL.Query().Get("address"), ExcludeId: int64FromQuery(r, "exclude_id"),
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

func (s *Server) getFactoryDeactivationImpact(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.GetFactoryDeactivationImpact(r.Context(), &mdv1.GetFactoryDeactivationImpactRequest{Id: idFromPath(r)})
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

func (s *Server) listFactories(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListFactories(r.Context(), &mdv1.ListFactoriesRequest{Page: pageFromQuery(r), Keyword: r.URL.Query().Get("keyword"), Status: r.URL.Query().Get("status"), CountryCode: r.URL.Query().Get("country_code"), City: r.URL.Query().Get("city"), SupplierId: int64FromQuery(r, "supplier_id"), OwnerId: int64FromQuery(r, "owner_id"), ProductCategory: r.URL.Query().Get("product_category")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) importFactories(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.ImportFactoriesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Suppliers.ImportFactories(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listFactoryCountries(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListFactoryCountries(r.Context(), &mdv1.ListFactoryCountriesRequest{IncludeInactive: boolFromQuery(r, "include_inactive")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) getFactory(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.GetFactory(r.Context(), &mdv1.GetFactoryRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createFactory(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateFactoryRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Suppliers.CreateFactory(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateFactory(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateFactoryRequest{Id: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Suppliers.UpdateFactory(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listFactoryContacts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListFactoryContacts(r.Context(), &mdv1.ListFactoryContactsRequest{FactoryId: idFromPath(r), IncludeInactive: boolFromQuery(r, "include_inactive")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createFactoryContact(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateFactoryContactRequest{FactoryId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryId = idFromPath(r)
	resp, err := s.Suppliers.CreateFactoryContact(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateFactoryContact(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateFactoryContactRequest{FactoryId: idFromPath(r), Id: relationID(r, "contactId")}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryId, req.Id = idFromPath(r), relationID(r, "contactId")
	resp, err := s.Suppliers.UpdateFactoryContact(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deactivateFactoryContact(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.DeactivateFactoryContact(r.Context(), &mdv1.DeactivateFactoryContactRequest{FactoryId: idFromPath(r), Id: relationID(r, "contactId")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listFactoryOwners(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListFactoryOwners(r.Context(), &mdv1.ListFactoryOwnersRequest{FactoryId: idFromPath(r), IncludeInactive: boolFromQuery(r, "include_inactive")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createFactoryOwner(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateFactoryOwnerRequest{FactoryId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryId = idFromPath(r)
	resp, err := s.Suppliers.CreateFactoryOwner(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateFactoryOwner(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateFactoryOwnerRequest{FactoryId: idFromPath(r), Id: relationID(r, "ownerId")}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryId, req.Id = idFromPath(r), relationID(r, "ownerId")
	resp, err := s.Suppliers.UpdateFactoryOwner(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deactivateFactoryOwner(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.DeactivateFactoryOwner(r.Context(), &mdv1.DeactivateFactoryOwnerRequest{FactoryId: idFromPath(r), Id: relationID(r, "ownerId")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listFactoryCapabilities(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListFactoryCapabilities(r.Context(), &mdv1.ListFactoryCapabilitiesRequest{FactoryId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createFactoryCapability(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateFactoryCapabilityRequest{FactoryId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryId = idFromPath(r)
	resp, err := s.Suppliers.CreateFactoryCapability(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deleteFactoryCapability(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.DeleteFactoryCapability(r.Context(), &mdv1.DeleteFactoryCapabilityRequest{FactoryId: idFromPath(r), Id: relationID(r, "capabilityId")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listFactoryCertificates(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListFactoryCertificates(r.Context(), &mdv1.ListFactoryCertificatesRequest{FactoryId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createFactoryCertificate(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateFactoryCertificateRequest{FactoryId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryId = idFromPath(r)
	resp, err := s.Suppliers.CreateFactoryCertificate(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deleteFactoryCertificate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.DeleteFactoryCertificate(r.Context(), &mdv1.DeleteFactoryCertificateRequest{FactoryId: idFromPath(r), Id: relationID(r, "certificateId")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// The certificate scan uploads straight from the browser to object storage;
// the gateway only brokers the short-lived URL.
func (s *Server) presignFactoryCertificateFile(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.PresignFactoryCertificateFileRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FactoryId = idFromPath(r)
	resp, err := s.Suppliers.PresignFactoryCertificateFile(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listFactoryChanges(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Suppliers.ListFactoryChanges(r.Context(), &mdv1.ListFactoryChangesRequest{FactoryId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
