package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
)

func relationID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id
}

func (s *Server) listCustomerContacts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerContacts(r.Context(), &mdv1.ListCustomerContactsRequest{CustomerId: idFromPath(r), Status: r.URL.Query().Get("status")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createCustomerContact(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateCustomerContactRequest{CustomerId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CustomerId = idFromPath(r)
	resp, err := s.Customers.CreateCustomerContact(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) updateCustomerContact(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateCustomerContactRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CustomerId, req.Id = idFromPath(r), relationID(r, "contactId")
	resp, err := s.Customers.UpdateCustomerContact(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deactivateCustomerContact(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.DeactivateCustomerContact(r.Context(), &mdv1.DeactivateCustomerContactRequest{CustomerId: idFromPath(r), Id: relationID(r, "contactId")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listCustomerOwners(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerOwners(r.Context(), &mdv1.ListCustomerOwnersRequest{CustomerId: idFromPath(r), Status: r.URL.Query().Get("status")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) createCustomerOwner(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.CreateCustomerOwnerRequest{CustomerId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CustomerId = idFromPath(r)
	if req.Owner == nil || req.Owner.EmployeeId == 0 {
		s.writeGRPCError(w, apierr.Invalid("MD_CUSTOMER_OWNER_REQUIRED", "请选择负责人"))
		return
	}
	// 员工状态属于 IAM，网关在跨服务边界校验，并用真实姓名覆盖浏览器传值。
	emp, err := s.Directory.GetEmployee(r.Context(), &iamv1.GetEmployeeRequest{Id: req.Owner.EmployeeId})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if emp.GetEmployee().GetStatus() != "ACTIVE" {
		s.writeGRPCError(w, apierr.Conflict("MD_OWNER_EMPLOYEE_INACTIVE", "只能选择在职员工作为客户负责人"))
		return
	}
	req.Owner.EmployeeName = emp.GetEmployee().GetName()
	resp, err := s.Customers.CreateCustomerOwner(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateCustomerOwner(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.UpdateCustomerOwnerRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CustomerId, req.Id = idFromPath(r), relationID(r, "ownerId")
	resp, err := s.Customers.UpdateCustomerOwner(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) deactivateCustomerOwner(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.DeactivateCustomerOwner(r.Context(), &mdv1.DeactivateCustomerOwnerRequest{CustomerId: idFromPath(r), Id: relationID(r, "ownerId"), EndDate: r.URL.Query().Get("end_date")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listCustomerChanges(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerChanges(r.Context(), &mdv1.ListCustomerChangesRequest{CustomerId: idFromPath(r), Page: pageFromQuery(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) importCustomers(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.ImportCustomersRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// Resolve every supplied owner ID in the authenticated tenant, never trust Excel names.
	owners := map[int64]string{}
	for _, row := range req.GetRows() {
		ids := row.GetOwnerEmployeeIds()
		if len(ids) == 0 && row.GetOwnerEmployeeId() > 0 {
			ids = []int64{row.GetOwnerEmployeeId()}
		}
		row.OwnerEmployeeIds = ids
		row.OwnerNames = nil
		for _, id := range ids {
			name, found := owners[id]
			if !found {
				employee, err := s.Directory.GetEmployee(r.Context(), &iamv1.GetEmployeeRequest{Id: id})
				if err != nil {
					s.writeGRPCError(w, err)
					return
				}
				if employee.GetEmployee().GetStatus() != "ACTIVE" {
					s.writeGRPCError(w, apierr.Invalid("MD_OWNER_EMPLOYEE_INACTIVE", "只能选择本公司在职员工作为客户负责人"))
					return
				}
				name = employee.GetEmployee().GetName()
				owners[id] = name
			}
			row.OwnerNames = append(row.OwnerNames, name)
		}
		if len(ids) == 1 {
			row.OwnerEmployeeId = ids[0]
			row.OwnerName = row.OwnerNames[0]
		}
	}

	resp, err := s.Customers.ImportCustomers(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listCustomerFields(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.ListCustomerFields(r.Context(), &mdv1.ListCustomerFieldsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) saveCustomerField(w http.ResponseWriter, r *http.Request) {
	req := &mdv1.SaveCustomerFieldRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.FieldKey = chi.URLParam(r, "fieldKey")
	resp, err := s.Customers.SaveCustomerField(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) checkCustomerDuplicates(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Customers.CheckCustomerDuplicates(r.Context(), &mdv1.CheckCustomerDuplicatesRequest{Name: r.URL.Query().Get("name"), TaxId: r.URL.Query().Get("tax_id"), Email: r.URL.Query().Get("email"), ExcludeId: int64FromQuery(r, "exclude_id")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
