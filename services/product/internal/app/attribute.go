package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/product/internal/store"
)

// Attribute levels. Shared across every variant of a product → PRODUCT;
// tells one variant from another → SKU. Where the line falls differs by
// business, which is why it is stored rather than decided in code.
const (
	LevelProduct = "PRODUCT"
	LevelSKU     = "SKU"
)

// AttributeDef is one field of a category's spec, resolved and ready to use.
type AttributeDef struct {
	Key           string
	Label         string
	DataType      string // DIMENSION / NUMBER / ENUM / TEXT / RANGE
	Unit          string
	EnumValues    []string
	MatchStrategy string // EXACT / TOLERANCE / SYNONYM / FUZZY / OVERLAP
	TolerancePct  string
	Level         string
	IsMatchable   bool
	IsRequired    bool
	SortOrder     int32
	// Which category in the ancestry contributed this definition. Shown in
	// the editor so somebody can tell an inherited field from a local one.
	FromCategoryID int64
}

// ResolveDefs collects a category's attributes along its ancestry.
//
// Categories carry a materialised path, so the whole lineage is one parse and
// one query. A child definition of the same key replaces the parent's: a
// subcategory can add fields or tighten a tolerance without restating
// everything above it.
//
// Returns nil for a category with no template anywhere in its lineage. That
// is not an error — it is the level-0 state every tenant starts in, and every
// caller has to keep working in it.
func (s *Service) ResolveDefs(ctx context.Context, tenantID, categoryID int64) ([]AttributeDef, error) {
	if categoryID == 0 {
		return nil, nil
	}
	category, err := s.q.GetCategory(ctx, store.GetCategoryParams{TenantID: tenantID, ID: categoryID})
	if err == pgx.ErrNoRows {
		return nil, apierr.NotFound("PD_CATEGORY_NOT_FOUND", "分类不存在")
	}
	if err != nil {
		return nil, err
	}
	lineage := lineageOf(category.Path, categoryID)
	rows, err := s.q.DefsForLineage(ctx, store.DefsForLineageParams{
		TenantID: tenantID, CategoryIds: lineage,
	})
	if err != nil {
		return nil, err
	}
	depth := make(map[int64]int, len(lineage))
	for i, id := range lineage {
		depth[id] = i
	}
	// Rows arrive shallow → deep, so a later row simply overwrites an
	// earlier one with the same key.
	byKey := make(map[string]AttributeDef, len(rows))
	order := make([]string, 0, len(rows))
	for _, r := range rows {
		if _, seen := byKey[r.Key]; !seen {
			order = append(order, r.Key)
		}
		byKey[r.Key] = AttributeDef{
			Key: r.Key, Label: r.Label, DataType: r.DataType, Unit: r.Unit,
			EnumValues: decodeEnum(r.EnumValues), MatchStrategy: r.MatchStrategy,
			TolerancePct: r.TolerancePct, Level: r.Level,
			IsMatchable: r.IsMatchable, IsRequired: r.IsRequired,
			SortOrder: r.SortOrder, FromCategoryID: r.CategoryID,
		}
	}
	out := make([]AttributeDef, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	// Ancestry first, then each template's own order. Both templates number
	// their fields from zero, so sorting on sort_order alone interleaves
	// inherited and local fields into a form nobody can read.
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := depth[out[i].FromCategoryID], depth[out[j].FromCategoryID]
		if di != dj {
			return di < dj
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

// lineageOf turns "/1/4/9/" into [1 4 9], root first. A path that has not
// been materialised yet still yields the category itself, so a flat tree
// works without special-casing.
func lineageOf(path string, categoryID int64) []int64 {
	ids := make([]int64, 0, 4)
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part == "" {
			continue
		}
		if id, err := strconv.ParseInt(part, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if id == categoryID {
			return ids
		}
	}
	return append(ids, categoryID)
}

func decodeEnum(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// TemplateInput is a category's attribute template, saved whole.
type TemplateInput struct {
	CategoryID int64
	Name       string
	Defs       []AttributeDef
}

// SaveTemplate replaces a category's own definitions wholesale.
//
// Wholesale because a template is edited as a form, not patched field by
// field, and because a partial update has no sensible meaning for the
// ordering. Inherited definitions are untouched: they belong to the ancestor
// that declared them.
func (s *Service) SaveTemplate(ctx context.Context, tenantID int64, in TemplateInput) error {
	if in.CategoryID == 0 {
		return apierr.Invalid("PD_CATEGORY_REQUIRED", "请选择分类")
	}
	seen := make(map[string]bool, len(in.Defs))
	for i, d := range in.Defs {
		if d.Key == "" || d.Label == "" {
			return apierr.Invalid("PD_ATTR_FIELDS_REQUIRED", "属性的字段名和显示名必填")
		}
		if !validAttrKey(d.Key) {
			return apierr.Invalid("PD_ATTR_KEY_INVALID",
				"字段名只能用小写字母、数字和下划线，且以字母开头").WithMeta("key", d.Key)
		}
		if seen[d.Key] {
			return apierr.Invalid("PD_ATTR_KEY_DUPLICATE", "字段名重复").WithMeta("key", d.Key)
		}
		seen[d.Key] = true
		if !validDataType(d.DataType) {
			return apierr.Invalid("PD_ATTR_TYPE_INVALID", "属性类型不合法").WithMeta("key", d.Key)
		}
		if d.Level != LevelProduct && d.Level != LevelSKU {
			return apierr.Invalid("PD_ATTR_LEVEL_INVALID", "属性层级只能是产品或规格").
				WithMeta("key", d.Key)
		}
		// A tolerance without a number to apply it to is a configuration
		// mistake that would silently never match anything.
		if d.MatchStrategy == "TOLERANCE" {
			if d.DataType != "DIMENSION" && d.DataType != "NUMBER" {
				return apierr.Invalid("PD_ATTR_TOLERANCE_NEEDS_NUMBER",
					"公差匹配只能用在数值类型上").WithMeta("key", d.Key)
			}
			if _, err := decimal.NewFromString(d.TolerancePct); err != nil {
				return apierr.Invalid("PD_ATTR_TOLERANCE_REQUIRED",
					"选了公差匹配就要填公差百分比").WithMeta("key", d.Key)
			}
		}
		in.Defs[i].SortOrder = int32(i)
	}

	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		templateID, err := q.UpsertAttributeTemplate(ctx, store.UpsertAttributeTemplateParams{
			TenantID: tenantID, CategoryID: in.CategoryID, Name: in.Name,
		})
		if err != nil {
			return err
		}
		if err := q.DeleteDefsOfTemplate(ctx, store.DeleteDefsOfTemplateParams{
			TenantID: tenantID, TemplateID: templateID,
		}); err != nil {
			return err
		}
		for _, d := range in.Defs {
			enum, err := json.Marshal(orEmpty(d.EnumValues))
			if err != nil {
				return err
			}
			if err := q.AddAttributeDef(ctx, store.AddAttributeDefParams{
				TenantID: tenantID, TemplateID: templateID,
				Key: d.Key, Label: d.Label, DataType: d.DataType, Unit: d.Unit,
				EnumValues: enum, MatchStrategy: orDefault(d.MatchStrategy, "EXACT"),
				TolerancePct: d.TolerancePct, Level: d.Level,
				IsMatchable: d.IsMatchable, IsRequired: d.IsRequired,
				SortOrder: d.SortOrder,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// OwnDefs returns only what this category declares itself, for the editor.
func (s *Service) OwnDefs(ctx context.Context, tenantID, categoryID int64) (string, []AttributeDef, error) {
	tpl, err := s.q.GetTemplateByCategory(ctx, store.GetTemplateByCategoryParams{
		TenantID: tenantID, CategoryID: categoryID,
	})
	if err == pgx.ErrNoRows {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	rows, err := s.q.ListDefsOfTemplate(ctx, store.ListDefsOfTemplateParams{
		TenantID: tenantID, TemplateID: tpl.ID,
	})
	if err != nil {
		return "", nil, err
	}
	out := make([]AttributeDef, 0, len(rows))
	for _, r := range rows {
		out = append(out, AttributeDef{
			Key: r.Key, Label: r.Label, DataType: r.DataType, Unit: r.Unit,
			EnumValues: decodeEnum(r.EnumValues), MatchStrategy: r.MatchStrategy,
			TolerancePct: r.TolerancePct, Level: r.Level,
			IsMatchable: r.IsMatchable, IsRequired: r.IsRequired,
			SortOrder: r.SortOrder, FromCategoryID: categoryID,
		})
	}
	return tpl.Name, out, nil
}

// validateValues checks one level's values against the resolved definitions.
//
// Unknown keys pass through untouched. That is deliberate: a tenant with no
// template has nothing but unknown keys, and refusing them would make the
// whole catalogue unwritable for anyone who has not configured a template.
// What is defined gets checked; what is not gets stored as-is.
func validateValues(defs []AttributeDef, values map[string]any, level string) error {
	for _, d := range defs {
		if d.Level != level {
			continue
		}
		raw, present := values[d.Key]
		if !present || raw == nil || raw == "" {
			if d.IsRequired {
				return apierr.Invalid("PD_ATTR_REQUIRED", "缺少必填属性："+d.Label).
					WithMeta("key", d.Key)
			}
			continue
		}
		switch d.DataType {
		case "DIMENSION", "NUMBER", "RANGE":
			if _, err := toNumber(raw); err != nil {
				return apierr.Invalid("PD_ATTR_NOT_NUMBER",
					d.Label+" 必须是数字").WithMeta("key", d.Key, "value", fmt.Sprint(raw))
			}
		case "ENUM":
			// An empty value list means the enum has not been filled in yet;
			// accepting anything keeps a half-configured template usable.
			if len(d.EnumValues) == 0 {
				continue
			}
			got := fmt.Sprint(raw)
			if !contains(d.EnumValues, got) {
				return apierr.Invalid("PD_ATTR_NOT_IN_ENUM",
					d.Label+" 不在可选值里").WithMeta(
					"key", d.Key, "value", got,
					"allowed", strings.Join(d.EnumValues, " / "))
			}
		}
	}
	return nil
}

// signatureOf renders the matchable SKU-level values into one canonical,
// key-sorted string. Two variants of a product that render the same are the
// same variant, which the unique index then refuses.
//
// Only SKU-level keys take part: the index is scoped by product_id, and the
// product already pins every PRODUCT-level value.
func signatureOf(defs []AttributeDef, values map[string]any) string {
	parts := make([]string, 0, len(defs))
	for _, d := range defs {
		if d.Level != LevelSKU || !d.IsMatchable {
			continue
		}
		raw, ok := values[d.Key]
		if !ok || raw == nil || raw == "" {
			continue
		}
		v := fmt.Sprint(raw)
		// Normalise numbers so 3.0 and 3.00 collide, which is the whole point.
		if n, err := toNumber(raw); err == nil {
			v = n.String()
		}
		parts = append(parts, d.Key+"="+v)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// SetProductAttributes validates and stores a product's shared attributes.
func (s *Service) SetProductAttributes(ctx context.Context, tenantID, productID int64, values map[string]any, operatorID int64) error {
	product, err := s.q.GetProduct(ctx, store.GetProductParams{TenantID: tenantID, ID: productID})
	if err == pgx.ErrNoRows {
		return apierr.NotFound("PD_PRODUCT_NOT_FOUND", "产品不存在")
	}
	if err != nil {
		return err
	}
	defs, err := s.ResolveDefs(ctx, tenantID, product.CategoryID)
	if err != nil {
		return err
	}
	if err := validateValues(defs, values, LevelProduct); err != nil {
		return err
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	n, err := s.q.SetProductAttributes(ctx, store.SetProductAttributesParams{
		TenantID: tenantID, ID: productID, Attributes: encoded, UpdatedBy: operatorID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("PD_PRODUCT_NOT_FOUND", "产品不存在")
	}
	return nil
}

// SetSkuAttributes validates, stores, and recomputes the uniqueness signature.
func (s *Service) SetSkuAttributes(ctx context.Context, tenantID, skuID int64, values map[string]any, spec string) error {
	sku, err := s.q.GetSku(ctx, store.GetSkuParams{TenantID: tenantID, ID: skuID})
	if err == pgx.ErrNoRows {
		return apierr.NotFound("PD_SKU_NOT_FOUND", "规格不存在")
	}
	if err != nil {
		return err
	}
	product, err := s.q.GetProduct(ctx, store.GetProductParams{TenantID: tenantID, ID: sku.ProductID})
	if err != nil {
		return err
	}
	defs, err := s.ResolveDefs(ctx, tenantID, product.CategoryID)
	if err != nil {
		return err
	}
	if err := validateValues(defs, values, LevelSKU); err != nil {
		return err
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	n, err := s.q.SetSkuAttributes(ctx, store.SetSkuAttributesParams{
		TenantID: tenantID, ID: skuID, Attributes: encoded, Spec: spec,
		AttrSignature: signatureOf(defs, values),
	})
	if err != nil {
		// The only unique constraint this statement can trip is the
		// signature index — it writes nothing else that is constrained.
		return translateUnique(err, "PD_SKU_SPEC_DUPLICATE",
			"这个产品下已经有规格完全相同的 SKU 了——重复规格会让匹配返回两个一模一样的候选")
	}
	if n == 0 {
		return apierr.NotFound("PD_SKU_NOT_FOUND", "规格不存在")
	}
	return nil
}

func validAttrKey(key string) bool {
	if key == "" || !(key[0] >= 'a' && key[0] <= 'z') {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' {
			return false
		}
	}
	return true
}

func validDataType(t string) bool {
	switch t {
	case "DIMENSION", "NUMBER", "ENUM", "TEXT", "RANGE":
		return true
	}
	return false
}

func toNumber(v any) (decimal.Decimal, error) {
	switch n := v.(type) {
	case float64:
		return decimal.NewFromFloat(n), nil
	case int64:
		return decimal.NewFromInt(n), nil
	case json.Number:
		return decimal.NewFromString(n.String())
	}
	return decimal.NewFromString(strings.TrimSpace(fmt.Sprint(v)))
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
