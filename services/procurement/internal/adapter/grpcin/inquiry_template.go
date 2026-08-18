package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type InquiryTemplateHandler struct {
	prv1.UnimplementedInquiryTemplateServiceServer
	svc *app.Service
}

func NewInquiryTemplates(svc *app.Service) *InquiryTemplateHandler {
	return &InquiryTemplateHandler{svc: svc}
}

func (h *InquiryTemplateHandler) ListInquiryTemplates(ctx context.Context, req *prv1.ListInquiryTemplatesRequest) (*prv1.ListInquiryTemplatesResponse, error) {
	views, err := h.svc.ListInquiryTemplates(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.InquiryTemplate, 0, len(views))
	for _, view := range views {
		out = append(out, inquiryTemplateToProto(view))
	}
	return &prv1.ListInquiryTemplatesResponse{Templates: out}, nil
}

func (h *InquiryTemplateHandler) GetInquiryTemplate(ctx context.Context, req *prv1.GetInquiryTemplateRequest) (*prv1.GetInquiryTemplateResponse, error) {
	view, err := h.svc.GetInquiryTemplate(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetInquiryTemplateResponse{Template: inquiryTemplateToProto(view)}, nil
}

func (h *InquiryTemplateHandler) GetDefaultInquiryTemplate(ctx context.Context, req *prv1.GetDefaultInquiryTemplateRequest) (*prv1.GetDefaultInquiryTemplateResponse, error) {
	view, err := h.svc.GetDefaultInquiryTemplate(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.GetDefaultInquiryTemplateResponse{Template: inquiryTemplateToProto(view)}, nil
}

func (h *InquiryTemplateHandler) CreateInquiryTemplate(ctx context.Context, req *prv1.CreateInquiryTemplateRequest) (*prv1.CreateInquiryTemplateResponse, error) {
	view, err := h.svc.CreateInquiryTemplate(ctx, grpcx.TenantID(ctx), inquiryTemplateInput(req.GetTemplateCode(), req.GetName(), req.GetDescription(), req.GetIsDefault(), req.GetFields()), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.CreateInquiryTemplateResponse{Template: inquiryTemplateToProto(view)}, nil
}

func (h *InquiryTemplateHandler) SaveInquiryTemplate(ctx context.Context, req *prv1.SaveInquiryTemplateRequest) (*prv1.SaveInquiryTemplateResponse, error) {
	view, err := h.svc.SaveInquiryTemplate(ctx, grpcx.TenantID(ctx), req.GetId(), inquiryTemplateInput("", req.GetName(), req.GetDescription(), req.GetIsDefault(), req.GetFields()), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.SaveInquiryTemplateResponse{Template: inquiryTemplateToProto(view)}, nil
}

func (h *InquiryTemplateHandler) SetInquiryTemplateStatus(ctx context.Context, req *prv1.SetInquiryTemplateStatusRequest) (*prv1.SetInquiryTemplateStatusResponse, error) {
	view, err := h.svc.SetInquiryTemplateStatus(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetStatus(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.SetInquiryTemplateStatusResponse{Template: inquiryTemplateToProto(view)}, nil
}

func (h *InquiryTemplateHandler) SetDefaultInquiryTemplate(ctx context.Context, req *prv1.SetDefaultInquiryTemplateRequest) (*prv1.SetDefaultInquiryTemplateResponse, error) {
	view, err := h.svc.SetDefaultInquiryTemplate(ctx, grpcx.TenantID(ctx), req.GetId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.SetDefaultInquiryTemplateResponse{Template: inquiryTemplateToProto(view)}, nil
}

func inquiryTemplateInput(code, name, description string, isDefault bool, fields []*prv1.InquiryTemplateField) app.InquiryTemplateInput {
	in := app.InquiryTemplateInput{
		TemplateCode: code, Name: name, Description: description, IsDefault: isDefault,
		Fields: make([]app.InquiryTemplateFieldInput, 0, len(fields)),
	}
	for _, field := range fields {
		in.Fields = append(in.Fields, app.InquiryTemplateFieldInput{
			FieldKey: field.GetFieldKey(), DisplayName: field.GetDisplayName(),
			SortOrder: field.GetSortOrder(), IsRequired: field.GetIsRequired(),
			DefaultValue: field.GetDefaultValue(), DataType: field.GetDataType(),
		})
	}
	return in
}

func inquiryTemplateToProto(view app.InquiryTemplateView) *prv1.InquiryTemplate {
	row := view.Template
	fields := make([]*prv1.InquiryTemplateField, 0, len(view.Fields))
	for _, field := range view.Fields {
		fields = append(fields, &prv1.InquiryTemplateField{
			FieldKey: field.FieldKey, DisplayName: field.DisplayName,
			SortOrder: field.SortOrder, IsRequired: field.IsRequired,
			DefaultValue: field.DefaultValue, DataType: field.DataType,
			IsCustom: field.IsCustom, IsCore: isCoreField(field),
		})
	}
	return &prv1.InquiryTemplate{
		Id: row.ID, TemplateCode: row.TemplateCode, Version: row.Version,
		Name: row.Name, Description: row.Description, Status: row.Status,
		IsDefault: row.IsDefault, IsSystem: row.TemplateCode == app.SystemInquiryTemplateCode,
		FieldCount: int32(len(view.Fields)), Fields: fields,
		CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt), UpdatedAt: ts(row.UpdatedAt),
	}
}

func isCoreField(field store.ListInquiryTemplateFieldsRow) bool {
	return app.IsCoreInquiryField(field.FieldKey)
}
