package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strings"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

type bulkOwnerOption struct {
	ID             int64  `json:"id"`
	EmployeeNo     string `json:"employeeNo"`
	Name           string `json:"name"`
	DepartmentName string `json:"departmentName"`
}

func (s *Server) requireBulkOwnerAdmin(ctx context.Context, module string) error {
	op, ok := grpcx.OperatorFromContext(ctx)
	if !ok || op.EmployeeID <= 0 {
		return apierr.Permission("MD_BULK_OWNER_DENIED", "批量负责人操作需要登录用户")
	}
	scope, err := s.Access.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{EmployeeId: op.EmployeeID, Module: module})
	if err != nil {
		return err
	}
	if !scope.GetAll() {
		return apierr.Permission("MD_BULK_OWNER_DENIED", "只有最高权限用户可以批量管理负责人")
	}
	return nil
}

func (s *Server) listBulkOwnerOptions(ctx context.Context, allowedRoleCodes map[string]bool) ([]bulkOwnerOption, error) {
	roles, err := s.Access.ListAllRoles(ctx, &iamv1.ListAllRolesRequest{})
	if err != nil {
		return nil, err
	}
	roleIDs := make([]int64, 0, len(allowedRoleCodes))
	for _, role := range roles.GetRoles() {
		if role.GetStatus() == "ACTIVE" && allowedRoleCodes[strings.ToUpper(role.GetCode())] {
			roleIDs = append(roleIDs, role.GetId())
		}
	}
	seen := map[int64]bool{}
	options := make([]bulkOwnerOption, 0)
	for _, roleID := range roleIDs {
		for page := int32(1); ; page++ {
			resp, err := s.Directory.ListEmployees(ctx, &iamv1.ListEmployeesRequest{
				Page: &commonv1.PageRequest{Page: page, PageSize: 200}, RoleId: roleID, EmploymentStatus: "ACTIVE",
			})
			if err != nil {
				return nil, err
			}
			for _, employee := range resp.GetEmployees() {
				if employee.GetStatus() != "ACTIVE" || seen[employee.GetId()] {
					continue
				}
				seen[employee.GetId()] = true
				options = append(options, bulkOwnerOption{ID: employee.GetId(), EmployeeNo: employee.GetCode(), Name: employee.GetName(), DepartmentName: employee.GetDepartmentName()})
			}
			if resp.GetMeta() == nil || int64(page)*int64(resp.GetMeta().GetPageSize()) >= resp.GetMeta().GetTotal() || len(resp.GetEmployees()) == 0 {
				break
			}
		}
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Name == options[j].Name {
			return options[i].EmployeeNo < options[j].EmployeeNo
		}
		return options[i].Name < options[j].Name
	})
	return options, nil
}

func customerOwnerRoleCodes() map[string]bool {
	return map[string]bool{"SALES": true, "SALES_MANAGER": true}
}

func supplierOwnerRoleCodes() map[string]bool {
	return map[string]bool{"BUYER": true, "PROCUREMENT_MANAGER": true}
}

func (s *Server) customerOwnerOptions(w http.ResponseWriter, r *http.Request) {
	if err := s.requireBulkOwnerAdmin(r.Context(), "customer"); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	options, err := s.listBulkOwnerOptions(r.Context(), customerOwnerRoleCodes())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeJSON(w, map[string]any{"employees": options})
}

func (s *Server) supplierOwnerOptions(w http.ResponseWriter, r *http.Request) {
	if err := s.requireBulkOwnerAdmin(r.Context(), "supplier"); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	options, err := s.listBulkOwnerOptions(r.Context(), supplierOwnerRoleCodes())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeJSON(w, map[string]any{"employees": options})
}

func resolvedBulkOwners(options []bulkOwnerOption, requested []*mdv1.BulkOwnerAssignment) ([]*mdv1.BulkOwnerAssignment, error) {
	available := make(map[int64]bulkOwnerOption, len(options))
	for _, option := range options {
		available[option.ID] = option
	}
	seen := map[int64]bool{}
	owners := make([]*mdv1.BulkOwnerAssignment, 0, len(requested))
	for _, requestedOwner := range requested {
		option, ok := available[requestedOwner.GetEmployeeId()]
		if !ok {
			return nil, apierr.Invalid("MD_BULK_OWNER_EMPLOYEE_INVALID", "所选负责人不是当前公司的有效业务人员")
		}
		if seen[option.ID] {
			continue
		}
		seen[option.ID] = true
		owners = append(owners, &mdv1.BulkOwnerAssignment{EmployeeId: option.ID, EmployeeName: option.Name})
	}
	return owners, nil
}

func (s *Server) batchUpdateCustomerOwners(w http.ResponseWriter, r *http.Request) {
	if err := s.requireBulkOwnerAdmin(r.Context(), "customer"); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req := &mdv1.BatchUpdateCustomerOwnersRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	options, err := s.listBulkOwnerOptions(r.Context(), customerOwnerRoleCodes())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.Owners, err = resolvedBulkOwners(options, req.GetOwners())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Customers.BatchUpdateCustomerOwners(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) batchUpdateSupplierOwners(w http.ResponseWriter, r *http.Request) {
	if err := s.requireBulkOwnerAdmin(r.Context(), "supplier"); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req := &mdv1.BatchUpdateSupplierOwnersRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	options, err := s.listBulkOwnerOptions(r.Context(), supplierOwnerRoleCodes())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.Owners, err = resolvedBulkOwners(options, req.GetOwners())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Suppliers.BatchUpdateSupplierOwners(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
