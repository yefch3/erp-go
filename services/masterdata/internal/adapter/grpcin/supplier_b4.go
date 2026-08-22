package grpcin

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

// CheckSupplierDuplicates 在保存前返回可能重复的供应商，结果仅用于提醒，不替代数据库唯一约束。
func (h *Handler) CheckSupplierDuplicates(ctx context.Context, req *mdv1.CheckSupplierDuplicatesRequest) (*mdv1.CheckSupplierDuplicatesResponse, error) {
	rows, err := h.svc.CheckSupplierDuplicates(ctx, grpcx.TenantID(ctx), strings.TrimSpace(req.GetName()), strings.TrimSpace(req.GetTaxId()), strings.TrimSpace(req.GetEmail()), req.GetExcludeId())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.SupplierDuplicate, len(rows))
	for i, row := range rows {
		out[i] = &mdv1.SupplierDuplicate{Id: row.ID, Code: row.Code, Name: row.Name, TaxId: row.TaxID, Email: row.Email, MatchFields: row.MatchFields}
	}
	return &mdv1.CheckSupplierDuplicatesResponse{Candidates: out}, nil
}

// CheckFactoryDuplicates 在同一供应商范围内检查工厂名称和地址是否相似。
func (h *Handler) CheckFactoryDuplicates(ctx context.Context, req *mdv1.CheckFactoryDuplicatesRequest) (*mdv1.CheckFactoryDuplicatesResponse, error) {
	rows, err := h.svc.CheckFactoryDuplicates(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), strings.TrimSpace(req.GetName()), strings.TrimSpace(req.GetAddress()), req.GetExcludeId())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.FactoryDuplicate, len(rows))
	for i, row := range rows {
		out[i] = &mdv1.FactoryDuplicate{Id: row.ID, Code: row.Code, SupplierId: row.SupplierID, SupplierName: row.SupplierName, Name: row.Name, Address: row.Address, MatchFields: row.MatchFields}
	}
	return &mdv1.CheckFactoryDuplicatesResponse{Candidates: out}, nil
}

func (h *Handler) GetSupplierDeactivationImpact(ctx context.Context, req *mdv1.GetSupplierDeactivationImpactRequest) (*mdv1.GetSupplierDeactivationImpactResponse, error) {
	rows, err := h.svc.DeactivationImpact(ctx, grpcx.TenantID(ctx), "SUPPLIER", req.GetId())
	if err != nil {
		return nil, err
	}
	items, total := impactItems(rows)
	return &mdv1.GetSupplierDeactivationImpactResponse{Items: items, Total: total}, nil
}

func (h *Handler) GetFactoryDeactivationImpact(ctx context.Context, req *mdv1.GetFactoryDeactivationImpactRequest) (*mdv1.GetFactoryDeactivationImpactResponse, error) {
	rows, err := h.svc.DeactivationImpact(ctx, grpcx.TenantID(ctx), "FACTORY", req.GetId())
	if err != nil {
		return nil, err
	}
	items, total := impactItems(rows)
	return &mdv1.GetFactoryDeactivationImpactResponse{Items: items, Total: total}, nil
}

func dateText(v pgtype.Date) string {
	if !v.Valid {
		return ""
	}
	return v.Time.Format("2006-01-02")
}

func timeText(v pgtype.Timestamptz) string {
	if !v.Valid {
		return ""
	}
	return v.Time.Format("2006-01-02T15:04:05Z07:00")
}

func numericText(v pgtype.Numeric) string {
	if !v.Valid {
		return ""
	}
	value, err := v.Value()
	if err != nil || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func partyContactInput(name, department, title, phone, email string, primary bool, remark string, ctx context.Context) app.PartyContactInput {
	return app.PartyContactInput{Name: name, Department: department, Title: title, Phone: phone, Email: email,
		IsPrimary: primary, Remark: remark, OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)}
}

func partyOwnerInput(employeeID int64, employeeName, responsibility, start, end string, primary bool, ctx context.Context) app.PartyOwnerInput {
	return app.PartyOwnerInput{EmployeeID: employeeID, EmployeeName: employeeName, ResponsibilityCode: responsibility,
		StartDate: start, EndDate: end, IsPrimary: primary, OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)}
}

