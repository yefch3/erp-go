package grpcin

import (
	"context"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
)

func customerBasicResponse(b app.CustomerImportRow, tenantID int64) *mdv1.CustomerBasicResponse {
	return &mdv1.CustomerBasicResponse{TenantId: tenantID, Basic: &mdv1.CustomerImportRow{
		Code: b.Code, Name: b.Name, ShortName: b.ShortName, CountryRegion: b.CountryRegion, CountryCode: b.CountryCode, CustomerType: b.CustomerType, CreditGrade: b.CreditGrade, Source: b.Source,
		ArchiveCreator: b.ArchiveCreator, CompanyPhone: b.CompanyPhone, FaxNumber: b.FaxNumber, CompanyEmail: b.CompanyEmail, Address: b.Address, PostalCode: b.PostalCode, AddressState: b.AddressState}}
}
func (h *Handler) GetCustomerBasic(ctx context.Context, r *mdv1.GetCustomerBasicRequest) (*mdv1.CustomerBasicResponse, error) {
	b, err := h.svc.GetCustomerBasic(ctx, grpcx.TenantID(ctx), r.CustomerId)
	return customerBasicResponse(b, grpcx.TenantID(ctx)), err
}
func (h *Handler) SaveCustomerBasic(ctx context.Context, r *mdv1.SaveCustomerBasicRequest) (*mdv1.CustomerBasicResponse, error) {
	b, err := h.svc.SaveCustomerBasic(ctx, grpcx.TenantID(ctx), r.CustomerId, importRowFromProto(r.Basic), operatorID(ctx), operatorName(ctx))
	return customerBasicResponse(b, grpcx.TenantID(ctx)), err
}
