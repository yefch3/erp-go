package httpapi

import (
	"context"

	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
)

// resolveActiveCustomer 校验客户仍可用于新业务，并返回权威主数据供调用方保存名称快照。
func (s *Server) resolveActiveCustomer(ctx context.Context, id int64) (*mdv1.Customer, error) {
	if id <= 0 {
		return nil, apierr.Invalid("MASTERDATA_CUSTOMER_REQUIRED", "请选择有效客户")
	}
	resp, err := s.Customers.GetCustomer(ctx, &mdv1.GetCustomerRequest{Id: id})
	if err != nil {
		return nil, err
	}
	customer := resp.GetCustomer()
	if customer.GetStatus() != "ACTIVE" {
		return nil, apierr.Conflict("MASTERDATA_CUSTOMER_INACTIVE", "已停用客户不能用于新业务")
	}
	return customer, nil
}

// resolveActiveCustomerContact 校验联系人属于所选客户，并返回主数据快照。
// 上传入口不能相信浏览器传来的姓名或邮箱，否则可以把甲客户的联系人挂到乙客户询盘上。
func (s *Server) resolveActiveCustomerContact(ctx context.Context, customerID, contactID int64) (*mdv1.Customer, *mdv1.Contact, error) {
	customer, err := s.resolveActiveCustomer(ctx, customerID)
	if err != nil {
		return nil, nil, err
	}
	if contactID <= 0 {
		return nil, nil, apierr.Invalid("MASTERDATA_CUSTOMER_CONTACT_REQUIRED", "请选择有效客户联系人")
	}
	resp, err := s.Customers.ListCustomerContacts(ctx, &mdv1.ListCustomerContactsRequest{CustomerId: customerID})
	if err != nil {
		return nil, nil, err
	}
	for _, contact := range resp.GetContacts() {
		if contact.GetId() != contactID || contact.GetStatus() != "ACTIVE" {
			continue
		}
		if contact.GetEmail() == "" {
			return nil, nil, apierr.Invalid("MASTERDATA_CUSTOMER_CONTACT_EMAIL_REQUIRED", "所选客户联系人尚未维护邮箱")
		}
		return customer, contact, nil
	}
	return nil, nil, apierr.Invalid("MASTERDATA_CUSTOMER_CONTACT_INVALID", "所选联系人不属于该客户或已停用")
}

// resolveActiveSupplier 校验供应商仍可用于新业务，并返回权威主数据供调用方保存名称快照。
func (s *Server) resolveActiveSupplier(ctx context.Context, id int64) (*mdv1.Supplier, error) {
	if id <= 0 {
		return nil, apierr.Invalid("MASTERDATA_SUPPLIER_REQUIRED", "请选择有效供应商")
	}
	resp, err := s.Suppliers.GetSupplier(ctx, &mdv1.GetSupplierRequest{Id: id})
	if err != nil {
		return nil, err
	}
	supplier := resp.GetSupplier()
	if supplier.GetStatus() != "ACTIVE" {
		return nil, apierr.Conflict("MASTERDATA_SUPPLIER_INACTIVE", "已停用供应商不能用于新业务")
	}
	return supplier, nil
}

// supplierHasRole reports whether the supplier carries any of the given
// business types. Roles live in master data and nowhere else.
func supplierHasRole(supplier *mdv1.Supplier, roles ...string) bool {
	for _, have := range supplier.GetBusinessTypes() {
		for _, want := range roles {
			if have == want {
				return true
			}
		}
	}
	return false
}