func supplierContactToProto(v store.SupplierContact) *mdv1.SupplierContact {
	return &mdv1.SupplierContact{Id: v.ID, SupplierId: v.SupplierID, Name: v.Name, Department: v.Department,
		Title: v.Title, Phone: v.Phone, Email: v.Email, IsPrimary: v.IsPrimary, Status: v.Status, Remark: v.Remark}
}

func supplierOwnerToProto(v store.SupplierOwner) *mdv1.SupplierOwner {
	return &mdv1.SupplierOwner{Id: v.ID, SupplierId: v.SupplierID, EmployeeId: v.EmployeeID, EmployeeName: v.EmployeeName,
		ResponsibilityCode: v.ResponsibilityCode, IsPrimary: v.IsPrimary, StartDate: dateText(v.StartDate),
		EndDate: dateText(v.EndDate), Status: v.Status}
}

func changeToProto(id int64, action, section, summary string, before, after []byte, operatorID int64, operatorName, createdAt string) *mdv1.MasterDataChange {
	return &mdv1.MasterDataChange{Id: id, Action: action, Section: section, Summary: summary,
		BeforeJson: string(before), AfterJson: string(after), OperatorId: operatorID, OperatorName: operatorName, CreatedAt: createdAt}
}

func factoryToProto(v store.Factory) *mdv1.Factory {
	return &mdv1.Factory{Id: v.ID, SupplierId: v.SupplierID, Code: v.Code, NameZh: v.NameZh, NameEn: v.NameEn,
		ShortName: v.ShortName, CountryCode: v.CountryCode, Timezone: v.Timezone, StateProvince: v.StateProvince,
		City: v.City, District: v.District, PostalCode: v.PostalCode, Address: v.Address, Status: v.Status, Remark: v.Remark}
}

func factoryInput(v *mdv1.FactoryInput, transferReason string, ctx context.Context) app.FactoryInput {
	if v == nil {
		v = &mdv1.FactoryInput{}
	}
	return app.FactoryInput{SupplierID: v.GetSupplierId(), Code: v.GetCode(), NameZh: v.GetNameZh(), NameEn: v.GetNameEn(),
		ShortName: v.GetShortName(), CountryCode: v.GetCountryCode(), Timezone: v.GetTimezone(), StateProvince: v.GetStateProvince(),
		City: v.GetCity(), District: v.GetDistrict(), PostalCode: v.GetPostalCode(), Address: v.GetAddress(), Status: v.GetStatus(),
		Remark: v.GetRemark(), TransferReason: transferReason, OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)}
}

func factoryContactToProto(v store.FactoryContact) *mdv1.FactoryContact {
	return &mdv1.FactoryContact{Id: v.ID, FactoryId: v.FactoryID, Name: v.Name, Department: v.Department, Title: v.Title,
		Phone: v.Phone, Email: v.Email, IsPrimary: v.IsPrimary, Status: v.Status, Remark: v.Remark}
}

func factoryOwnerToProto(v store.FactoryOwner) *mdv1.FactoryOwner {
	return &mdv1.FactoryOwner{Id: v.ID, FactoryId: v.FactoryID, EmployeeId: v.EmployeeID, EmployeeName: v.EmployeeName,
		ResponsibilityCode: v.ResponsibilityCode, IsPrimary: v.IsPrimary, StartDate: dateText(v.StartDate),
		EndDate: dateText(v.EndDate), Status: v.Status}
}

// 以下方法负责把供应商与工厂业务层模型转换为稳定的 gRPC 合同，避免前端直接依赖数据库字段。
func (h *Handler) ListSupplierCountries(ctx context.Context, req *mdv1.ListSupplierCountriesRequest) (*mdv1.ListSupplierCountriesResponse, error) {
	rows, err := h.svc.ListSupplierCountries(ctx, grpcx.TenantID(ctx), req.GetIncludeInactive())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CountryCount, len(rows))
	for i, v := range rows {
		out[i] = &mdv1.CountryCount{CountryCode: v.CountryCode, Count: v.SupplierCount}
	}
	return &mdv1.ListSupplierCountriesResponse{Countries: out}, nil
}

