package app

// 标准询盘列模板：公司级「标准列」的注册表。邮件模块的标准化输出和采购
// 待复核的表头校验都以租户当前默认模板为准——编辑默认模板即改变 LLM 被
// 要求抽取的列，以及上传的标准文件必须包含的列。
//
// 版本不可变：保存生成新版本，旧版本转为 SUPERSEDED，历史询盘继续引用
// 读取时的列布局。唯一 ACTIVE（按编码）与唯一默认（按租户）由数据库
// 部分唯一索引兜底，这里的事务只负责让常规路径碰不到它们。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

const (
	InquiryTemplateStatusActive     = "ACTIVE"
	InquiryTemplateStatusSuperseded = "SUPERSEDED"
	InquiryTemplateStatusDisabled   = "DISABLED"

	// SystemInquiryTemplateCode 是首次使用时播种的内置布局。它只是种子，
	// 不是只读约束：公司可以直接编辑它（保存为新版本）。
	SystemInquiryTemplateCode = "SYSTEM_DEFAULT"
	// SteelDetailedInquiryTemplateCode 是把不同截面概念完全拆开的内置模板。
	// 它与系统默认模板并存且不自动设为默认，由用户在模板页或邮箱转换时选择。
	SteelDetailedInquiryTemplateCode = "STEEL_DETAILED_DIMENSIONS"
)

// 核心字段承载采购后续流程的硬依赖（建行、比价、需求、价格公式列），任何
// 模板都不得缺少——可以改表头、调顺序、给默认值，但不能删除。其中产品、
// 数量、单位始终必填；单价、总价在询盘阶段留空（价格由工厂报价产生），
// 仅作为固定列存在。
var coreInquiryFieldKeys = []string{"product", "quantity", "quantity_unit", "unit_price", "total_price"}

// 强制必填的只是前三个；两个价格核心列强制非必填。
var requiredInquiryFieldKeys = map[string]bool{"product": true, "quantity": true, "quantity_unit": true}

// 已知业务字段。它们在每个消费侧（邮件抽取、采购解析、RFQ 工作簿）都有
// 固定含义；custom.* 之外的字段名必须来自这张表。
var knownInquiryFieldKeys = map[string]bool{
	"product": true, "material_standard": true, "grade": true,
	"thickness": true, "width": true, "length_or_form": true,
	"surface_requirement": true, "coating": true, "tolerance": true,
	"coil_weight": true, "coil_id": true, "packaging": true, "delivery": true,
	"payment_terms": true, "incoterm": true, "port": true, "quantity": true,
	"quantity_unit": true, "remarks": true, "unit_price": true, "total_price": true,
}

