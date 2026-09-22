package grpcin

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

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

func (h *Handler) GetSupplierDeactivationImpact(ctx context.Context, req *mdv1.GetSupplierDeactivationImpactRequest) (*mdv1.GetSupplierDeactivationImpactResponse, error) {
	rows, err := h.svc.DeactivationImpact(ctx, grpcx.TenantID(ctx), "SUPPLIER", req.GetId())
	if err != nil {
		return nil, err
	}
	items, total := impactItems(rows)
	return &mdv1.GetSupplierDeactivationImpactResponse{Items: items, Total: total}, nil
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

func (h *Handler) BatchUpdateSupplierOwners(ctx context.Context, req *mdv1.BatchUpdateSupplierOwnersRequest) (*mdv1.BatchUpdateSupplierOwnersResponse, error) {
	owners := make([]app.BulkOwnerAssignment, 0, len(req.GetOwners()))
	for _, owner := range req.GetOwners() {
		owners = append(owners, app.BulkOwnerAssignment{EmployeeID: owner.GetEmployeeId(), EmployeeName: owner.GetEmployeeName()})
	}
	changed, err := h.svc.BatchUpdateSupplierOwners(ctx, grpcx.TenantID(ctx), req.GetSupplierIds(), owners, req.GetAction(), operatorID(ctx), operatorName(ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.BatchUpdateSupplierOwnersResponse{SupplierCount: int32(len(req.GetSupplierIds())), OwnerCount: int32(len(owners)), ChangedCount: changed}, nil
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

func importIssuesToProto(issues []app.MasterDataImportIssue) []*mdv1.ImportMasterDataIssue {
	out := make([]*mdv1.ImportMasterDataIssue, len(issues))
	for i, issue := range issues {
		out[i] = &mdv1.ImportMasterDataIssue{RowNumber: issue.RowNumber, Code: issue.Code, Name: issue.Name, Message: issue.Message}
	}
	return out
}

func (h *Handler) ImportSuppliers(ctx context.Context, req *mdv1.ImportSuppliersRequest) (*mdv1.ImportSuppliersResponse, error) {
	rows := make([]app.SupplierImportRow, len(req.GetRows()))
	for i, row := range req.GetRows() {
		rows[i] = app.SupplierImportRow{RowNumber: row.GetRowNumber(), Code: row.GetCode(), NameZh: row.GetNameZh(),
			NameEn: row.GetNameEn(), ShortName: row.GetShortName(), CountryCode: row.GetCountryCode(), Currency: row.GetCurrency(),
			PaymentTerm: row.GetPaymentTerm(), BusinessTypes: row.GetBusinessTypes(), ContactName: row.GetContactName(),
			ContactPhone: row.GetContactPhone(), ContactEmail: row.GetContactEmail(), Address: row.GetAddress(), Remark: row.GetRemark(),
			TaxID: row.GetTaxId(), RegisteredAddress: row.GetRegisteredAddress(), ContactDepartment: row.GetContactDepartment(),
			ContactTitle: row.GetContactTitle(), ContactIsPrimary: row.GetContactIsPrimary(), ContactRemark: row.GetContactRemark()}
	}
	ready, imported, issues, err := h.svc.ImportSuppliers(ctx, grpcx.TenantID(ctx), rows, req.GetConfirm(), operatorID(ctx), operatorName(ctx))
	if err != nil {
		return nil, err
	}
	protoIssues := importIssuesToProto(issues)
	return &mdv1.ImportSuppliersResponse{ReadyCount: ready, ImportedCount: imported, Issues: protoIssues}, nil
}
