package app

import (
	"reflect"
	"testing"

	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestMissingIntakeReviewFields(t *testing.T) {
	fields := []store.ListInquiryTemplateFieldsRow{
		{FieldKey: "product", DisplayName: "品名", IsRequired: true},
		{FieldKey: "quantity", DisplayName: "需求数量", IsRequired: true},
		{FieldKey: "quantity_unit", DisplayName: "计量单位", IsRequired: true},
		{FieldKey: "custom.customer_part_no", DisplayName: "客户料号", IsRequired: true},
		{FieldKey: "material_standard", DisplayName: "材质", IsRequired: false},
	}
	complete := store.ListSourcingLinesRow{Product: "客户产品", Quantity: "25", QuantityUnit: "MT", CustomFields: []byte(`{"custom.customer_part_no":"CP-001"}`)}
	if missing := missingIntakeReviewFields(complete, fields); len(missing) != 0 {
		t.Fatalf("完整明细不应缺字段: %v", missing)
	}

	incomplete := complete
	incomplete.QuantityUnit = ""
	incomplete.CustomFields = []byte(`{}`)
	want := []string{"计量单位", "客户料号"}
	if got := missingIntakeReviewFields(incomplete, fields); !reflect.DeepEqual(got, want) {
		t.Fatalf("缺失字段=%v, want %v", got, want)
	}
}