var (
	inquiryTemplateCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,59}$`)
	customInquiryFieldPattern  = regexp.MustCompile(`^custom\.[a-z][a-z0-9_]{0,79}$`)
	inquiryTemplateDataTypes   = map[string]bool{"TEXT": true, "NUMBER": true, "DATE": true}
)

type InquiryTemplateFieldInput struct {
	FieldKey, DisplayName  string
	SortOrder              int32
	IsRequired             bool
	DefaultValue, DataType string
}

type InquiryTemplateInput struct {
	TemplateCode, Name, Description string
	IsDefault                       bool
	Fields                          []InquiryTemplateFieldInput
}

type InquiryTemplateView struct {
	Template store.InquiryTemplate
	Fields   []store.ListInquiryTemplateFieldsRow
}

// systemInquiryTemplateFields 是播种内容，与邮件模块现行 22 列一一对应。
// 截面第二维使用 custom.* 落采购明细的 custom_fields，避免为一个尺寸字段
// 扩大采购核心表结构，同时仍受默认模板、上传校验和 LLM schema 约束。
func systemInquiryTemplateFields() []InquiryTemplateFieldInput {
	type f = InquiryTemplateFieldInput
	return []f{
		{FieldKey: "product", DisplayName: "产品", SortOrder: 1, IsRequired: true, DataType: "TEXT"},
		{FieldKey: "material_standard", DisplayName: "材质/标准", SortOrder: 2, DataType: "TEXT"},
		{FieldKey: "grade", DisplayName: "牌号/等级", SortOrder: 3, DataType: "TEXT"},
		{FieldKey: "thickness", DisplayName: "厚度/壁厚(mm)", SortOrder: 4, DataType: "NUMBER"},
		{FieldKey: "width", DisplayName: "宽度/直径/边长1(mm)", SortOrder: 5, DataType: "NUMBER"},
		{FieldKey: "custom.height_or_leg2", DisplayName: "高度/边长2(mm)", SortOrder: 6, DataType: "NUMBER"},
		{FieldKey: "length_or_form", DisplayName: "长度(mm)", SortOrder: 7, DataType: "NUMBER"},
		{FieldKey: "surface_requirement", DisplayName: "表面要求", SortOrder: 8, DataType: "TEXT"},
		{FieldKey: "coating", DisplayName: "涂层/镀层", SortOrder: 9, DataType: "TEXT"},
		{FieldKey: "tolerance", DisplayName: "公差", SortOrder: 10, DataType: "TEXT"},
		{FieldKey: "coil_weight", DisplayName: "卷重", SortOrder: 11, DataType: "TEXT"},
		{FieldKey: "coil_id", DisplayName: "卷内径", SortOrder: 12, DataType: "TEXT"},
		{FieldKey: "packaging", DisplayName: "包装", SortOrder: 13, DataType: "TEXT"},
		{FieldKey: "delivery", DisplayName: "交期", SortOrder: 14, DataType: "TEXT"},
		{FieldKey: "payment_terms", DisplayName: "付款条件", SortOrder: 15, DataType: "TEXT"},
		{FieldKey: "incoterm", DisplayName: "贸易术语", SortOrder: 16, DataType: "TEXT"},
		{FieldKey: "port", DisplayName: "港口", SortOrder: 17, DataType: "TEXT"},
		{FieldKey: "quantity_unit", DisplayName: "单位", SortOrder: 18, IsRequired: true, DataType: "TEXT"},
		{FieldKey: "remarks", DisplayName: "备注", SortOrder: 19, DataType: "TEXT"},
		{FieldKey: "quantity", DisplayName: "数量", SortOrder: 20, IsRequired: true, DataType: "NUMBER"},
		{FieldKey: "unit_price", DisplayName: "单价", SortOrder: 21, DataType: "NUMBER"},
		{FieldKey: "total_price", DisplayName: "总价", SortOrder: 22, DataType: "NUMBER"},
	}
}

// steelDetailedInquiryTemplateFields 不复用含义相近但业务不同的尺寸列。
// 不适用于某种产品的尺寸在输出中保持空白；额外尺寸放入 custom_fields，
// 因而无需改变采购明细表结构，也能随询盘模板完整保存。
func steelDetailedInquiryTemplateFields() []InquiryTemplateFieldInput {
	type f = InquiryTemplateFieldInput
	return []f{
		{FieldKey: "product", DisplayName: "产品", SortOrder: 1, IsRequired: true, DataType: "TEXT"},
		{FieldKey: "material_standard", DisplayName: "材质/标准", SortOrder: 2, DataType: "TEXT"},
		{FieldKey: "grade", DisplayName: "牌号/等级", SortOrder: 3, DataType: "TEXT"},
		{FieldKey: "custom.thickness_mm", DisplayName: "厚度(mm)", SortOrder: 4, DataType: "NUMBER"},
		{FieldKey: "custom.wall_thickness_mm", DisplayName: "壁厚(mm)", SortOrder: 5, DataType: "NUMBER"},
		{FieldKey: "custom.width_mm", DisplayName: "宽度(mm)", SortOrder: 6, DataType: "NUMBER"},
		{FieldKey: "custom.height_mm", DisplayName: "高度(mm)", SortOrder: 7, DataType: "NUMBER"},
		{FieldKey: "custom.diameter_mm", DisplayName: "直径(mm)", SortOrder: 8, DataType: "NUMBER"},
		{FieldKey: "custom.leg1_mm", DisplayName: "边长1(mm)", SortOrder: 9, DataType: "NUMBER"},
		{FieldKey: "custom.leg2_mm", DisplayName: "边长2(mm)", SortOrder: 10, DataType: "NUMBER"},
		{FieldKey: "length_or_form", DisplayName: "长度(mm)", SortOrder: 11, DataType: "NUMBER"},
		{FieldKey: "surface_requirement", DisplayName: "表面要求", SortOrder: 12, DataType: "TEXT"},
		{FieldKey: "coating", DisplayName: "涂层/镀层", SortOrder: 13, DataType: "TEXT"},
		{FieldKey: "tolerance", DisplayName: "公差", SortOrder: 14, DataType: "TEXT"},
		{FieldKey: "coil_weight", DisplayName: "卷重", SortOrder: 15, DataType: "TEXT"},
		{FieldKey: "coil_id", DisplayName: "卷内径", SortOrder: 16, DataType: "TEXT"},
		{FieldKey: "packaging", DisplayName: "包装", SortOrder: 17, DataType: "TEXT"},
		{FieldKey: "delivery", DisplayName: "交期", SortOrder: 18, DataType: "TEXT"},
		{FieldKey: "payment_terms", DisplayName: "付款条件", SortOrder: 19, DataType: "TEXT"},
		{FieldKey: "incoterm", DisplayName: "贸易术语", SortOrder: 20, DataType: "TEXT"},
		{FieldKey: "port", DisplayName: "港口", SortOrder: 21, DataType: "TEXT"},
		{FieldKey: "quantity_unit", DisplayName: "单位", SortOrder: 22, IsRequired: true, DataType: "TEXT"},
		{FieldKey: "remarks", DisplayName: "备注", SortOrder: 23, DataType: "TEXT"},
		{FieldKey: "quantity", DisplayName: "数量", SortOrder: 24, IsRequired: true, DataType: "NUMBER"},
		{FieldKey: "unit_price", DisplayName: "单价", SortOrder: 25, DataType: "NUMBER"},
		{FieldKey: "total_price", DisplayName: "总价", SortOrder: 26, DataType: "NUMBER"},
	}
}

// IsCoreInquiryField 报告字段是否为五个核心字段之一（产品、数量、单位、
// 单价、总价）。
func IsCoreInquiryField(key string) bool {
	for _, core := range coreInquiryFieldKeys {
		if key == core {
			return true
		}
	}
	return false
}

// EnsureDefaultInquiryTemplate 让「每个租户都有默认模板」成为不变量：
// 首次访问时播种系统布局。并发首访撞唯一索引时按已存在处理。
func (s *Service) EnsureDefaultInquiryTemplate(ctx context.Context, tenantID int64) error {
	if err := s.ensureDefaultInquiryTemplate(ctx, tenantID); err != nil {
		return err
	}
	return s.ensureSteelDetailedInquiryTemplate(ctx, tenantID)
}

func (s *Service) ensureDefaultInquiryTemplate(ctx context.Context, tenantID int64) error {
	current, err := s.q.GetDefaultInquiryTemplate(ctx, tenantID)
	if err == nil {
		view, viewErr := s.templateView(ctx, current)
		if viewErr != nil {
			return viewErr
		}
		if isLegacySystemInquiryTemplate(view) {
			_, saveErr := s.SaveInquiryTemplate(ctx, tenantID, current.ID, InquiryTemplateInput{
				Name: current.Name, Description: "系统内置标准列（毫米尺寸结构）",
				IsDefault: true, Fields: systemInquiryTemplateFields(),
			}, Operator{Name: "system"})
			if saveErr != nil {
				// 并发请求可能已完成同一升级；重新读取确认即可。
				latest, latestErr := s.q.GetDefaultInquiryTemplate(ctx, tenantID)
				if latestErr == nil {
					latestView, latestViewErr := s.templateView(ctx, latest)
					if latestViewErr == nil && !isLegacySystemInquiryTemplate(latestView) {
						return nil
					}
				}
				return saveErr
			}
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, createErr := q.CreateInquiryTemplate(ctx, store.CreateInquiryTemplateParams{
			TenantID: tenantID, TemplateCode: SystemInquiryTemplateCode, Version: 1,
			Name: "系统标准询盘模板", Description: "系统内置标准列",
			Status: InquiryTemplateStatusActive, IsDefault: true,
			CreatedByName: "system",
		})
		if createErr != nil {
			return createErr
		}
		for _, field := range systemInquiryTemplateFields() {
			if err := q.CreateInquiryTemplateField(ctx, store.CreateInquiryTemplateFieldParams{
				TenantID: tenantID, TemplateID: id, FieldKey: field.FieldKey,
				DisplayName: field.DisplayName, SortOrder: field.SortOrder,
				IsRequired: field.IsRequired, DefaultValue: field.DefaultValue,
				DataType: field.DataType, IsCustom: strings.HasPrefix(field.FieldKey, "custom."),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "inquiry_templates_default_idx" {
			return nil // 另一个请求刚刚播种完成
		}
		return err
	}
	return nil
}

func (s *Service) ensureSteelDetailedInquiryTemplate(ctx context.Context, tenantID int64) error {
	rows, err := s.q.ListInquiryTemplates(ctx, tenantID)
	if err != nil {
		return err
	}
	// ACTIVE 和 DISABLED 都算已经播种；用户停用后不能被系统悄悄恢复。
	for _, row := range rows {
		if row.TemplateCode == SteelDetailedInquiryTemplateCode {
			return nil
		}
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, createErr := q.CreateInquiryTemplate(ctx, store.CreateInquiryTemplateParams{
			TenantID: tenantID, TemplateCode: SteelDetailedInquiryTemplateCode, Version: 1,
			Name: "钢材详细尺寸模板", Description: "厚度、壁厚、宽度、高度、直径和边长分别独立；不适用的尺寸留空",
			Status: InquiryTemplateStatusActive, IsDefault: false, CreatedByName: "system",
		})
		if createErr != nil {
			return createErr
		}
		for _, field := range steelDetailedInquiryTemplateFields() {
			if err := q.CreateInquiryTemplateField(ctx, store.CreateInquiryTemplateFieldParams{
				TenantID: tenantID, TemplateID: id, FieldKey: field.FieldKey,
				DisplayName: field.DisplayName, SortOrder: field.SortOrder,
				IsRequired: field.IsRequired, DefaultValue: field.DefaultValue,
				DataType: field.DataType, IsCustom: strings.HasPrefix(field.FieldKey, "custom."),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.ConstraintName == "inquiry_templates_active_code_idx" ||
			pgErr.ConstraintName == "inquiry_templates_tenant_id_template_code_version_key") {
			return nil // 另一个请求刚刚完成播种
		}
	}
	return err
}

func isLegacySystemInquiryTemplate(view InquiryTemplateView) bool {
	if view.Template.TemplateCode != SystemInquiryTemplateCode || view.Template.Version != 1 || view.Template.CreatedByName != "system" || len(view.Fields) != 21 {
		return false
	}
	legacyKeys := []string{
		"product", "material_standard", "grade", "thickness", "width", "length_or_form",
		"surface_requirement", "coating", "tolerance", "coil_weight", "coil_id", "packaging",
		"delivery", "payment_terms", "incoterm", "port", "quantity_unit", "remarks", "quantity",
		"unit_price", "total_price",
	}
	for i, field := range view.Fields {
		if field.FieldKey != legacyKeys[i] {
			return false
		}
	}
	return true
}

func (s *Service) templateView(ctx context.Context, row store.InquiryTemplate) (InquiryTemplateView, error) {
	fields, err := s.q.ListInquiryTemplateFields(ctx, store.ListInquiryTemplateFieldsParams{
		TenantID: row.TenantID, TemplateID: row.ID,
	})
	if err != nil {
		return InquiryTemplateView{}, err
	}
	return InquiryTemplateView{Template: row, Fields: fields}, nil
}

func (s *Service) ListInquiryTemplates(ctx context.Context, tenantID int64) ([]InquiryTemplateView, error) {
	if err := s.EnsureDefaultInquiryTemplate(ctx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListInquiryTemplates(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	views := make([]InquiryTemplateView, 0, len(rows))
	for _, row := range rows {
		view, err := s.templateView(ctx, row)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) GetInquiryTemplate(ctx context.Context, tenantID, id int64) (InquiryTemplateView, error) {
	row, err := s.q.GetInquiryTemplate(ctx, store.GetInquiryTemplateParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InquiryTemplateView{}, apierr.NotFound("SC_TEMPLATE_NOT_FOUND", "询盘模板不存在")
		}
		return InquiryTemplateView{}, err
	}
	return s.templateView(ctx, row)
}

// GetDefaultInquiryTemplate 是消费侧的读取入口：邮件转换与待复核解析都取
// 当前默认模板的完整字段。
func (s *Service) GetDefaultInquiryTemplate(ctx context.Context, tenantID int64) (InquiryTemplateView, error) {
	if err := s.EnsureDefaultInquiryTemplate(ctx, tenantID); err != nil {
		return InquiryTemplateView{}, err
	}
	row, err := s.q.GetDefaultInquiryTemplate(ctx, tenantID)
	if err != nil {
		return InquiryTemplateView{}, err
	}
	return s.templateView(ctx, row)
}

func validateInquiryTemplateFields(fields []InquiryTemplateFieldInput) ([]InquiryTemplateFieldInput, error) {
	if len(fields) == 0 {
		return nil, apierr.Invalid("SC_TEMPLATE_FIELDS_REQUIRED", "模板至少需要一列")
	}
	seenKeys := make(map[string]bool, len(fields))
	seenNames := make(map[string]bool, len(fields))
	normalized := make([]InquiryTemplateFieldInput, 0, len(fields))
	for i, field := range fields {
		field.FieldKey = strings.TrimSpace(field.FieldKey)
		field.DisplayName = strings.TrimSpace(field.DisplayName)
		field.DefaultValue = strings.TrimSpace(field.DefaultValue)
		field.DataType = strings.ToUpper(strings.TrimSpace(field.DataType))
		if field.DataType == "" {
			field.DataType = "TEXT"
		}
		switch {
		case knownInquiryFieldKeys[field.FieldKey]:
		case customInquiryFieldPattern.MatchString(field.FieldKey):
		default:
			return nil, apierr.Invalid("SC_TEMPLATE_FIELD_KEY", "字段标识必须是系统已知字段或 custom.* 自定义字段").
				WithMeta("field_key", field.FieldKey)
		}
		if field.DisplayName == "" {
			return nil, apierr.Invalid("SC_TEMPLATE_FIELD_NAME", "每一列都必须填写 Excel 表头").
				WithMeta("field_key", field.FieldKey)
		}
		if !inquiryTemplateDataTypes[field.DataType] {
			return nil, apierr.Invalid("SC_TEMPLATE_FIELD_TYPE", "列类型只能是 TEXT、NUMBER 或 DATE").
				WithMeta("field_key", field.FieldKey)
		}
		if requiredInquiryFieldKeys[field.FieldKey] {
			field.IsRequired = true
		}
		// 价格核心列在询盘阶段永远留空，不能标必填。
		if field.FieldKey == "unit_price" || field.FieldKey == "total_price" {
			field.IsRequired = false
		}
		if seenKeys[field.FieldKey] {
			return nil, apierr.Invalid("SC_TEMPLATE_FIELD_DUP", "字段标识重复："+field.FieldKey)
		}
		nameKey := strings.ToLower(field.DisplayName)
		if seenNames[nameKey] {
			return nil, apierr.Invalid("SC_TEMPLATE_NAME_DUP", "Excel 表头重复："+field.DisplayName)
		}
		seenKeys[field.FieldKey] = true
		seenNames[nameKey] = true
		if field.SortOrder == 0 {
			field.SortOrder = int32(i + 1)
		}
		normalized = append(normalized, field)
	}
	for _, core := range coreInquiryFieldKeys {
		if !seenKeys[core] {
			return nil, apierr.Invalid("SC_TEMPLATE_CORE_MISSING", "产品、数量、单位、单价和总价是不可删除的核心字段").
				WithMeta("field_key", core)
		}
	}
	return normalized, nil
}

func (s *Service) createTemplateWithFields(ctx context.Context, tx pgx.Tx, tenantID int64, in InquiryTemplateInput, version int32, op Operator) (int64, error) {
	q := s.q.WithTx(tx)
	if in.IsDefault {
		if err := q.ClearDefaultInquiryTemplate(ctx, tenantID); err != nil {
			return 0, err
		}
	}
	id, err := q.CreateInquiryTemplate(ctx, store.CreateInquiryTemplateParams{
		TenantID: tenantID, TemplateCode: in.TemplateCode, Version: version,
		Name: in.Name, Description: in.Description, Status: InquiryTemplateStatusActive,
		IsDefault: in.IsDefault, CreatedBy: op.ID, CreatedByName: op.Name,
	})
	if err != nil {
		return 0, err
	}
	for _, field := range in.Fields {
		if err := q.CreateInquiryTemplateField(ctx, store.CreateInquiryTemplateFieldParams{
			TenantID: tenantID, TemplateID: id, FieldKey: field.FieldKey,
			DisplayName: field.DisplayName, SortOrder: field.SortOrder,
			IsRequired: field.IsRequired, DefaultValue: field.DefaultValue,
			DataType: field.DataType,
			IsCustom: !knownInquiryFieldKeys[field.FieldKey],
		}); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (s *Service) CreateInquiryTemplate(ctx context.Context, tenantID int64, in InquiryTemplateInput, op Operator) (InquiryTemplateView, error) {
	in.TemplateCode = strings.ToUpper(strings.TrimSpace(in.TemplateCode))
	if !inquiryTemplateCodePattern.MatchString(in.TemplateCode) {
		return InquiryTemplateView{}, apierr.Invalid("SC_TEMPLATE_CODE", "模板编码须为大写字母、数字或下划线")
	}
	if strings.TrimSpace(in.Name) == "" {
		return InquiryTemplateView{}, apierr.Invalid("SC_TEMPLATE_NAME_REQUIRED", "请填写模板名称")
	}
	fields, err := validateInquiryTemplateFields(in.Fields)
	if err != nil {
		return InquiryTemplateView{}, err
	}
	in.Fields = fields
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var txErr error
		id, txErr = s.createTemplateWithFields(ctx, tx, tenantID, in, 1, op)
		return txErr
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "inquiry_templates_active_code_idx" {
			return InquiryTemplateView{}, apierr.Conflict("SC_TEMPLATE_CODE_DUP", "相同编码的模板已存在")
		}
		return InquiryTemplateView{}, err
	}
	return s.GetInquiryTemplate(ctx, tenantID, id)
}

// SaveInquiryTemplate 把编辑落成下一个版本：同事务内旧 ACTIVE 版本转
// SUPERSEDED，新版本生效；默认标记跟随被编辑的版本，不会在版本轮换中丢失。
func (s *Service) SaveInquiryTemplate(ctx context.Context, tenantID, id int64, in InquiryTemplateInput, op Operator) (InquiryTemplateView, error) {
	source, err := s.q.GetInquiryTemplate(ctx, store.GetInquiryTemplateParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InquiryTemplateView{}, apierr.NotFound("SC_TEMPLATE_NOT_FOUND", "询盘模板不存在")
		}
		return InquiryTemplateView{}, err
	}
	if source.Status != InquiryTemplateStatusActive {
		return InquiryTemplateView{}, apierr.Conflict("SC_TEMPLATE_NOT_ACTIVE", "只有生效中的模板可以编辑")
	}
	if strings.TrimSpace(in.Name) == "" {
		return InquiryTemplateView{}, apierr.Invalid("SC_TEMPLATE_NAME_REQUIRED", "请填写模板名称")
	}
	fields, err := validateInquiryTemplateFields(in.Fields)
	if err != nil {
		return InquiryTemplateView{}, err
	}
	in.TemplateCode = source.TemplateCode
	in.Fields = fields
	in.IsDefault = in.IsDefault || source.IsDefault

	var newID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		maxVersion, err := q.MaxInquiryTemplateVersion(ctx, store.MaxInquiryTemplateVersionParams{
			TenantID: tenantID, TemplateCode: source.TemplateCode,
		})
		if err != nil {
			return err
		}
		if _, err := q.SupersedeActiveInquiryTemplate(ctx, store.SupersedeActiveInquiryTemplateParams{
			TenantID: tenantID, TemplateCode: source.TemplateCode,
		}); err != nil {
			return err
		}
		newID, err = s.createTemplateWithFields(ctx, tx, tenantID, in, maxVersion+1, op)
		return err
	})
	if err != nil {
		return InquiryTemplateView{}, err
	}
	return s.GetInquiryTemplate(ctx, tenantID, newID)
}

func (s *Service) SetInquiryTemplateStatus(ctx context.Context, tenantID, id int64, status string, op Operator) (InquiryTemplateView, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != InquiryTemplateStatusActive && status != InquiryTemplateStatusDisabled {
		return InquiryTemplateView{}, apierr.Invalid("SC_TEMPLATE_STATUS", "状态只能是 ACTIVE 或 DISABLED")
	}
	row, err := s.q.GetInquiryTemplate(ctx, store.GetInquiryTemplateParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InquiryTemplateView{}, apierr.NotFound("SC_TEMPLATE_NOT_FOUND", "询盘模板不存在")
		}
		return InquiryTemplateView{}, err
	}
	if status == InquiryTemplateStatusDisabled && row.IsDefault {
		return InquiryTemplateView{}, apierr.Conflict("SC_TEMPLATE_DISABLE_DEFAULT", "默认模板不能停用，请先将其他模板设为默认")
	}
	affected, err := s.q.SetInquiryTemplateStatus(ctx, store.SetInquiryTemplateStatusParams{
		TenantID: tenantID, ID: id, Status: status,
	})
	if err != nil {
		return InquiryTemplateView{}, err
	}
	if affected == 0 {
		return InquiryTemplateView{}, apierr.Conflict("SC_TEMPLATE_STATUS_CHANGED", fmt.Sprintf("模板状态已变化（当前 %s），请刷新后重试", row.Status))
	}
	return s.GetInquiryTemplate(ctx, tenantID, id)
}

func (s *Service) SetDefaultInquiryTemplate(ctx context.Context, tenantID, id int64, op Operator) (InquiryTemplateView, error) {
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ClearDefaultInquiryTemplate(ctx, tenantID); err != nil {
			return err
		}
		affected, err := q.SetDefaultInquiryTemplate(ctx, store.SetDefaultInquiryTemplateParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if affected == 0 {
			return apierr.Conflict("SC_TEMPLATE_NOT_ACTIVE", "只有生效中的模板可以设为默认")
		}
		return nil
	})
	if err != nil {
		return InquiryTemplateView{}, err
	}
	return s.GetInquiryTemplate(ctx, tenantID, id)
}
