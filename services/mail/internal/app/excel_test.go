package app

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestBuildXLSXCreatesWorkbookAndNeverTurnsTextIntoFormula(t *testing.T) {
	book := Workbook{Title: "Any source", Sheets: []WorkbookSheet{{
		Name: "Lines", Columns: []string{"Item", "Quantity", "Customer value"},
		ColumnTypes: []string{"string", "number", "string"},
		Rows: [][]string{
			{"A", "1250", "=1+1"},
			{"B", "2,500", "+SUM(A1:A2)"},
			{"C", "9007199254740993", "large identifier"},
		},
	}}}
	if err := validateWorkbook(&book); err != nil {
		t.Fatal(err)
	}
	data, err := buildXLSX(book)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatal("not an xlsx zip")
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var sheet string
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		_ = r.Close()
		sheet = string(b)
	}
	if sheet == "" {
		t.Fatal("worksheet missing")
	}
	if strings.Contains(sheet, "<f>") {
		t.Fatal("customer text became an Excel formula")
	}
	if !strings.Contains(sheet, "<v>1250</v>") {
		t.Fatal("canonical number was not numeric")
	}
	if !strings.Contains(sheet, "2,500") {
		t.Fatal("non-canonical number should be preserved as text")
	}
	if strings.Contains(sheet, "<v>9007199254740992</v>") {
		t.Fatal("large number was rounded through float64")
	}
	if !strings.Contains(sheet, `t="inlineStr" s="2"><is><t xml:space="preserve">9007199254740993</t>`) {
		t.Fatal("number beyond Excel's safe precision should be preserved as text")
	}
}

func TestInquiryWorkbookHasFixedColumnsBlankUnitPriceAndTrustedTotalFormula(t *testing.T) {
	book := NewInquiryWorkbook(InquiryExtraction{
		Title: "Customer inquiry", Summary: "HRC 1250 MT",
		Items: []InquiryItem{{
			Product: "HRC", MaterialStandard: "ASTM A36 / JIS G 3132 SPHT-1",
			Thickness: "1.10", Width: "1200", CoilWeight: "10.50 MT max",
			CoilID: "762 MM", QuantityUnit: "MT", Quantity: "1250",
		}},
	})
	if err := validateWorkbook(&book); err != nil {
		t.Fatal(err)
	}
	sheet := book.Sheets[0]
	if len(sheet.Columns) != 21 {
		t.Fatalf("columns = %d", len(sheet.Columns))
	}
	if got := sheet.Columns[len(sheet.Columns)-3:]; strings.Join(got, ",") != "数量,单价,总价" {
		t.Fatalf("last columns = %v", got)
	}
	if sheet.Rows[0][19] != "" || sheet.Rows[0][20] != "=S2*T2" {
		t.Fatalf("price cells = %q, %q", sheet.Rows[0][19], sheet.Rows[0][20])
	}

	data, err := buildXLSX(book)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var xml string
	for _, f := range zr.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			_ = r.Close()
			xml = string(b)
		}
	}
	if !strings.Contains(xml, `<c r="U2" s="3"><f>S2*T2</f><v>0</v></c>`) {
		t.Fatalf("trusted total formula missing: %s", xml)
	}
}

func TestSafeExcelDecimalHonorsExcelsPrecisionBoundary(t *testing.T) {
	for _, v := range []string{"0", "-12.50", "999999999999999", "0.12345678901234"} {
		if got, ok := safeExcelDecimal(v); !ok || got != v {
			t.Errorf("safeExcelDecimal(%q) = %q, %v", v, got, ok)
		}
	}
	for _, v := range []string{"9007199254740993", "0.1234567890123456", "00123", "1e3", "+12"} {
		if got, ok := safeExcelDecimal(v); ok {
			t.Errorf("safeExcelDecimal(%q) unexpectedly accepted %q", v, got)
		}
	}
}

func TestSelectedTextSampleSurvivesWhitespaceNormalization(t *testing.T) {
	data, err := os.ReadFile("../../../../_test_case/email content.txt")
	if err != nil {
		t.Fatal(err)
	}
	selected := "ITEM THICKNESS TONS. 1 1,35 X 1500 200"
	if !containsNormalizedText(string(data), selected) {
		t.Fatal("real email selection was not recognized after whitespace normalization")
	}
}

func TestValidateWorkbookMakesSheetNamesSafeAndUnique(t *testing.T) {
	book := Workbook{Sheets: []WorkbookSheet{
		{Name: "A/B", Columns: []string{"x"}, ColumnTypes: []string{"string"}},
		{Name: "A:B", Columns: []string{"x"}, ColumnTypes: []string{"string"}},
	}}
	if err := validateWorkbook(&book); err != nil {
		t.Fatal(err)
	}
	if book.Sheets[0].Name == book.Sheets[1].Name {
		t.Fatal("duplicate sheet names")
	}
	if strings.ContainsAny(book.Sheets[0].Name+book.Sheets[1].Name, "[]:*?/\\") {
		t.Fatal("unsafe character remained in sheet name")
	}
}
