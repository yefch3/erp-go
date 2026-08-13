package grpcin

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/masterdata/internal/app"
)

func portToProto(p app.Port) *mdv1.Port {
	return &mdv1.Port{Id: p.ID, Unlocode: p.UNLOCODE, NameZh: p.NameZH, NameEn: p.NameEN, CountryCode: p.CountryCode, City: p.City, Timezone: p.Timezone, Aliases: p.Aliases, Status: p.Status, Remark: p.Remark, Version: p.Version}
}

func portInput(p *mdv1.Port, ctx context.Context) app.PortInput {
	if p == nil {
		p = &mdv1.Port{}
	}
	return app.PortInput{Port: app.Port{ID: p.GetId(), UNLOCODE: p.GetUnlocode(), NameZH: p.GetNameZh(), NameEN: p.GetNameEn(), CountryCode: p.GetCountryCode(), City: p.GetCity(), Timezone: p.GetTimezone(), Aliases: p.GetAliases(), Status: p.GetStatus(), Remark: p.GetRemark(), Version: p.GetVersion()}, OperatorID: operatorID(ctx), OperatorName: operatorName(ctx)}
}

func (h *Handler) CreatePort(ctx context.Context, req *mdv1.CreatePortRequest) (*mdv1.CreatePortResponse, error) {
	if req.GetPort() == nil {
		return nil, apierr.Invalid("MD_PORT_REQUIRED", "港口资料不能为空")
	}
	p, err := h.svc.CreatePort(ctx, grpcx.TenantID(ctx), portInput(req.GetPort(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.CreatePortResponse{Port: portToProto(p)}, nil
}
func (h *Handler) GetPort(ctx context.Context, req *mdv1.GetPortRequest) (*mdv1.GetPortResponse, error) {
	p, err := h.svc.GetPort(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &mdv1.GetPortResponse{Port: portToProto(p)}, nil
}
func (h *Handler) ListPorts(ctx context.Context, req *mdv1.ListPortsRequest) (*mdv1.ListPortsResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	ports, total, err := h.svc.ListPorts(ctx, grpcx.TenantID(ctx), req.GetKeyword(), req.GetCountryCode(), req.GetStatus(), page, size)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	out := make([]*mdv1.Port, len(ports))
	for i, p := range ports {
		out[i] = portToProto(p)
	}
	return &mdv1.ListPortsResponse{Ports: out, Meta: &commonv1.PageMeta{Total: total, Page: page, PageSize: size}}, nil
}

func (h *Handler) ListPortCountries(ctx context.Context, req *mdv1.ListPortCountriesRequest) (*mdv1.ListPortCountriesResponse, error) {
	items, err := h.svc.ListPortCountries(ctx, grpcx.TenantID(ctx), req.GetStatus())
	if err != nil {
		return nil, err
	}
	out := make([]*mdv1.PortCountryCount, len(items))
	for i, item := range items {
		out[i] = &mdv1.PortCountryCount{CountryCode: item.CountryCode, PortCount: item.PortCount}
	}
	return &mdv1.ListPortCountriesResponse{Countries: out}, nil
}

func (h *Handler) ImportPorts(ctx context.Context, req *mdv1.ImportPortsRequest) (*mdv1.ImportPortsResponse, error) {
	rows := make([]app.PortImportRow, len(req.GetRows()))
	for i, row := range req.GetRows() {
		rows[i] = app.PortImportRow{RowNumber: row.GetRowNumber(), PortInput: app.PortInput{Port: app.Port{UNLOCODE: row.GetUnlocode(), NameZH: row.GetNameZh(), NameEN: row.GetNameEn(), CountryCode: row.GetCountryCode(), City: row.GetCity(), Timezone: row.GetTimezone(), Aliases: row.GetAliases(), Remark: row.GetRemark()}}}
	}
	result, err := h.svc.ImportPorts(ctx, grpcx.TenantID(ctx), rows, req.GetConfirm(), operatorID(ctx), operatorName(ctx))
	if err != nil {
		return nil, err
	}
	issues := make([]*mdv1.PortImportIssue, len(result.Issues))
	for i, issue := range result.Issues {
		issues[i] = &mdv1.PortImportIssue{RowNumber: issue.RowNumber, Code: issue.Code, Message: issue.Message}
	}
	return &mdv1.ImportPortsResponse{CreateCount: result.CreateCount, UpdateCount: result.UpdateCount, SkipCount: result.SkipCount, Issues: issues}, nil
}
func (h *Handler) UpdatePort(ctx context.Context, req *mdv1.UpdatePortRequest) (*mdv1.UpdatePortResponse, error) {
	if req.GetPort() == nil {
		return nil, apierr.Invalid("MD_PORT_REQUIRED", "港口资料不能为空")
	}
	p, err := h.svc.UpdatePort(ctx, grpcx.TenantID(ctx), portInput(req.GetPort(), ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.UpdatePortResponse{Port: portToProto(p)}, nil
}
func (h *Handler) SetPortStatus(ctx context.Context, req *mdv1.SetPortStatusRequest) (*mdv1.SetPortStatusResponse, error) {
	p, err := h.svc.SetPortStatus(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetStatus(), req.GetVersion(), operatorID(ctx), operatorName(ctx))
	if err != nil {
		return nil, err
	}
	return &mdv1.SetPortStatusResponse{Port: portToProto(p)}, nil
}
