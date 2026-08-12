package grpcin

import (
	"context"
	"strings"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

func contactToProto(c store.CustomerContact) *mdv1.Contact {
	return &mdv1.Contact{Id: c.ID, Name: c.Name, Department: c.Department, Title: c.Title,
		Email: c.Email, Phone: c.Phone, Mobile: c.Mobile, InstantMessaging: c.InstantMessaging,
		Language: c.Language, Remark: c.Remark, IsPrimary: c.IsPrimary, SortOrder: c.SortOrder, Status: c.Status,
		EmailPermission: c.EmailPermission, EmailCategories: c.EmailCategories}
}

func contactInput(c *mdv1.CustomerContactInput, ctx context.Context) app.CustomerContactInput {
	if c == nil {
		c = &mdv1.CustomerContactInput{}
	}
	return app.CustomerContactInput{Name: c.GetName(), Department: c.GetDepartment(), Title: c.GetTitle(),
		Email: c.GetEmail(), Phone: c.GetPhone(), Mobile: c.GetMobile(), InstantMessaging: c.GetInstantMessaging(),
		Language: c.GetLanguage(), Remark: c.GetRemark(), IsPrimary: c.GetIsPrimary(), SortOrder: c.GetSortOrder(),
		EmailPermission: c.GetEmailPermission(), EmailCategories: c.GetEmailCategories(),
		OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)}
}

func (h *Handler) ListCustomerContacts(ctx context.Context, req *mdv1.ListCustomerContactsRequest) (*mdv1.ListCustomerContactsResponse, error) {
	rows, err := h.svc.ListCustomerContactsDetailed(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetStatus())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.Contact, len(rows))
	for i := range rows {
		out[i] = contactToProto(rows[i])
	}
	return &mdv1.ListCustomerContactsResponse{Contacts: out}, nil
}

