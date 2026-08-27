package app

import (
	"strings"
	"testing"
)

func validTemplateFields() []InquiryTemplateFieldInput {
	return []InquiryTemplateFieldInput{
		{FieldKey: "product", DisplayName: "品名", IsRequired: true},
		{FieldKey: "quantity", DisplayName: "需求数量", IsRequired: true},
		{FieldKey: "quantity_unit", DisplayName: "计量单位", IsRequired: true, DefaultValue: "MT"},
		{FieldKey: "unit_price", DisplayName: "单价"},
		{FieldKey: "total_price", DisplayName: "总价"},
		{FieldKey: "custom.customer_part_no", DisplayName: "客户料号"},
	}
}

func TestValidateInquiryTemplateFieldsAcceptsCustomColumns(t *testing.T) {
	fields, err := validateInquiryTemplateFields(validTemplateFields())
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 6 || fields[5].SortOrder != 6 || fields[5].DataType != "TEXT" {
		t.Fatalf("unexpected normalization: %#v", fields)
	}
}

func TestValidateInquiryTemplateFieldsRequiresCoreFields(t *testing.T) {
	for _, core := range []string{"product", "quantity", "quantity_unit", "unit_price", "total_price"} {
		fields := validTemplateFields()
		kept := fields[:0]
		for _, field := range fields {
			if field.FieldKey != core {
				kept = append(kept, field)
			}
		}
		if _, err := validateInquiryTemplateFields(kept); err == nil || !strings.Contains(err.Error(), "核心字段") {
			t.Fatalf("removing %s should fail as core field", core)
		}
	}
}

func TestValidateInquiryTemplateFieldsForcesCoreRequired(t *testing.T) {
	fields := validTemplateFields()
	fields[1].IsRequired = false
	fields[3].IsRequired = true // 价格列试图标必填，应被拉回 false
	normalized, err := validateInquiryTemplateFields(fields)
	if err != nil {
		t.Fatal(err)
	}
	if !normalized[1].IsRequired {
		t.Fatal("core field quantity must stay required")
	}
	if normalized[3].IsRequired || normalized[4].IsRequired {
		t.Fatal("price core columns must never be required at inquiry stage")
	}
}

func TestValidateInquiryTemplateFieldsRejectsUnknownKeys(t *testing.T) {
	fields := append(validTemplateFields(), InquiryTemplateFieldInput{FieldKey: "factory_price", DisplayName: "出厂价"})
	if _, err := validateInquiryTemplateFields(fields); err == nil {
		t.Fatal("expected unknown key to be rejected")
	}
}

func TestValidateInquiryTemplateFieldsRejectsDuplicateHeaders(t *testing.T) {
	fields := append(validTemplateFields(), InquiryTemplateFieldInput{FieldKey: "custom.copy", DisplayName: "品名"})
	if _, err := validateInquiryTemplateFields(fields); err == nil {
		t.Fatal("expected duplicate header to be rejected")
	}
}

func TestSteelDetailedTemplateUsesOneMeaningPerDimensionColumn(t *testing.T) {
	fields, err := validateInquiryTemplateFields(steelDetailedInquiryTemplateFields())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"custom.thickness_mm": "厚度(mm)", "custom.wall_thickness_mm": "壁厚(mm)",
		"custom.width_mm": "宽度(mm)", "custom.height_mm": "高度(mm)",
		"custom.diameter_mm": "直径(mm)", "custom.leg1_mm": "边长1(mm)",
		"custom.leg2_mm": "边长2(mm)", "length_or_form": "长度(mm)",
	}
	for _, field := range fields {
		if displayName, ok := want[field.FieldKey]; ok {
			if field.DisplayName != displayName || field.DataType != "NUMBER" {
				t.Fatalf("dimension field = %#v", field)
			}
			delete(want, field.FieldKey)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing detailed dimension fields: %#v", want)
	}
}
