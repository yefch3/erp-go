package app

import "testing"

func TestInquiryTemplateRequiredAndTypedFields(t *testing.T) {
	body := InquiryBody{
		Customer: "客户 A",
		Template: &InquiryTemplateSnapshot{Fields: []InquiryTemplateFieldSnapshot{
			{FieldKey: "product", DisplayName: "产品", IsRequired: true, DataType: "TEXT"},
			{FieldKey: "quantity", DisplayName: "数量", IsRequired: true, DataType: "NUMBER"},
			{FieldKey: "quantity_unit", DisplayName: "单位", IsRequired: true, DataType: "TEXT"},
			{FieldKey: "custom.height_mm", DisplayName: "高度", IsRequired: true, DataType: "NUMBER"},
		}},
		Products: []InquiryProduct{{Product: "方管", Quantity: "2", Unit: "MT", CustomFields: map[string]string{}}},
	}
	if err := validateInquiryBody(body, true); err == nil {
		t.Fatal("missing required template field was accepted")
	}
	body.Products[0].CustomFields["custom.height_mm"] = "not-a-number"
	if err := validateInquiryBody(body, true); err == nil {
		t.Fatal("invalid template number was accepted")
	}
	body.Products[0].CustomFields["custom.height_mm"] = "1250"
	if err := validateInquiryBody(body, true); err != nil {
		t.Fatalf("valid template-driven product was rejected: %v", err)
	}
}

func TestInquiryTemplateDefaultsPreserveEnteredValues(t *testing.T) {
	body := InquiryBody{
		Template: &InquiryTemplateSnapshot{Fields: []InquiryTemplateFieldSnapshot{
			{FieldKey: "quantity_unit", DefaultValue: "MT"},
			{FieldKey: "custom.color", DefaultValue: "蓝色"},
		}},
		Products: []InquiryProduct{{Unit: "PCS", CustomFields: map[string]string{}}},
	}
	applyInquiryTemplateDefaults(&body)
	if body.Products[0].Unit != "PCS" || body.Products[0].CustomFields["custom.color"] != "蓝色" {
		t.Fatalf("unexpected defaults result: %#v", body.Products[0])
	}
}
