// Package grpcin adapts gRPC requests onto the app layer: proto <-> app
// conversion only, no business logic.
package grpcin

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type Handler struct {
	mdv1.UnimplementedCustomerServiceServer
	mdv1.UnimplementedSupplierServiceServer
	mdv1.UnimplementedPortServiceServer
	mdv1.UnimplementedOptionServiceServer
	mdv1.UnimplementedNumberingServiceServer
	mdv1.UnimplementedCreditRatingServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func operatorID(ctx context.Context) int64 {
	op, _ := grpcx.OperatorFromContext(ctx)
	return op.EmployeeID
}

func operatorName(ctx context.Context) string {
	op, _ := grpcx.OperatorFromContext(ctx)
	return op.Name
}

// ---------------------------------------------------------------- customers

func customerInput(name, country, countryCode, address, currency, term, remark string, contacts []*mdv1.Contact, opID int64) app.CustomerInput {
	in := app.CustomerInput{
		Name: name, Country: country, CountryCode: countryCode,
		Address: address, Currency: currency,
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

func applyCustomerProfile(in *app.CustomerInput,
	shortName, englishName, customerType, industry, source string, tags []string,
	website, primaryLanguage, timezone, registeredName, registrationNo, taxID string,
	invoiceTitle, invoiceTaxNo, invoiceRemark string, paymentDays int32,
	creditLimitMinor int64, creditCurrency, creditStatus, businessStatus string,
) {
	in.ShortName, in.EnglishName = shortName, englishName
	in.CustomerType, in.Industry, in.Source, in.Tags = customerType, industry, source, tags
	in.Website, in.PrimaryLanguage, in.Timezone = website, primaryLanguage, timezone
	in.RegisteredName, in.RegistrationNo, in.TaxID = registeredName, registrationNo, taxID
	in.InvoiceTitle, in.InvoiceTaxNo, in.InvoiceRemark = invoiceTitle, invoiceTaxNo, invoiceRemark
	in.PaymentDays, in.CreditLimitMinor = paymentDays, creditLimitMinor
	in.CreditCurrency, in.CreditStatus, in.BusinessStatus = creditCurrency, creditStatus, businessStatus
}

func customerProfileInput(req *mdv1.UpdateCustomerProfileRequest, opID int64) app.CustomerProfileInput {
	return app.CustomerProfileInput{
		ShortName: req.GetShortName(), EnglishName: req.GetEnglishName(),
		CustomerType: req.GetCustomerType(), Industry: req.GetIndustry(), Source: req.GetSource(),
		Tags: req.GetTags(), Website: req.GetWebsite(), PrimaryLanguage: req.GetPrimaryLanguage(),
		Timezone: req.GetTimezone(), RegisteredName: req.GetRegisteredName(),
		RegistrationNo: req.GetRegistrationNo(), TaxID: req.GetTaxId(),
		InvoiceTitle: req.GetInvoiceTitle(), InvoiceTaxNo: req.GetInvoiceTaxNo(),
		InvoiceRemark: req.GetInvoiceRemark(), PaymentDays: req.GetPaymentDays(),
		CreditLimitMinor: req.GetCreditLimitMinor(), CreditCurrency: req.GetCreditCurrency(),
		CreditStatus: req.GetCreditStatus(), BusinessStatus: req.GetBusinessStatus(), OperatorID: opID,
	}
}

func addressToProto(a store.CustomerAddress) *mdv1.CustomerAddress {
	return &mdv1.CustomerAddress{
		Id: a.ID, CustomerId: a.CustomerID, AddressType: a.AddressType,
		CountryCode: strings.TrimSpace(a.CountryCode), State: a.State, City: a.City,
		PostalCode: a.PostalCode, AddressLine: a.AddressLine, IsDefault: a.IsDefault,
		SortOrder: a.SortOrder, Status: a.Status,
	}
}

// 时间戳过边界统一成 RFC3339 文本，空值给空串——「没有评过」和「1970 年
// 评的」在页面上必须长得不一样。
func ts(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func customerToProto(c store.Customer, contacts []store.CustomerContact, addresses []store.CustomerAddress) *mdv1.Customer {
	out := &mdv1.Customer{
		Id: c.ID, Code: c.Code, Name: c.Name, Country: c.Country,
		CountryCode: strings.TrimSpace(c.CountryCode), Address: c.Address,
		Currency: c.Currency, PaymentTerm: c.PaymentTerm, Remark: c.Remark, Status: c.Status,
		ShortName: c.ShortName, EnglishName: c.EnglishName, CustomerType: c.CustomerType,
		Industry: c.Industry, Source: c.Source, Tags: c.Tags, Website: c.Website,
		PrimaryLanguage: c.PrimaryLanguage, Timezone: c.Timezone,
		RegisteredName: c.RegisteredName, RegistrationNo: c.RegistrationNo, TaxId: c.TaxID,
		InvoiceTitle: c.InvoiceTitle, InvoiceTaxNo: c.InvoiceTaxNo,
		InvoiceRemark: c.InvoiceRemark, PaymentDays: c.PaymentDays,
		CreditLimitMinor: c.CreditLimitMinor, CreditCurrency: c.CreditCurrency,
		CreditStatus: c.CreditStatus, BusinessStatus: c.BusinessStatus,
		CreditGrade: c.CreditGrade, CreditGradedAt: ts(c.CreditGradedAt),
	}
	for _, ct := range contacts {
		out.Contacts = append(out.Contacts, contactToProto(ct))
	}
	for _, address := range addresses {
		out.Addresses = append(out.Addresses, addressToProto(address))
	}
	return out
}

func (h *Handler) CreateCustomer(ctx context.Context, req *mdv1.CreateCustomerRequest) (*mdv1.CreateCustomerResponse, error) {
	in := customerInput(req.GetName(), req.GetCountry(), req.GetCountryCode(),
		req.GetAddress(), req.GetCurrency(),
		req.GetPaymentTerm(), req.GetRemark(), req.GetContacts(), operatorID(ctx))
	applyCustomerProfile(&in, req.GetShortName(), req.GetEnglishName(), req.GetCustomerType(),
		req.GetIndustry(), req.GetSource(), req.GetTags(), req.GetWebsite(),
		req.GetPrimaryLanguage(), req.GetTimezone(), req.GetRegisteredName(),
		req.GetRegistrationNo(), req.GetTaxId(), req.GetInvoiceTitle(),
		req.GetInvoiceTaxNo(), req.GetInvoiceRemark(), req.GetPaymentDays(),
		req.GetCreditLimitMinor(), req.GetCreditCurrency(), req.GetCreditStatus(),
		req.GetBusinessStatus())
	in.Code = req.GetCode()
	in.OperatorName = operatorName(ctx)
	c, contacts, err := h.svc.CreateCustomer(ctx, grpcx.TenantID(ctx), in)
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateCustomerResponse{Customer: customerToProto(c, contacts, nil)}, nil
}

func (h *Handler) GetCustomer(ctx context.Context, req *mdv1.GetCustomerRequest) (*mdv1.GetCustomerResponse, error) {
	c, contacts, err := h.svc.GetCustomer(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	addresses, err := h.svc.ListCustomerAddresses(ctx, grpcx.TenantID(ctx), req.GetId(), "ALL")
	if err != nil {
		return nil, err
	}
	return &mdv1.GetCustomerResponse{Customer: customerToProto(c, contacts, addresses)}, nil
}

func (h *Handler) ListCustomers(ctx context.Context, req *mdv1.ListCustomersRequest) (*mdv1.ListCustomersResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.ListCustomers(ctx, grpcx.TenantID(ctx), req.GetKeyword(), req.GetStatus(), req.GetCountryCode(),
		req.GetCustomerType(), req.GetBusinessStatus(), req.GetTag(), req.GetOwnerEmployeeId(), page, size)
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.Customer, len(rows))
	for i, r := range rows {
		out[i] = &mdv1.Customer{
			Id: r.ID, Code: r.Code, Name: r.Name, Country: r.Country,
			CountryCode: strings.TrimSpace(r.CountryCode), Address: r.Address,
			Currency: r.Currency, PaymentTerm: r.PaymentTerm, Remark: r.Remark, Status: r.Status,
			ShortName: r.ShortName, EnglishName: r.EnglishName, CustomerType: r.CustomerType,
			Industry: r.Industry, Source: r.Source, Tags: r.Tags, Website: r.Website,
			PrimaryLanguage: r.PrimaryLanguage, Timezone: r.Timezone,
			RegisteredName: r.RegisteredName, RegistrationNo: r.RegistrationNo, TaxId: r.TaxID,
			InvoiceTitle: r.InvoiceTitle, InvoiceTaxNo: r.InvoiceTaxNo,
			InvoiceRemark: r.InvoiceRemark, PaymentDays: r.PaymentDays,
			CreditLimitMinor: r.CreditLimitMinor, CreditCurrency: r.CreditCurrency,
			CreditStatus: r.CreditStatus, BusinessStatus: r.BusinessStatus,
			CreditGrade: r.CreditGrade, CreditGradedAt: ts(r.CreditGradedAt),
			PrimaryContactName: r.PrimaryContactName,
		}
		if r.OwnerNames != "" {
			for _, name := range strings.Split(r.OwnerNames, "、") {
				out[i].Owners = append(out[i].Owners, &mdv1.CustomerOwner{EmployeeName: name, Status: "ACTIVE"})
			}
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
	in := customerInput(req.GetName(), req.GetCountry(), req.GetCountryCode(),
		req.GetAddress(), req.GetCurrency(),
		req.GetPaymentTerm(), req.GetRemark(), req.GetContacts(), operatorID(ctx))
	in.OperatorName = operatorName(ctx)
	c, contacts, err := h.svc.UpdateCustomer(ctx, grpcx.TenantID(ctx), req.GetId(), in)
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateCustomerResponse{Customer: customerToProto(c, contacts, nil)}, nil
}

// UpdateCustomerProfile 单独更新客户详细资料，避免旧版基础信息表单覆盖新字段。
func (h *Handler) UpdateCustomerProfile(ctx context.Context, req *mdv1.UpdateCustomerProfileRequest) (*mdv1.UpdateCustomerProfileResponse, error) {
	in := customerProfileInput(req, operatorID(ctx))
	in.OperatorName = operatorName(ctx)
	c, err := h.svc.UpdateCustomerProfile(ctx, grpcx.TenantID(ctx), req.GetId(), in)
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateCustomerProfileResponse{Customer: customerToProto(c, nil, nil)}, nil
}

func (h *Handler) DeactivateCustomer(ctx context.Context, req *mdv1.DeactivateCustomerRequest) (*mdv1.DeactivateCustomerResponse, error) {
	if err := h.svc.DeactivateCustomer(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx), operatorName(ctx), req.GetReason()); err != nil {
		return nil, err
	}
	return &mdv1.DeactivateCustomerResponse{}, nil
}

func addressInput(in *mdv1.CustomerAddressInput, opID int64) app.CustomerAddressInput {
	if in == nil {
		in = &mdv1.CustomerAddressInput{}
	}
	return app.CustomerAddressInput{
		AddressType: in.GetAddressType(), CountryCode: in.GetCountryCode(),
		State: in.GetState(), City: in.GetCity(), PostalCode: in.GetPostalCode(),
		AddressLine: in.GetAddressLine(), IsDefault: in.GetIsDefault(),
		SortOrder: in.GetSortOrder(), OperatorID: opID,
	}
}

func (h *Handler) ListCustomerAddresses(ctx context.Context, req *mdv1.ListCustomerAddressesRequest) (*mdv1.ListCustomerAddressesResponse, error) {
	rows, err := h.svc.ListCustomerAddresses(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetStatus())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CustomerAddress, len(rows))
	for i, row := range rows {
		out[i] = addressToProto(row)
	}
	return &mdv1.ListCustomerAddressesResponse{Addresses: out}, nil
}

func (h *Handler) CreateCustomerAddress(ctx context.Context, req *mdv1.CreateCustomerAddressRequest) (*mdv1.CreateCustomerAddressResponse, error) {
	row, err := h.svc.CreateCustomerAddress(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), addressInput(req.GetAddress(), operatorID(ctx)))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateCustomerAddressResponse{Address: addressToProto(row)}, nil
}

func (h *Handler) UpdateCustomerAddress(ctx context.Context, req *mdv1.UpdateCustomerAddressRequest) (*mdv1.UpdateCustomerAddressResponse, error) {
	row, err := h.svc.UpdateCustomerAddress(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetId(), addressInput(req.GetAddress(), operatorID(ctx)))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateCustomerAddressResponse{Address: addressToProto(row)}, nil
}

func (h *Handler) DeactivateCustomerAddress(ctx context.Context, req *mdv1.DeactivateCustomerAddressRequest) (*mdv1.DeactivateCustomerAddressResponse, error) {
	if err := h.svc.DeactivateCustomerAddress(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetId(), operatorID(ctx), operatorName(ctx)); err != nil {
		return nil, err
	}
	return &mdv1.DeactivateCustomerAddressResponse{}, nil
}

// ---------------------------------------------------------------- suppliers

func supplierToProto(s store.Supplier) *mdv1.Supplier {
	return &mdv1.Supplier{
		Id: s.ID, Code: s.Code, Name: s.Name, Country: s.Country, Address: s.Address,
		Currency: s.Currency, ContactName: s.ContactName, ContactPhone: s.ContactPhone,
		ContactEmail: s.ContactEmail, Remark: s.Remark, Status: s.Status,
		NameZh: s.NameZh, NameEn: s.NameEn, ShortName: s.ShortName,
		CountryCode: s.CountryCode, TaxId: s.TaxID, RegisteredAddress: s.RegisteredAddress,
		PaymentTerm: s.PaymentTerm, BusinessTypes: s.BusinessTypes,
		CreditGrade: s.CreditGrade, CreditGradedAt: ts(s.CreditGradedAt),
	}
}

func (h *Handler) CreateSupplier(ctx context.Context, req *mdv1.CreateSupplierRequest) (*mdv1.CreateSupplierResponse, error) {
	sp, err := h.svc.CreateSupplier(ctx, grpcx.TenantID(ctx), app.SupplierInput{
		Code: req.GetCode(), Name: req.GetName(), Country: req.GetCountry(),
		Address: req.GetAddress(), Currency: req.GetCurrency(),
		ContactName: req.GetContactName(), ContactPhone: req.GetContactPhone(),
		ContactEmail: req.GetContactEmail(), Remark: req.GetRemark(),
		NameZh: req.GetNameZh(), NameEn: req.GetNameEn(), ShortName: req.GetShortName(),
		CountryCode: req.GetCountryCode(), TaxID: req.GetTaxId(), RegisteredAddress: req.GetRegisteredAddress(),
		PaymentTerm: req.GetPaymentTerm(), BusinessTypes: req.GetBusinessTypes(),
		OperatorID: operatorID(ctx), OperatorName: operatorName(ctx),
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
	rows, total, err := h.svc.ListSuppliers(ctx, grpcx.TenantID(ctx), req.GetKeyword(), req.GetStatus(),
		req.GetCountryCode(), req.GetBusinessType(), req.GetOwnerId(), page, size)
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.Supplier, len(rows))
	for i, r := range rows {
		out[i] = &mdv1.Supplier{
			Id: r.ID, Code: r.Code, Name: r.Name, Country: r.Country, Address: r.Address,
			Currency: r.Currency, ContactName: r.ContactName, ContactPhone: r.ContactPhone,
			ContactEmail: r.ContactEmail, Remark: r.Remark, Status: r.Status,
			NameZh: r.NameZh, NameEn: r.NameEn, ShortName: r.ShortName,
			CountryCode: r.CountryCode, TaxId: r.TaxID, RegisteredAddress: r.RegisteredAddress,
			PaymentTerm: r.PaymentTerm, BusinessTypes: r.BusinessTypes,
			CreditGrade: r.CreditGrade, CreditGradedAt: ts(r.CreditGradedAt),
			FactoryCount: r.FactoryCount, OwnerNames: r.OwnerNames,
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
		Remark: req.GetRemark(), NameZh: req.GetNameZh(), NameEn: req.GetNameEn(),
		ShortName: req.GetShortName(), CountryCode: req.GetCountryCode(), TaxID: req.GetTaxId(),
		RegisteredAddress: req.GetRegisteredAddress(), PaymentTerm: req.GetPaymentTerm(),
		BusinessTypes: req.GetBusinessTypes(), OperatorID: operatorID(ctx), OperatorName: operatorName(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateSupplierResponse{Supplier: supplierToProto(sp)}, nil
}

func (h *Handler) DeactivateSupplier(ctx context.Context, req *mdv1.DeactivateSupplierRequest) (*mdv1.DeactivateSupplierResponse, error) {
	if err := h.svc.DeactivateSupplier(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx), operatorName(ctx), req.GetReason()); err != nil {
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
	if err := h.svc.ActivateCustomer(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx), operatorName(ctx), req.GetReason()); err != nil {
		return nil, err
	}
	return &mdv1.ActivateCustomerResponse{}, nil
}

func impactItems(rows []app.DeactivationImpactItem) ([]*mdv1.DeactivationImpactItem, int64) {
	out := make([]*mdv1.DeactivationImpactItem, len(rows))
	var total int64
	for i, row := range rows {
		out[i] = &mdv1.DeactivationImpactItem{Code: row.Code, Label: row.Label, Count: row.Count}
		total += row.Count
	}
	return out, total
}

func (h *Handler) GetCustomerDeactivationImpact(ctx context.Context, req *mdv1.GetCustomerDeactivationImpactRequest) (*mdv1.GetCustomerDeactivationImpactResponse, error) {
	rows, err := h.svc.DeactivationImpact(ctx, grpcx.TenantID(ctx), "CUSTOMER", req.GetId())
	if err != nil {
		return nil, err
	}
	items, total := impactItems(rows)
	return &mdv1.GetCustomerDeactivationImpactResponse{Items: items, Total: total}, nil
}

func (h *Handler) ActivateSupplier(ctx context.Context, req *mdv1.ActivateSupplierRequest) (*mdv1.ActivateSupplierResponse, error) {
	if err := h.svc.ActivateSupplier(ctx, grpcx.TenantID(ctx), req.GetId(), operatorID(ctx), operatorName(ctx), req.GetReason()); err != nil {
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
			CountryCode: strings.TrimSpace(r.CountryCode), Language: r.Language,
			EmailCategories: r.EmailCategories,
		})
	}
	return &mdv1.ListMailingContactsResponse{Contacts: out}, nil
}

func (h *Handler) ListCustomerCountries(ctx context.Context, req *mdv1.ListCustomerCountriesRequest) (*mdv1.ListCustomerCountriesResponse, error) {
	groups, err := h.svc.ListCustomerCountries(ctx, grpcx.TenantID(ctx), req.GetStatus())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CountryGroup, len(groups))
	for i, g := range groups {
		out[i] = &mdv1.CountryGroup{
			Code: g.Code, CustomerCount: g.CustomerCount,
			ContactCount: g.ContactCount, OneEachCount: g.OneEachCount,
		}
	}
	return &mdv1.ListCustomerCountriesResponse{Countries: out}, nil
}

func (h *Handler) ContactsInCountry(ctx context.Context, req *mdv1.ContactsInCountryRequest) (*mdv1.ContactsInCountryResponse, error) {
	rows, err := h.svc.ContactsInCountry(ctx, grpcx.TenantID(ctx),
		req.GetCountryCode(), req.GetPrimaryOnly())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.MailingContact, len(rows))
	for i, r := range rows {
		out[i] = &mdv1.MailingContact{
			ContactId: r.ContactID, Name: r.Name, Title: r.Title, Email: r.Email,
			IsPrimary: r.IsPrimary, CustomerId: r.CustomerID,
			CustomerName: r.CustomerName, Country: r.Country, CountryCode: r.CountryCode,
			Language: r.Language, EmailCategories: r.EmailCategories,
		}
	}
	return &mdv1.ContactsInCountryResponse{Contacts: out}, nil
}