func (h *Handler) ListSupplierContacts(ctx context.Context, req *mdv1.ListSupplierContactsRequest) (*mdv1.ListSupplierContactsResponse, error) {
	rows, err := h.svc.ListSupplierContacts(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), req.GetIncludeInactive())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.SupplierContact, len(rows))
	for i, v := range rows {
		out[i] = supplierContactToProto(v)
	}
	return &mdv1.ListSupplierContactsResponse{Contacts: out}, nil
}

func (h *Handler) CreateSupplierContact(ctx context.Context, req *mdv1.CreateSupplierContactRequest) (*mdv1.CreateSupplierContactResponse, error) {
	v := req.GetContact()
	if v == nil {
		v = &mdv1.SupplierContactInput{}
	}
	out, err := h.svc.CreateSupplierContact(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), partyContactInput(v.GetName(), v.GetDepartment(), v.GetTitle(), v.GetPhone(), v.GetEmail(), v.GetIsPrimary(), v.GetRemark(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateSupplierContactResponse{Contact: supplierContactToProto(out)}, nil
}

func (h *Handler) UpdateSupplierContact(ctx context.Context, req *mdv1.UpdateSupplierContactRequest) (*mdv1.UpdateSupplierContactResponse, error) {
	v := req.GetContact()
	if v == nil {
		v = &mdv1.SupplierContactInput{}
	}
	out, err := h.svc.UpdateSupplierContact(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), req.GetId(), partyContactInput(v.GetName(), v.GetDepartment(), v.GetTitle(), v.GetPhone(), v.GetEmail(), v.GetIsPrimary(), v.GetRemark(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateSupplierContactResponse{Contact: supplierContactToProto(out)}, nil
}

func (h *Handler) DeactivateSupplierContact(ctx context.Context, req *mdv1.DeactivateSupplierContactRequest) (*mdv1.DeactivateSupplierContactResponse, error) {
	err := h.svc.DeactivateSupplierContact(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), req.GetId(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeactivateSupplierContactResponse{}, err
}

func (h *Handler) ListSupplierOwners(ctx context.Context, req *mdv1.ListSupplierOwnersRequest) (*mdv1.ListSupplierOwnersResponse, error) {
	rows, err := h.svc.ListSupplierOwners(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), req.GetIncludeInactive())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.SupplierOwner, len(rows))
	for i, v := range rows {
		out[i] = supplierOwnerToProto(v)
	}
	return &mdv1.ListSupplierOwnersResponse{Owners: out}, nil
}

func (h *Handler) CreateSupplierOwner(ctx context.Context, req *mdv1.CreateSupplierOwnerRequest) (*mdv1.CreateSupplierOwnerResponse, error) {
	v := req.GetOwner()
	if v == nil {
		v = &mdv1.SupplierOwnerInput{}
	}
	out, err := h.svc.CreateSupplierOwner(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), partyOwnerInput(v.GetEmployeeId(), v.GetEmployeeName(), v.GetResponsibilityCode(), v.GetStartDate(), v.GetEndDate(), v.GetIsPrimary(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateSupplierOwnerResponse{Owner: supplierOwnerToProto(out)}, nil
}

func (h *Handler) UpdateSupplierOwner(ctx context.Context, req *mdv1.UpdateSupplierOwnerRequest) (*mdv1.UpdateSupplierOwnerResponse, error) {
	v := req.GetOwner()
	if v == nil {
		v = &mdv1.SupplierOwnerInput{}
	}
	out, err := h.svc.UpdateSupplierOwner(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), req.GetId(), partyOwnerInput(v.GetEmployeeId(), v.GetEmployeeName(), v.GetResponsibilityCode(), v.GetStartDate(), v.GetEndDate(), v.GetIsPrimary(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateSupplierOwnerResponse{Owner: supplierOwnerToProto(out)}, nil
}

func (h *Handler) DeactivateSupplierOwner(ctx context.Context, req *mdv1.DeactivateSupplierOwnerRequest) (*mdv1.DeactivateSupplierOwnerResponse, error) {
	err := h.svc.DeactivateSupplierOwner(ctx, grpcx.TenantID(ctx), req.GetSupplierId(), req.GetId(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeactivateSupplierOwnerResponse{}, err
}

func (h *Handler) ListSupplierChanges(ctx context.Context, req *mdv1.ListSupplierChangesRequest) (*mdv1.ListSupplierChangesResponse, error) {
	rows, err := h.svc.ListSupplierChanges(ctx, grpcx.TenantID(ctx), req.GetSupplierId())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.MasterDataChange, len(rows))
	for i, v := range rows {
		out[i] = changeToProto(v.ID, v.Action, v.Section, v.Summary, v.BeforeData, v.AfterData, v.OperatorID, v.OperatorName, timeText(v.CreatedAt))
	}
	return &mdv1.ListSupplierChangesResponse{Changes: out}, nil
}

func (h *Handler) ListFactories(ctx context.Context, req *mdv1.ListFactoriesRequest) (*mdv1.ListFactoriesResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.ListFactories(ctx, grpcx.TenantID(ctx), req.GetKeyword(), req.GetStatus(), req.GetCountryCode(), req.GetCity(), req.GetProductCategory(), req.GetSupplierId(), req.GetOwnerId(), page, size)
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.Factory, len(rows))
	for i, v := range rows {
		out[i] = factoryToProto(store.Factory{ID: v.ID, SupplierID: v.SupplierID, Code: v.Code, NameZh: v.NameZh, NameEn: v.NameEn, ShortName: v.ShortName, CountryCode: v.CountryCode, Timezone: v.Timezone, StateProvince: v.StateProvince, City: v.City, District: v.District, PostalCode: v.PostalCode, Address: v.Address, Status: v.Status, Remark: v.Remark})
		out[i].SupplierCode = v.SupplierCode
		out[i].SupplierName = v.SupplierName
		out[i].OwnerNames = v.OwnerNames
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	return &mdv1.ListFactoriesResponse{Factories: out, Meta: &commonv1.PageMeta{Total: total, Page: page, PageSize: size}}, nil
}

func (h *Handler) GetFactory(ctx context.Context, req *mdv1.GetFactoryRequest) (*mdv1.GetFactoryResponse, error) {
	out, err := h.svc.GetFactory(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &mdv1.GetFactoryResponse{Factory: factoryToProto(out)}, nil
}
func (h *Handler) CreateFactory(ctx context.Context, req *mdv1.CreateFactoryRequest) (*mdv1.CreateFactoryResponse, error) {
	out, err := h.svc.CreateFactory(ctx, grpcx.TenantID(ctx), factoryInput(req.GetFactory(), "", ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateFactoryResponse{Factory: factoryToProto(out)}, nil
}
func (h *Handler) UpdateFactory(ctx context.Context, req *mdv1.UpdateFactoryRequest) (*mdv1.UpdateFactoryResponse, error) {
	out, err := h.svc.UpdateFactory(ctx, grpcx.TenantID(ctx), req.GetId(), factoryInput(req.GetFactory(), req.GetTransferReason(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateFactoryResponse{Factory: factoryToProto(out)}, nil
}

func (h *Handler) ListFactoryCountries(ctx context.Context, req *mdv1.ListFactoryCountriesRequest) (*mdv1.ListFactoryCountriesResponse, error) {
	rows, err := h.svc.ListFactoryCountries(ctx, grpcx.TenantID(ctx), req.GetIncludeInactive())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CountryCount, len(rows))
	for i, v := range rows {
		out[i] = &mdv1.CountryCount{CountryCode: v.CountryCode, Count: v.FactoryCount}
	}
	return &mdv1.ListFactoryCountriesResponse{Countries: out}, nil
}

func (h *Handler) ListFactoryContacts(ctx context.Context, req *mdv1.ListFactoryContactsRequest) (*mdv1.ListFactoryContactsResponse, error) {
	rows, err := h.svc.ListFactoryContacts(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetIncludeInactive())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.FactoryContact, len(rows))
	for i, v := range rows {
		out[i] = factoryContactToProto(v)
	}
	return &mdv1.ListFactoryContactsResponse{Contacts: out}, nil
}
func (h *Handler) CreateFactoryContact(ctx context.Context, req *mdv1.CreateFactoryContactRequest) (*mdv1.CreateFactoryContactResponse, error) {
	v := req.GetContact()
	if v == nil {
		v = &mdv1.FactoryContactInput{}
	}
	out, err := h.svc.CreateFactoryContact(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), partyContactInput(v.GetName(), v.GetDepartment(), v.GetTitle(), v.GetPhone(), v.GetEmail(), v.GetIsPrimary(), v.GetRemark(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateFactoryContactResponse{Contact: factoryContactToProto(out)}, nil
}
func (h *Handler) UpdateFactoryContact(ctx context.Context, req *mdv1.UpdateFactoryContactRequest) (*mdv1.UpdateFactoryContactResponse, error) {
	v := req.GetContact()
	if v == nil {
		v = &mdv1.FactoryContactInput{}
	}
	out, err := h.svc.UpdateFactoryContact(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetId(), partyContactInput(v.GetName(), v.GetDepartment(), v.GetTitle(), v.GetPhone(), v.GetEmail(), v.GetIsPrimary(), v.GetRemark(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateFactoryContactResponse{Contact: factoryContactToProto(out)}, nil
}
func (h *Handler) DeactivateFactoryContact(ctx context.Context, req *mdv1.DeactivateFactoryContactRequest) (*mdv1.DeactivateFactoryContactResponse, error) {
	err := h.svc.DeactivateFactoryContact(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetId(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeactivateFactoryContactResponse{}, err
}

func (h *Handler) ListFactoryOwners(ctx context.Context, req *mdv1.ListFactoryOwnersRequest) (*mdv1.ListFactoryOwnersResponse, error) {
	rows, err := h.svc.ListFactoryOwners(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetIncludeInactive())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.FactoryOwner, len(rows))
	for i, v := range rows {
		out[i] = factoryOwnerToProto(v)
	}
	return &mdv1.ListFactoryOwnersResponse{Owners: out}, nil
}
func (h *Handler) CreateFactoryOwner(ctx context.Context, req *mdv1.CreateFactoryOwnerRequest) (*mdv1.CreateFactoryOwnerResponse, error) {
	v := req.GetOwner()
	if v == nil {
		v = &mdv1.FactoryOwnerInput{}
	}
	out, err := h.svc.CreateFactoryOwner(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), partyOwnerInput(v.GetEmployeeId(), v.GetEmployeeName(), v.GetResponsibilityCode(), v.GetStartDate(), v.GetEndDate(), v.GetIsPrimary(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateFactoryOwnerResponse{Owner: factoryOwnerToProto(out)}, nil
}
func (h *Handler) UpdateFactoryOwner(ctx context.Context, req *mdv1.UpdateFactoryOwnerRequest) (*mdv1.UpdateFactoryOwnerResponse, error) {
	v := req.GetOwner()
	if v == nil {
		v = &mdv1.FactoryOwnerInput{}
	}
	out, err := h.svc.UpdateFactoryOwner(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetId(), partyOwnerInput(v.GetEmployeeId(), v.GetEmployeeName(), v.GetResponsibilityCode(), v.GetStartDate(), v.GetEndDate(), v.GetIsPrimary(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateFactoryOwnerResponse{Owner: factoryOwnerToProto(out)}, nil
}
func (h *Handler) DeactivateFactoryOwner(ctx context.Context, req *mdv1.DeactivateFactoryOwnerRequest) (*mdv1.DeactivateFactoryOwnerResponse, error) {
	err := h.svc.DeactivateFactoryOwner(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetId(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeactivateFactoryOwnerResponse{}, err
}

func (h *Handler) ListFactoryCapabilities(ctx context.Context, req *mdv1.ListFactoryCapabilitiesRequest) (*mdv1.ListFactoryCapabilitiesResponse, error) {
	rows, err := h.svc.ListFactoryCapabilities(ctx, grpcx.TenantID(ctx), req.GetFactoryId())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.FactoryCapability, len(rows))
	for i, v := range rows {
		out[i] = &mdv1.FactoryCapability{Id: v.ID, FactoryId: v.FactoryID, ProductCategory: v.ProductCategory, Process: v.Process, MonthlyCapacity: numericText(v.MonthlyCapacity), CapacityUnit: v.CapacityUnit, Moq: numericText(v.Moq), LeadTimeDays: v.LeadTimeDays, PeriodLabel: v.PeriodLabel, ConfirmedOn: dateText(v.ConfirmedOn), Remark: v.Remark}
	}
	return &mdv1.ListFactoryCapabilitiesResponse{Capabilities: out}, nil
}
func (h *Handler) CreateFactoryCapability(ctx context.Context, req *mdv1.CreateFactoryCapabilityRequest) (*mdv1.CreateFactoryCapabilityResponse, error) {
	v := req.GetCapability()
	if v == nil {
		v = &mdv1.FactoryCapabilityInput{}
	}
	out, err := h.svc.CreateFactoryCapability(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), app.FactoryCapabilityInput{ProductCategory: v.GetProductCategory(), Process: v.GetProcess(), MonthlyCapacity: v.GetMonthlyCapacity(), CapacityUnit: v.GetCapacityUnit(), MOQ: v.GetMoq(), LeadTimeDays: v.GetLeadTimeDays(), PeriodLabel: v.GetPeriodLabel(), ConfirmedOn: v.GetConfirmedOn(), Remark: v.GetRemark(), OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)})
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateFactoryCapabilityResponse{Capability: &mdv1.FactoryCapability{Id: out.ID, FactoryId: out.FactoryID, ProductCategory: out.ProductCategory, Process: out.Process, MonthlyCapacity: numericText(out.MonthlyCapacity), CapacityUnit: out.CapacityUnit, Moq: numericText(out.Moq), LeadTimeDays: out.LeadTimeDays, PeriodLabel: out.PeriodLabel, ConfirmedOn: dateText(out.ConfirmedOn), Remark: out.Remark}}, nil
}
func (h *Handler) DeleteFactoryCapability(ctx context.Context, req *mdv1.DeleteFactoryCapabilityRequest) (*mdv1.DeleteFactoryCapabilityResponse, error) {
	err := h.svc.DeleteFactoryCapability(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetId(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeleteFactoryCapabilityResponse{}, err
}

func (h *Handler) ListFactoryCertificates(ctx context.Context, req *mdv1.ListFactoryCertificatesRequest) (*mdv1.ListFactoryCertificatesResponse, error) {
	rows, err := h.svc.ListFactoryCertificates(ctx, grpcx.TenantID(ctx), req.GetFactoryId())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.FactoryCertificate, len(rows))
	for i, v := range rows {
		out[i] = &mdv1.FactoryCertificate{Id: v.Row.ID, FactoryId: v.Row.FactoryID, Name: v.Row.Name, CertificateNo: v.Row.CertificateNo, IssuedOn: dateText(v.Row.IssuedOn), ExpiresOn: dateText(v.Row.ExpiresOn), Status: v.Row.Status, FileKey: v.Row.FileKey, Remark: v.Row.Remark, FileUrl: v.FileURL, FileName: v.FileName}
	}
	return &mdv1.ListFactoryCertificatesResponse{Certificates: out}, nil
}

func (h *Handler) PresignFactoryCertificateFile(ctx context.Context, req *mdv1.PresignFactoryCertificateFileRequest) (*mdv1.PresignFactoryCertificateFileResponse, error) {
	p, err := h.svc.PresignFactoryCertificateFile(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &mdv1.PresignFactoryCertificateFileResponse{Key: p.Key, UploadUrl: p.UploadURL, ExpiresSeconds: p.Expires}, nil
}
func (h *Handler) CreateFactoryCertificate(ctx context.Context, req *mdv1.CreateFactoryCertificateRequest) (*mdv1.CreateFactoryCertificateResponse, error) {
	v := req.GetCertificate()
	if v == nil {
		v = &mdv1.FactoryCertificateInput{}
	}
	out, err := h.svc.CreateFactoryCertificate(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), app.FactoryCertificateInput{Name: v.GetName(), CertificateNo: v.GetCertificateNo(), IssuedOn: v.GetIssuedOn(), ExpiresOn: v.GetExpiresOn(), Status: v.GetStatus(), FileKey: v.GetFileKey(), Remark: v.GetRemark(), OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)})
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateFactoryCertificateResponse{Certificate: &mdv1.FactoryCertificate{Id: out.ID, FactoryId: out.FactoryID, Name: out.Name, CertificateNo: out.CertificateNo, IssuedOn: dateText(out.IssuedOn), ExpiresOn: dateText(out.ExpiresOn), Status: out.Status, FileKey: out.FileKey, Remark: out.Remark}}, nil
}
func (h *Handler) DeleteFactoryCertificate(ctx context.Context, req *mdv1.DeleteFactoryCertificateRequest) (*mdv1.DeleteFactoryCertificateResponse, error) {
	err := h.svc.DeleteFactoryCertificate(ctx, grpcx.TenantID(ctx), req.GetFactoryId(), req.GetId(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeleteFactoryCertificateResponse{}, err
}

func (h *Handler) ListFactoryChanges(ctx context.Context, req *mdv1.ListFactoryChangesRequest) (*mdv1.ListFactoryChangesResponse, error) {
	rows, err := h.svc.ListFactoryChanges(ctx, grpcx.TenantID(ctx), req.GetFactoryId())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.MasterDataChange, len(rows))
	for i, v := range rows {
		out[i] = changeToProto(v.ID, v.Action, v.Section, v.Summary, v.BeforeData, v.AfterData, v.OperatorID, v.OperatorName, timeText(v.CreatedAt))
	}
	return &mdv1.ListFactoryChangesResponse{Changes: out}, nil
}

// ImportSuppliers 将预检和确认导入统一交给业务层，保证 Gateway 与其他调用方规则一致。
func (h *Handler) ImportSuppliers(ctx context.Context, req *mdv1.ImportSuppliersRequest) (*mdv1.ImportSuppliersResponse, error) {
	rows := make([]app.SupplierImportRow, len(req.GetRows()))
	for i, row := range req.GetRows() {
		rows[i] = app.SupplierImportRow{RowNumber: row.GetRowNumber(), Code: row.GetCode(), NameZh: row.GetNameZh(),
			NameEn: row.GetNameEn(), ShortName: row.GetShortName(), CountryCode: row.GetCountryCode(), Currency: row.GetCurrency(),
			PaymentTerm: row.GetPaymentTerm(), BusinessTypes: row.GetBusinessTypes(), ContactName: row.GetContactName(),
			ContactPhone: row.GetContactPhone(), ContactEmail: row.GetContactEmail(), Address: row.GetAddress(), Remark: row.GetRemark()}
	}
	ready, imported, issues, err := h.svc.ImportSuppliers(ctx, grpcx.TenantID(ctx), rows, req.GetConfirm(), operatorID(ctx), operatorName(ctx))
	if err != nil {
		return nil, err
	}
	protoIssues := importIssuesToProto(issues)
	return &mdv1.ImportSuppliersResponse{ReadyCount: ready, ImportedCount: imported, Issues: protoIssues}, nil
}

// ImportFactories 把 CSV 行转换成工厂导入模型，并保留原始行号供界面定位错误。
func (h *Handler) ImportFactories(ctx context.Context, req *mdv1.ImportFactoriesRequest) (*mdv1.ImportFactoriesResponse, error) {
	rows := make([]app.FactoryImportRow, len(req.GetRows()))
	for i, row := range req.GetRows() {
		rows[i] = app.FactoryImportRow{RowNumber: row.GetRowNumber(), Code: row.GetCode(), SupplierCode: row.GetSupplierCode(),
			NameZh: row.GetNameZh(), NameEn: row.GetNameEn(), ShortName: row.GetShortName(), CountryCode: row.GetCountryCode(),
			Timezone: row.GetTimezone(), StateProvince: row.GetStateProvince(), City: row.GetCity(), District: row.GetDistrict(),
			PostalCode: row.GetPostalCode(), Address: row.GetAddress(), Status: row.GetStatus(), Remark: row.GetRemark()}
	}
	ready, imported, issues, err := h.svc.ImportFactories(ctx, grpcx.TenantID(ctx), rows, req.GetConfirm(), operatorID(ctx), operatorName(ctx))
	if err != nil {
		return nil, err
	}
	protoIssues := importIssuesToProto(issues)
	return &mdv1.ImportFactoriesResponse{ReadyCount: ready, ImportedCount: imported, Issues: protoIssues}, nil
}

func importIssuesToProto(issues []app.MasterDataImportIssue) []*mdv1.ImportMasterDataIssue {
	out := make([]*mdv1.ImportMasterDataIssue, len(issues))
	for i, issue := range issues {
		out[i] = &mdv1.ImportMasterDataIssue{RowNumber: issue.RowNumber, Code: issue.Code, Name: issue.Name, Message: issue.Message}
	}
	return out
}
