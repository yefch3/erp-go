// Package grpcin adapts gRPC requests onto the app layer: proto <-> app
// conversion only, no business logic.
package grpcin

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type Handler struct {
	mdv1.UnimplementedCustomerServiceServer
	mdv1.UnimplementedSupplierServiceServer
	mdv1.UnimplementedOptionServiceServer
	mdv1.UnimplementedNumberingServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func operatorID(ctx context.Context) int64 {
	op, _ := grpcx.OperatorFromContext(ctx)
	return op.EmployeeID
}

// ---------------------------------------------------------------- customers

func customerInput(name, country, address, currency, term, remark string, contacts []*mdv1.Contact, opID int64) app.CustomerInput {
	in := app.CustomerInput{
		Name: name, Country: country, Address: address, Currency: currency,
		PaymentTerm: term, Remark: remark, OperatorID: opID,
	}
	for _, c := range contacts {
		in.Contacts = append(in.Contacts, app.ContactInput{
			Name: c.GetName(), Title: c.GetTitle(), Email: c.GetEmail(),
			Phone: c.GetPhone(), IsPrimary: c.GetIsPrimary(),
		})
	}
	return in
}

func customerToProto(c store.Customer, contacts []store.CustomerContact) *mdv1.Customer {
	out := &mdv1.Customer{
		Id: c.ID, Code: c.Code, Name: c.Name, Country: c.Country, Address: c.Address,
		Currency: c.Currency, PaymentTerm: c.PaymentTerm, Remark: c.Remark, Status: c.Status,
	}
	for _, ct := range contacts {
		out.Contacts = append(out.Contacts, &mdv1.Contact{
			Name: ct.Name, Title: ct.Title, Email: ct.Email, Phone: ct.Phone, IsPrimary: ct.IsPrimary,
		})
	}
	return out
}

func (h *Handler) CreateCustomer(ctx context.Context, req *mdv1.CreateCustomerRequest) (*mdv1.CreateCustomerResponse, error) {
	in := customerInput(req.GetName(), req.GetCountry(), req.GetAddress(), req.GetCurrency(),
		req.GetPaymentTerm(), req.GetRemark(), req.GetContacts(), operatorID(ctx))
	in.Code = req.GetCode()
	c, contacts, err := h.svc.CreateCustomer(ctx, grpcx.TenantID(ctx), in)
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateCustomerResponse{Customer: customerToProto(c, contacts)}, nil
}

func (h *Handler) GetCustomer(ctx context.Context, req *mdv1.GetCustomerRequest) (*mdv1.GetCustomerResponse, error) {
	c, contacts, err := h.svc.GetCustomer(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &mdv1.GetCustomerResponse{Customer: customerToProto(c, contacts)}, nil
}

func (h *Handler) ListCustomers(ctx context.Context, req *mdv1.ListCustomersRequest) (*mdv1.ListCustomersResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.ListCustomers(ctx, grpcx.TenantID(ctx), req.GetKeyword(), req.GetStatus(), page, size)
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.Customer, len(rows))
	for i, r := range rows {
		out[i] = &mdv1.Customer{
			Id: r.ID, Code: r.Code, Name: r.Name, Country: r.Country, Address: r.Address,
			Currency: r.Currency, PaymentTerm: r.PaymentTerm, Remark: r.Remark, Status: r.Status,
		}
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	return &mdv1.ListCustomersResponse{
		Customers: out,
		Meta:      &commonv1.PageMeta{Total: total, Page: page, PageSize: size},
	}, nil
}

func (h *Handler) UpdateCustomer(ctx context.Context, req *mdv1.UpdateCustomerRequest) (*mdv1.UpdateCustomerResponse, error) {
	in := customerInput(req.GetName(), req.GetCountry(), req.GetAddress(), req.GetCurrency(),
		req.GetPaymentTerm(), req.GetRemark(), req.GetContacts(), operatorID(ctx))
	c, contacts, err := h.svc.UpdateCustomer(ctx, grpcx.TenantID(ctx), req.GetId(), in)
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateCustomerResponse{Customer: customerToProto(c, contacts)}, nil
}

func (h *Handler) DeactivateCustomer(ctx context.Context, req *mdv1.DeactivateCustomerRequest) (*mdv1.DeactivateCustomerResponse, error) {
	if err := h.svc.DeactivateCustomer(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx)); err != nil {
		return nil, err
	}
	return &mdv1.DeactivateCustomerResponse{}, nil
}

// ---------------------------------------------------------------- suppliers

func supplierToProto(s store.Supplier) *mdv1.Supplier {
	return &mdv1.Supplier{
		Id: s.ID, Code: s.Code, Name: s.Name, Country: s.Country, Address: s.Address,
		Currency: s.Currency, ContactName: s.ContactName, ContactPhone: s.ContactPhone,
		ContactEmail: s.ContactEmail, Remark: s.Remark, Status: s.Status,
	}
}

func (h *Handler) CreateSupplier(ctx context.Context, req *mdv1.CreateSupplierRequest) (*mdv1.CreateSupplierResponse, error) {
	sp, err := h.svc.CreateSupplier(ctx, grpcx.TenantID(ctx), app.SupplierInput{
		Code: req.GetCode(), Name: req.GetName(), Country: req.GetCountry(),
		Address: req.GetAddress(), Currency: req.GetCurrency(),
		ContactName: req.GetContactName(), ContactPhone: req.GetContactPhone(),
		ContactEmail: req.GetContactEmail(), Remark: req.GetRemark(),
		OperatorID: operatorID(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateSupplierResponse{Supplier: supplierToProto(sp)}, nil
}

func (h *Handler) GetSupplier(ctx context.Context, req *mdv1.GetSupplierRequest) (*mdv1.GetSupplierResponse, error) {
	sp, err := h.svc.GetSupplier(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &mdv1.GetSupplierResponse{Supplier: supplierToProto(sp)}, nil
}

func (h *Handler) ListSuppliers(ctx context.Context, req *mdv1.ListSuppliersRequest) (*mdv1.ListSuppliersResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.ListSuppliers(ctx, grpcx.TenantID(ctx), req.GetKeyword(), req.GetStatus(), page, size)
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.Supplier, len(rows))
	for i, r := range rows {
		out[i] = &mdv1.Supplier{
			Id: r.ID, Code: r.Code, Name: r.Name, Country: r.Country, Address: r.Address,
			Currency: r.Currency, ContactName: r.ContactName, ContactPhone: r.ContactPhone,
			ContactEmail: r.ContactEmail, Remark: r.Remark, Status: r.Status,
		}
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	return &mdv1.ListSuppliersResponse{
		Suppliers: out,
		Meta:      &commonv1.PageMeta{Total: total, Page: page, PageSize: size},
	}, nil
}

func (h *Handler) UpdateSupplier(ctx context.Context, req *mdv1.UpdateSupplierRequest) (*mdv1.UpdateSupplierResponse, error) {
	sp, err := h.svc.UpdateSupplier(ctx, grpcx.TenantID(ctx), req.GetId(), app.SupplierInput{
		Name: req.GetName(), Country: req.GetCountry(), Address: req.GetAddress(),
		Currency: req.GetCurrency(), ContactName: req.GetContactName(),
		ContactPhone: req.GetContactPhone(), ContactEmail: req.GetContactEmail(),
		Remark: req.GetRemark(), OperatorID: operatorID(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateSupplierResponse{Supplier: supplierToProto(sp)}, nil
}

func (h *Handler) DeactivateSupplier(ctx context.Context, req *mdv1.DeactivateSupplierRequest) (*mdv1.DeactivateSupplierResponse, error) {
	if err := h.svc.DeactivateSupplier(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx)); err != nil {
		return nil, err
	}
	return &mdv1.DeactivateSupplierResponse{}, nil
}

// ---------------------------------------------------------------- options

func (h *Handler) ListOptions(ctx context.Context, req *mdv1.ListOptionsRequest) (*mdv1.ListOptionsResponse, error) {
	items, err := h.svc.ListOptions(ctx, grpcx.TenantID(ctx), req.GetCategory())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.OptionItem, len(items))
	for i, o := range items {
		out[i] = &mdv1.OptionItem{
			Id: o.ID, Category: o.Category, Code: o.Code, Label: o.Label,
			SortOrder: o.SortOrder, Status: o.Status,
		}
	}
	return &mdv1.ListOptionsResponse{Options: out}, nil
}

func (h *Handler) CreateOption(ctx context.Context, req *mdv1.CreateOptionRequest) (*mdv1.CreateOptionResponse, error) {
	o, err := h.svc.CreateOption(ctx, grpcx.TenantID(ctx),
		req.GetCategory(), req.GetCode(), req.GetLabel(), req.GetSortOrder())
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateOptionResponse{Option: &mdv1.OptionItem{
		Id: o.ID, Category: o.Category, Code: o.Code, Label: o.Label,
		SortOrder: o.SortOrder, Status: o.Status,
	}}, nil
}

// ---------------------------------------------------------------- numbering

func (h *Handler) NextNumber(ctx context.Context, req *mdv1.NextNumberRequest) (*mdv1.NextNumberResponse, error) {
	num, err := h.svc.NextNumber(ctx, grpcx.TenantID(ctx), req.GetBizType())
	if err != nil {
		return nil, err
	}
	return &mdv1.NextNumberResponse{Number: num}, nil
}

func (h *Handler) ListNumberRules(ctx context.Context, _ *mdv1.ListNumberRulesRequest) (*mdv1.ListNumberRulesResponse, error) {
	rules, err := h.svc.ListNumberRules(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.NumberRule, len(rules))
	for i, r := range rules {
		out[i] = &mdv1.NumberRule{
			Id: r.ID, BizType: r.BizType, Prefix: r.Prefix, Period: r.Period, SeqLen: r.SeqLen,
		}
	}
	return &mdv1.ListNumberRulesResponse{Rules: out}, nil
}

func (h *Handler) ActivateCustomer(ctx context.Context, req *mdv1.ActivateCustomerRequest) (*mdv1.ActivateCustomerResponse, error) {
	if err := h.svc.ActivateCustomer(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx)); err != nil {
		return nil, err
	}
	return &mdv1.ActivateCustomerResponse{}, nil
}

func (h *Handler) ActivateSupplier(ctx context.Context, req *mdv1.ActivateSupplierRequest) (*mdv1.ActivateSupplierResponse, error) {
	if err := h.svc.ActivateSupplier(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx)); err != nil {
		return nil, err
	}
	return &mdv1.ActivateSupplierResponse{}, nil
}

func (h *Handler) ListMailingContacts(ctx context.Context, req *mdv1.ListMailingContactsRequest) (*mdv1.ListMailingContactsResponse, error) {
	rows, err := h.svc.ListMailingContacts(ctx, grpcx.TenantID(ctx), req.GetKeyword(), req.GetCustomerIds())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.MailingContact, 0, len(rows))
	for _, r := range rows {
		out = append(out, &mdv1.MailingContact{
			ContactId: r.ContactID, Name: r.Name, Title: r.Title, Email: r.Email,
			IsPrimary: r.IsPrimary, CustomerId: r.CustomerID,
			CustomerName: r.CustomerName, Country: r.Country,
		})
	}
	return &mdv1.ListMailingContactsResponse{Contacts: out}, nil
}
