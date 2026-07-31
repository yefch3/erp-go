package grpcin

import (
	"context"
	"encoding/json"
	"strings"

	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/product/internal/app"
)

func (h *Handler) ResolveAttributes(ctx context.Context, req *pdv1.ResolveAttributesRequest) (*pdv1.ResolveAttributesResponse, error) {
	defs, err := h.svc.ResolveDefs(ctx, grpcx.TenantID(ctx), req.GetCategoryId())
	if err != nil {
		return nil, err
	}
	return &pdv1.ResolveAttributesResponse{Defs: defsToProto(defs)}, nil
}

func (h *Handler) GetTemplate(ctx context.Context, req *pdv1.GetTemplateRequest) (*pdv1.GetTemplateResponse, error) {
	name, defs, err := h.svc.OwnDefs(ctx, grpcx.TenantID(ctx), req.GetCategoryId())
	if err != nil {
		return nil, err
	}
	return &pdv1.GetTemplateResponse{Name: name, Defs: defsToProto(defs)}, nil
}

func (h *Handler) SaveTemplate(ctx context.Context, req *pdv1.SaveTemplateRequest) (*pdv1.SaveTemplateResponse, error) {
	tenantID := grpcx.TenantID(ctx)
	if err := h.svc.SaveTemplate(ctx, tenantID, app.TemplateInput{
		CategoryID: req.GetCategoryId(), Name: req.GetName(),
		Defs: defsFromProto(req.GetDefs()),
	}); err != nil {
		return nil, err
	}
	_, defs, err := h.svc.OwnDefs(ctx, tenantID, req.GetCategoryId())
	if err != nil {
		return nil, err
	}
	return &pdv1.SaveTemplateResponse{Defs: defsToProto(defs)}, nil
}

func (h *Handler) SetProductAttributes(ctx context.Context, req *pdv1.SetProductAttributesRequest) (*pdv1.SetProductAttributesResponse, error) {
	values, err := decodeAttrs(req.GetAttributesJson())
	if err != nil {
		return nil, err
	}
	if err := h.svc.SetProductAttributes(ctx, grpcx.TenantID(ctx),
		req.GetProductId(), values, operator(ctx)); err != nil {
		return nil, err
	}
	return &pdv1.SetProductAttributesResponse{AttributesJson: req.GetAttributesJson()}, nil
}

func (h *Handler) SetSkuAttributes(ctx context.Context, req *pdv1.SetSkuAttributesRequest) (*pdv1.SetSkuAttributesResponse, error) {
	values, err := decodeAttrs(req.GetAttributesJson())
	if err != nil {
		return nil, err
	}
	if err := h.svc.SetSkuAttributes(ctx, grpcx.TenantID(ctx),
		req.GetSkuId(), values, req.GetSpec()); err != nil {
		return nil, err
	}
	return &pdv1.SetSkuAttributesResponse{AttributesJson: req.GetAttributesJson()}, nil
}

func (h *Handler) RecallCandidates(ctx context.Context, req *pdv1.RecallCandidatesRequest) (*pdv1.RecallCandidatesResponse, error) {
	values, err := decodeAttrs(req.GetAttributesJson())
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.Recall(ctx, grpcx.TenantID(ctx), app.AttrQuery{
		CategoryID: req.GetCategoryId(), Values: values,
		Keyword: req.GetKeyword(), Limit: req.GetLimit(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*pdv1.Candidate, 0, len(rows))
	for _, c := range rows {
		attrs, _ := json.Marshal(c.Attributes)
		out = append(out, &pdv1.Candidate{
			ProductId: c.ProductID, SkuId: c.SkuID,
			ProductCode: c.ProductCode, ProductName: c.ProductName,
			SkuCode: c.SkuCode, Spec: c.Spec,
			AttributesJson: string(attrs), Score: c.Score,
			RecallSource: c.RecallSource, Reason: c.Reason,
		})
	}
	return &pdv1.RecallCandidatesResponse{Candidates: out}, nil
}

// decodeAttrs reads the JSON object the client sent. json.Number keeps a
// dimension a number rather than turning 3.0 into 3 or into a float that
// prints as 2.9999999 — the value is about to be compared for equality.
func decodeAttrs(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, apierr.Invalid("PD_ATTR_JSON_INVALID", "属性内容不是合法的 JSON 对象")
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func defsToProto(defs []app.AttributeDef) []*pdv1.AttributeDef {
	out := make([]*pdv1.AttributeDef, 0, len(defs))
	for _, d := range defs {
		out = append(out, &pdv1.AttributeDef{
			Key: d.Key, Label: d.Label, DataType: d.DataType, Unit: d.Unit,
			EnumValues: d.EnumValues, MatchStrategy: d.MatchStrategy,
			TolerancePct: d.TolerancePct, Level: d.Level,
			IsMatchable: d.IsMatchable, IsRequired: d.IsRequired,
			SortOrder: d.SortOrder, FromCategoryId: d.FromCategoryID,
		})
	}
	return out
}

func defsFromProto(in []*pdv1.AttributeDef) []app.AttributeDef {
	out := make([]app.AttributeDef, 0, len(in))
	for _, d := range in {
		out = append(out, app.AttributeDef{
			Key: d.GetKey(), Label: d.GetLabel(), DataType: d.GetDataType(),
			Unit: d.GetUnit(), EnumValues: d.GetEnumValues(),
			MatchStrategy: d.GetMatchStrategy(), TolerancePct: d.GetTolerancePct(),
			Level: d.GetLevel(), IsMatchable: d.GetIsMatchable(),
			IsRequired: d.GetIsRequired(), SortOrder: d.GetSortOrder(),
		})
	}
	return out
}
