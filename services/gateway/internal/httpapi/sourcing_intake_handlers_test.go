package httpapi

import (
	"mime/multipart"
	"strings"
	"testing"

)

func TestParseStandardizedInquiryCSV(t *testing.T) {
	header := &multipart.FileHeader{Filename: "standard-inquiry.csv"}
	data := []byte("产品,材质/标准,牌号/等级,厚度,宽度,长度/形式,表面要求,涂层/镀层,公差,卷重,卷内径,包装,交期,付款条件,贸易术语,港口,数量,单位,备注\n冷轧卷,ASTM A1008,CS-B,1.2,1250,,,,,,,,2026-09-01,,,上海,20,MT,\n")
	lines, err := parseStandardizedInquiry(header, data)
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

func TestParseStandardizedInquiryRejectsHeaderMismatch(t *testing.T) {
	data := []byte("产品,材质/标准,牌号/等级,厚度,宽度,长度/形式,表面要求,涂层/镀层,公差,卷重,卷内径,包装,交期,付款条件,贸易术语,港口,需求数量,单位,备注,额外规格\n镀锌钢卷,,,,,,,,,,,,,,,,25,MT,,测试\n")
	_, err := parseStandardizedInquiry(&multipart.FileHeader{Filename: "mismatch.csv"}, data)
	if err == nil || !strings.Contains(err.Error(), "表头与系统标准不一致") ||
		!strings.Contains(err.Error(), "缺少列：数量") || !strings.Contains(err.Error(), "非标准列") {
		t.Fatalf("expected detailed mismatch error, got %v", err)
	}
}

func TestParseStandardizedInquiryRejectsDuplicateHeader(t *testing.T) {
	_, err := validateInquiryHeaders([]string{"产品", "数量", "数量"}, standardizedInquiryFields())
	if err == nil || !strings.Contains(err.Error(), "重复列：数量") {
		t.Fatalf("expected duplicate header error, got %v", err)
	}
}

func TestParseStandardizedInquiryRejectsUnknownFile(t *testing.T) {
	_, err := parseStandardizedInquiry(&multipart.FileHeader{Filename: "raw-email.txt"}, []byte("hello"))
	if err == nil {
		t.Fatal("expected unsupported file error")
	}
}
