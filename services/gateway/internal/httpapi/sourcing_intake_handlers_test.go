package httpapi

import (
	"mime/multipart"
	"strings"
	"testing"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// 与系统种子模板一致的 22 列字段表；intake 解析的列定义来自默认模板。
func testInquiryFields() []standardizedInquiryField {
	names := []struct{ key, display string }{
		{"product", "产品"}, {"material_standard", "材质/标准"}, {"grade", "牌号/等级"},
		{"thickness", "厚度/壁厚(mm)"}, {"width", "宽度/直径/边长1(mm)"}, {"custom.height_or_leg2", "高度/边长2(mm)"}, {"length_or_form", "长度(mm)"},
		{"surface_requirement", "表面要求"}, {"coating", "涂层/镀层"}, {"tolerance", "公差"},
		{"coil_weight", "卷重"}, {"coil_id", "卷内径"}, {"packaging", "包装"}, {"delivery", "交期"},
		{"payment_terms", "付款条件"}, {"incoterm", "贸易术语"}, {"port", "港口"},
		{"quantity_unit", "单位"}, {"remarks", "备注"}, {"quantity", "数量"},
		{"unit_price", "单价"}, {"total_price", "总价"},
	}
	fields := make([]standardizedInquiryField, 0, len(names))
	for _, item := range names {
		fields = append(fields, standardizedInquiryField{Key: item.key, Header: item.display})
	}
	return fields
}

const testInquiryHeader = "产品,材质/标准,牌号/等级,厚度/壁厚(mm),宽度/直径/边长1(mm),高度/边长2(mm),长度(mm),表面要求,涂层/镀层,公差,卷重,卷内径,包装,交期,付款条件,贸易术语,港口,单位,备注,数量,单价,总价"

// 按 22 列表头的位置构造一行，免得手数逗号。
func testInquiryRow(values map[int]string) string {
	row := make([]string, 22)
	for i, value := range values {
		row[i] = value
	}
	return strings.Join(row, ",")
}

func TestParseStandardizedInquiryCSV(t *testing.T) {
	header := &multipart.FileHeader{Filename: "standard-inquiry.csv"}
	row := testInquiryRow(map[int]string{0: "冷轧卷", 1: "ASTM A1008", 2: "CS-B", 3: "1.2", 4: "1250", 13: "2026-09-01", 16: "上海", 17: "MT", 19: "20"})
	data := []byte(testInquiryHeader + "\n" + row + "\n")
	lines, err := parseStandardizedInquiry(header, data, testInquiryFields())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	line := lines[0]
	if line.GetProduct() != "冷轧卷" || line.GetMaterialStandard() != "ASTM A1008" || line.GetQuantity() != "20" || line.GetQuantityUnit() != "MT" {
		t.Fatalf("unexpected parsed line: %+v", line)
	}
}

func TestParseStandardizedInquiryMapsCustomColumns(t *testing.T) {
	fields := append(testInquiryFields(), standardizedInquiryField{Key: "custom.customer_part_no", Header: "客户料号"})
	row := make([]string, 23)
	row[0] = "冷轧卷"
	row[17] = "MT"
	row[19] = "20"
	row[22] = "CP-99887"
	data := []byte(testInquiryHeader + ",客户料号\n" + strings.Join(row, ",") + "\n")
	lines, err := parseStandardizedInquiry(&multipart.FileHeader{Filename: "custom.csv"}, data, fields)
	if err != nil {
		t.Fatal(err)
	}
	if got := lines[0].GetCustomFields()["custom.customer_part_no"]; got != "CP-99887" {
		t.Fatalf("custom column should land in custom_fields, got %q", got)
	}
}

func TestParseStandardizedInquiryRejectsHeaderMismatch(t *testing.T) {
	data := []byte("产品,材质/标准,牌号/等级,厚度,宽度,长度/形式,表面要求,涂层/镀层,公差,卷重,卷内径,包装,交期,付款条件,贸易术语,港口,需求数量,单位,备注,额外规格\n镀锌钢卷,,,,,,,,,,,,,,,,25,MT,,测试\n")
	_, err := parseStandardizedInquiry(&multipart.FileHeader{Filename: "mismatch.csv"}, data, testInquiryFields())
	if err == nil || !strings.Contains(err.Error(), "表头与系统标准不一致") ||
		!strings.Contains(err.Error(), "缺少列") || !strings.Contains(err.Error(), "数量") || !strings.Contains(err.Error(), "非标准列") {
		t.Fatalf("expected detailed mismatch error, got %v", err)
	}
}

func TestParseStandardizedInquiryRejectsDuplicateHeader(t *testing.T) {
	_, err := validateInquiryHeaders([]string{"产品", "数量", "数量"}, testInquiryFields())
	if err == nil || !strings.Contains(err.Error(), "重复列：数量") {
		t.Fatalf("expected duplicate header error, got %v", err)
	}
}

func TestParseStandardizedInquiryRejectsUnknownFile(t *testing.T) {
	_, err := parseStandardizedInquiry(&multipart.FileHeader{Filename: "raw-email.txt"}, []byte("hello"), testInquiryFields())
	if err == nil {
		t.Fatal("expected unsupported file error")
	}
}

func TestParseOptionalManualInquiryTemplateID(t *testing.T) {
	if id, err := parseOptionalManualInquiryTemplateID(" 42 "); err != nil || id != 42 {
		t.Fatalf("valid template id: id=%d err=%v", id, err)
	}
	if id, err := parseOptionalManualInquiryTemplateID(""); err != nil || id != 0 {
		t.Fatalf("empty id should enable automatic recognition: id=%d err=%v", id, err)
	}
	for _, value := range []string{"0", "not-a-number"} {
		if _, err := parseOptionalManualInquiryTemplateID(value); err == nil {
			t.Fatalf("template id %q should be rejected", value)
		}
	}
}

func TestRecognizeInquiryTemplateByHeaders(t *testing.T) {
	templates := []*prv1.InquiryTemplate{
		{Id: 11, Name: "钢材询盘", Status: "ACTIVE", Fields: []*prv1.InquiryTemplateField{
			{FieldKey: "product", DisplayName: "产品", SortOrder: 1},
			{FieldKey: "quantity", DisplayName: "数量", SortOrder: 2},
			{FieldKey: "quantity_unit", DisplayName: "单位", SortOrder: 3},
		}},
		{Id: 12, Name: "食品询盘", Status: "ACTIVE", Fields: []*prv1.InquiryTemplateField{
			{FieldKey: "product", DisplayName: "品名", SortOrder: 1},
			{FieldKey: "quantity", DisplayName: "需求数量", SortOrder: 2},
			{FieldKey: "quantity_unit", DisplayName: "计量单位", SortOrder: 3},
		}},
	}
	id, err := recognizeInquiryTemplate(
		&multipart.FileHeader{Filename: "food.csv"},
		[]byte("计量单位,品名,需求数量\n箱,冻干草莓,100\n"),
		templates,
	)
	if err != nil || id != 12 {
		t.Fatalf("recognized id=%d err=%v, want 12", id, err)
	}
}

func TestRecognizeInquiryTemplatePrefersDefaultWhenHeadersDuplicate(t *testing.T) {
	fields := []*prv1.InquiryTemplateField{
		{FieldKey: "product", DisplayName: "产品", SortOrder: 1},
		{FieldKey: "quantity", DisplayName: "数量", SortOrder: 2},
	}
	templates := []*prv1.InquiryTemplate{
		{Id: 21, Status: "ACTIVE", Fields: fields},
		{Id: 22, Status: "ACTIVE", IsDefault: true, Fields: fields},
	}
	id, err := recognizeInquiryTemplate(&multipart.FileHeader{Filename: "same.csv"}, []byte("产品,数量\n螺栓,10\n"), templates)
	if err != nil || id != 22 {
		t.Fatalf("recognized id=%d err=%v, want default 22", id, err)
	}
}

func TestRecognizeInquiryTemplateAcceptsUTF8BOM(t *testing.T) {
	templates := []*prv1.InquiryTemplate{{
		Id: 31, Status: "ACTIVE", Fields: []*prv1.InquiryTemplateField{
			{FieldKey: "product", DisplayName: "产品", SortOrder: 1},
			{FieldKey: "quantity", DisplayName: "数量", SortOrder: 2},
		},
	}}
	id, err := recognizeInquiryTemplate(
		&multipart.FileHeader{Filename: "downloaded-template.csv"},
		[]byte("\xef\xbb\xbf产品,数量\n轴承,20\n"),
		templates,
	)
	if err != nil || id != 31 {
		t.Fatalf("BOM template should be recognized: id=%d err=%v", id, err)
	}
}