func (h *Handler) CreateCustomerContact(ctx context.Context, req *mdv1.CreateCustomerContactRequest) (*mdv1.CreateCustomerContactResponse, error) {
	out, err := h.svc.CreateCustomerContact(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), contactInput(req.GetContact(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateCustomerContactResponse{Contact: contactToProto(out)}, nil
}

func (h *Handler) UpdateCustomerContact(ctx context.Context, req *mdv1.UpdateCustomerContactRequest) (*mdv1.UpdateCustomerContactResponse, error) {
	out, err := h.svc.UpdateCustomerContact(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetId(), contactInput(req.GetContact(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateCustomerContactResponse{Contact: contactToProto(out)}, nil
}

func (h *Handler) DeactivateCustomerContact(ctx context.Context, req *mdv1.DeactivateCustomerContactRequest) (*mdv1.DeactivateCustomerContactResponse, error) {
	err := h.svc.DeactivateCustomerContact(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetId(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeactivateCustomerContactResponse{}, err
}

func ownerToProto(o store.CustomerOwner) *mdv1.CustomerOwner {
	start, end := "", ""
	if o.StartDate.Valid {
		start = o.StartDate.Time.Format("2006-01-02")
	}
	if o.EndDate.Valid {
		end = o.EndDate.Time.Format("2006-01-02")
	}
	return &mdv1.CustomerOwner{Id: o.ID, CustomerId: o.CustomerID, EmployeeId: o.EmployeeID,
		EmployeeName: o.EmployeeName, ResponsibilityCode: o.ResponsibilityCode, StartDate: start, EndDate: end,
		Status: o.Status, IsPrimary: o.IsPrimary}
}

func (h *Handler) ListCustomerOwners(ctx context.Context, req *mdv1.ListCustomerOwnersRequest) (*mdv1.ListCustomerOwnersResponse, error) {
	rows, err := h.svc.ListCustomerOwners(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetStatus())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CustomerOwner, len(rows))
	for i := range rows {
		out[i] = ownerToProto(rows[i])
	}
	return &mdv1.ListCustomerOwnersResponse{Owners: out}, nil
}

func (h *Handler) CreateCustomerOwner(ctx context.Context, req *mdv1.CreateCustomerOwnerRequest) (*mdv1.CreateCustomerOwnerResponse, error) {
	in := req.GetOwner()
	if in == nil {
		in = &mdv1.CustomerOwnerInput{}
	}
	out, err := h.svc.CreateCustomerOwner(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), app.CustomerOwnerInput{
		EmployeeID: in.GetEmployeeId(), EmployeeName: in.GetEmployeeName(), ResponsibilityCode: in.GetResponsibilityCode(),
		StartDate: in.GetStartDate(), EndDate: in.GetEndDate(), IsPrimary: in.GetIsPrimary(),
		OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)})
	if err != nil {
		return nil, err
	}
	return &mdv1.CreateCustomerOwnerResponse{Owner: ownerToProto(out)}, nil
}

func (h *Handler) UpdateCustomerOwner(ctx context.Context, req *mdv1.UpdateCustomerOwnerRequest) (*mdv1.UpdateCustomerOwnerResponse, error) {
	in := req.GetOwner()
	if in == nil {
		in = &mdv1.CustomerOwnerInput{}
	}
	out, err := h.svc.UpdateCustomerOwner(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetId(), app.CustomerOwnerInput{
		ResponsibilityCode: in.GetResponsibilityCode(), StartDate: in.GetStartDate(), EndDate: in.GetEndDate(),
		IsPrimary: in.GetIsPrimary(), OperatorID: operatorID(ctx), OperatorName: operatorName(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdateCustomerOwnerResponse{Owner: ownerToProto(out)}, nil
}

func (h *Handler) DeactivateCustomerOwner(ctx context.Context, req *mdv1.DeactivateCustomerOwnerRequest) (*mdv1.DeactivateCustomerOwnerResponse, error) {
	err := h.svc.DeactivateCustomerOwner(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), req.GetId(), req.GetEndDate(), operatorID(ctx), operatorName(ctx))
	return &mdv1.DeactivateCustomerOwnerResponse{}, err
}

func (h *Handler) ListCustomerChanges(ctx context.Context, req *mdv1.ListCustomerChangesRequest) (*mdv1.ListCustomerChangesResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, err := h.svc.ListCustomerChanges(ctx, grpcx.TenantID(ctx), req.GetCustomerId(), page, size)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	out := make([]*mdv1.CustomerChange, len(rows))
	for i, c := range rows {
		created := ""
		if c.CreatedAt.Valid {
			created = c.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		}
		out[i] = &mdv1.CustomerChange{Id: c.ID, Action: c.Action, Section: c.Section, Summary: c.Summary, BeforeData: string(c.BeforeData), AfterData: string(c.AfterData), OperatorId: c.OperatorID, OperatorName: c.OperatorName, CreatedAt: created}
	}
	return &mdv1.ListCustomerChangesResponse{Changes: out, Meta: &commonv1.PageMeta{Total: total, Page: page, PageSize: size}}, nil
}

func (h *Handler) ImportCustomers(ctx context.Context, req *mdv1.ImportCustomersRequest) (*mdv1.ImportCustomersResponse, error) {
	rows := make([]app.CustomerImportRow, len(req.GetRows()))
	for i, r := range req.GetRows() {
		rows[i] = app.CustomerImportRow{Code: r.GetCode(), Name: r.GetName(), CountryCode: r.GetCountryCode(), CustomerType: r.GetCustomerType(), Currency: r.GetCurrency(), PaymentTerm: r.GetPaymentTerm(), ContactName: r.GetContactName(), ContactEmail: r.GetContactEmail(), ContactPhone: r.GetContactPhone(), Remark: r.GetRemark()}
	}
	verdicts, imported, err := h.svc.ImportCustomers(ctx, grpcx.TenantID(ctx), rows, req.GetDryRun(), operatorID(ctx), operatorName(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CustomerImportVerdict, len(verdicts))
	var ready, blocked int32
	for i, v := range verdicts {
		out[i] = &mdv1.CustomerImportVerdict{Line: v.Line, Code: v.Code, Name: v.Name, Ok: v.OK, Reason: v.Reason}
		if v.OK {
			ready++
		} else {
			blocked++
		}
	}
	return &mdv1.ImportCustomersResponse{Verdicts: out, Ready: ready, Blocked: blocked, Imported: imported}, nil
}

func (h *Handler) CheckCustomerDuplicates(ctx context.Context, req *mdv1.CheckCustomerDuplicatesRequest) (*mdv1.CheckCustomerDuplicatesResponse, error) {
	rows, err := h.svc.CheckCustomerDuplicates(ctx, grpcx.TenantID(ctx), strings.TrimSpace(req.GetName()), strings.TrimSpace(req.GetTaxId()), req.GetExcludeId())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.CustomerDuplicate, len(rows))
	for i, r := range rows {
		out[i] = &mdv1.CustomerDuplicate{Id: r.ID, Code: r.Code, Name: r.Name, TaxId: r.TaxID}
	}
	return &mdv1.CheckCustomerDuplicatesResponse{Candidates: out}, nil
}
