package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestSpreadsheetSourceRefsSteelRequestSample(t *testing.T) {
	data, err := os.ReadFile("../../../../_test_case/Steel Request July 2026 (1).xlsx")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := SpreadsheetSourceRefs(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 107 {
		t.Fatalf("detail refs = %d, want 107: %v", len(refs), refs)
	}
	if refs[0] != "PRODUCTS!3" || refs[26] != "PRODUCTS!29" || refs[27] != "COILS!3" || refs[106] != "COILS!82" {
		t.Fatalf("unexpected source boundaries: first=%q products-last=%q coils-first=%q last=%q", refs[0], refs[26], refs[27], refs[106])
	}
	rows, err := SpreadsheetSourceRows(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[27].Cells["STEEL"]; got != "GI" {
		t.Fatalf("COILS formula cache = %q, want GI", got)
	}
	if got := rows[27].QuantityUnitHint; got != "MT" {
		t.Fatalf("COILS quantity unit hint = %q, want MT", got)
	}
}

func TestSpreadsheetSourceClassifiesAndSelectsEverySteelRequestSheet(t *testing.T) {
	data, err := os.ReadFile("../../../../_test_case/Steel Request July 2026 (1).xlsx")
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseSpreadsheetSource(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Sheets) != 7 {
		t.Fatalf("sheet inventory = %d, want 7: %#v", len(document.Sheets), document.Sheets)
	}
	roles := map[string]SpreadsheetSheetInfo{}
	for _, sheet := range document.Sheets {
		roles[sheet.Name] = sheet
	}
	if roles["PRODUCTS"].Role != "detail" || roles["PRODUCTS"].DetailRows != 27 || roles["COILS"].Role != "detail" || roles["COILS"].DetailRows != 80 {
		t.Fatalf("detail classification = %#v", roles)
	}
	for _, name := range []string{"Datos", "ESPECIFICACIONES PELpara los án", "Hoja1", "GENERAL SPECIFICATIONS CINTAC", "GENERAL SPECIFICATIONS TUPEMESA"} {
		if roles[name].Role != "context" || roles[name].ContextBlocks == 0 {
			t.Fatalf("context sheet %q was not classified: %#v", name, roles[name])
		}
	}

	rowByRef := map[string]SpreadsheetSourceRow{}
	for _, row := range document.Rows {
		rowByRef[row.SourceRef] = row
	}
	assertContext := func(ref string, contains, excludes []string) {
		t.Helper()
		blocks := RelevantSpreadsheetContext(document.ContextBlocks, []SpreadsheetSourceRow{rowByRef[ref]})
		encoded, err := json.Marshal(blocks)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		for _, want := range contains {
			if !strings.Contains(text, want) {
				t.Fatalf("context for %s misses %q: %s", ref, want, text)
			}
		}
		for _, unwanted := range excludes {
			if strings.Contains(text, unwanted) {
				t.Fatalf("context for %s incorrectly includes %q", ref, unwanted)
			}
		}
	}
	assertContext("PRODUCTS!3", []string{"Lot TU.7 PT", "ASTM A36 Grade B", "Port Callao", "Calidad del acero A270ES"}, []string{"Lot TU.6 PT"})
	assertContext("PRODUCTS!6", []string{"Lot TU.6 PT", "In length of 6.00 metres", "Container shipping"}, []string{"Lot TU.7 PT"})
	assertContext("PRODUCTS!19", []string{"Lot TU.2 PT", "Structural Tubes According to ASTM A 500", "Lengths:  6M"}, []string{"Lot TU.6 PT"})
	assertContext("COILS!3", []string{"Lot CIN.2", "ASTM A 653", "Port Valparaíso", "20” Coil I.D."}, []string{"Lot TU.7 PT"})
}

func TestSpreadsheetAnchorsOverrideMissingOrAlteredModelFacts(t *testing.T) {
	source := SpreadsheetSourceRow{SourceRef: "COILS!54", QuantityUnitHint: "MT", Cells: map[string]string{
		"COUNTRY": "Perú Tupemesa", "LOT": "TU.1", "STEEL": "CRC",
		"Thickness [mm]": "1.4", "Width [mm]": "1200", "INQ Q'ty": "301.39290671479", "Coil weights": "12-16 TM",
	}}
	item := map[string]string{"product": "", "width": "1190", "quantity": "301", "remarks": "LOT: TU.1; model note; model note"}
	applySpreadsheetAnchors(source, item)
	for key, want := range map[string]string{
		"product": "CRC", "thickness": "1.4", "width": "1200", "quantity": "301.39290671479",
		"quantity_unit": "MT", "coil_weight": "12-16 TM",
	} {
		if got := item[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if !strings.Contains(item["remarks"], "COUNTRY: Perú Tupemesa") || !strings.Contains(item["remarks"], "LOT: TU.1") {
		t.Fatalf("remarks lost source identity: %q", item["remarks"])
	}
	if strings.Count(item["remarks"], "LOT: TU.1") != 1 || strings.Count(item["remarks"], "model note") != 1 {
		t.Fatalf("remarks were not de-duplicated: %q", item["remarks"])
	}
}

func TestSpreadsheetAnchorsParseOnlyUnambiguousPlateDescription(t *testing.T) {
	item := map[string]string{}
	applySpreadsheetAnchors(SpreadsheetSourceRow{Cells: map[string]string{
		"DESCRIPTION": "PL ESTRIADA ASTM A36 2.00 X 1200 X 2400",
		"INQ Q'ty":    "519.352549858347",
	}}, item)
	for key, want := range map[string]string{
		"product": "PL ESTRIADA ASTM A36 2.00 X 1200 X 2400", "material_standard": "ASTM A36",
		"thickness": "2", "width": "1200", "length_or_form": "2400", "quantity": "519.352549858347",
	} {
		if got := item[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	profile := map[string]string{"thickness": "model-derived"}
	applySpreadsheetAnchors(SpreadsheetSourceRow{Cells: map[string]string{
		"DESCRIPTION": "ANG 25 x 25 x 2.5 X6M",
	}}, profile)
	for key, want := range map[string]string{"thickness": "2.5", "width": "25", "custom.height_or_leg2": "25", "length_or_form": "6000"} {
		if got := profile[key]; got != want {
			t.Fatalf("angle %s = %q, want %q", key, got, want)
		}
	}
	for key, want := range map[string]string{
		"custom.thickness_mm": "2.5", "custom.wall_thickness_mm": "",
		"custom.width_mm": "", "custom.height_mm": "", "custom.diameter_mm": "",
		"custom.leg1_mm": "25", "custom.leg2_mm": "25",
	} {
		if got := profile[key]; got != want {
			t.Fatalf("angle exact %s = %q, want %q", key, got, want)
		}
	}
}

func TestKnownProductDimensionsKeepDimensionConceptsSeparate(t *testing.T) {
	tests := []struct {
		description string
		want        map[string]string
	}{
		{"BARRA REDONDA 1/2 X 6M", map[string]string{"custom.diameter_mm": "12.7"}},
		{"REC 200 X 100 X 4.5 X 6M", map[string]string{
			"custom.wall_thickness_mm": "4.5", "custom.width_mm": "200", "custom.height_mm": "100",
		}},
		{"PLATINA 50 X 5 X 6M", map[string]string{"custom.thickness_mm": "5", "custom.width_mm": "50"}},
	}
	keys := []string{
		"custom.thickness_mm", "custom.wall_thickness_mm", "custom.width_mm", "custom.height_mm",
		"custom.diameter_mm", "custom.leg1_mm", "custom.leg2_mm",
	}
	for _, test := range tests {
		item := map[string]string{}
		if !applyKnownProductDimensions(test.description, item) {
			t.Fatalf("description not recognized: %s", test.description)
		}
		for _, key := range keys {
			if got := item[key]; got != test.want[key] {
				t.Fatalf("%s %s = %q, want %q", test.description, key, got, test.want[key])
			}
		}
	}
}

func TestSpreadsheetAnchorsParseEverySteelRequestProductDimension(t *testing.T) {
	data, err := os.ReadFile("../../../../_test_case/Steel Request July 2026 (1).xlsx")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := SpreadsheetSourceRows(data)
	if err != nil {
		t.Fatal(err)
	}
	expected := [][]string{
		{"2", "1200", "", "2400"}, {"2.5", "1200", "", "2400"}, {"2.9", "1200", "", "2400"},
		{"2.5", "25", "25", "6000"}, {"", "12.7", "", "6000"}, {"2.38125", "38.1", "38.1", "6000"},
		{"", "15.875", "", "6000"}, {"2.5", "19.05", "19.05", "6000"}, {"", "9.525", "", "6000"},
		{"3.175", "12.7", "", "6000"}, {"3.175", "50.8", "", "6000"}, {"3.175", "25.4", "", "6000"},
		{"3", "25", "25", "6000"}, {"3.175", "38.1", "38.1", "6000"}, {"", "9", "9", "6000"},
		{"", "12", "12", "6000"}, {"3", "150", "150", "6000"}, {"4.5", "200", "100", "6000"},
		{"4.5", "150", "100", "6000"}, {"4.5", "150", "150", "6000"}, {"3", "150", "100", "6000"},
		{"3", "200", "100", "6000"}, {"3", "150", "100", "6000"}, {"3", "150", "150", "6000"},
		{"4", "150", "150", "6000"}, {"3", "200", "100", "6000"}, {"4", "200", "200", "12000"},
	}
	if len(rows) < len(expected) {
		t.Fatalf("source rows = %d, want at least %d", len(rows), len(expected))
	}
	keys := []string{"thickness", "width", "custom.height_or_leg2", "length_or_form"}
	for i, want := range expected {
		item := map[string]string{}
		applySpreadsheetAnchors(rows[i], item)
		for j, key := range keys {
			if got := item[key]; got != want[j] {
				t.Fatalf("%s %s = %q, want %q (product %q)", rows[i].SourceRef, key, got, want[j], item["product"])
			}
		}
	}
}

func TestGeneratedDimensionEdgeCasesWorkbook(t *testing.T) {
	data, err := os.ReadFile("../../../../_test_case/Steel Dimension Edge Cases.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseSpreadsheetSource(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Rows) != 31 {
		t.Fatalf("detail rows = %d, want 31", len(document.Rows))
	}
	roles := map[string]SpreadsheetSheetInfo{}
	for _, sheet := range document.Sheets {
		roles[sheet.Name] = sheet
	}
	for _, name := range []string{"METRIC_PRODUCTS", "IMPERIAL_PRODUCTS", "EDGE_CASES", "COIL_EDGE"} {
		if roles[name].Role != "detail" {
			t.Fatalf("sheet %s role = %#v", name, roles[name])
		}
	}
	if roles["SPECIFICATIONS"].Role != "context" || roles["SPECIFICATIONS"].ContextBlocks == 0 {
		t.Fatalf("specifications role = %#v", roles["SPECIFICATIONS"])
	}

	type expectedDimensions struct {
		thickness, width, height, length string
		known                            bool
	}
	expected := map[string]expectedDimensions{
		"METRIC_PRODUCTS!3":    {"1.5", "1000", "", "2000", true},
		"METRIC_PRODUCTS!4":    {"4", "50", "30", "12000", true},
		"METRIC_PRODUCTS!5":    {"", "16", "", "6000", true},
		"METRIC_PRODUCTS!6":    {"6", "50", "", "6000", true},
		"METRIC_PRODUCTS!7":    {"", "20", "20", "12000", true},
		"METRIC_PRODUCTS!8":    {"5", "100", "100", "12000", true},
		"METRIC_PRODUCTS!9":    {"6", "120", "80", "12000", true},
		"METRIC_PRODUCTS!10":   {"3.5", "80", "80", "12000", true},
		"METRIC_PRODUCTS!11":   {"2.5", "100", "50", "6000", true},
		"IMPERIAL_PRODUCTS!3":  {"", "19.05", "", "6000", true},
		"IMPERIAL_PRODUCTS!4":  {"", "31.75", "", "12000", true},
		"IMPERIAL_PRODUCTS!5":  {"6.35", "50.8", "", "6000", true},
		"IMPERIAL_PRODUCTS!6":  {"4.7625", "38.1", "", "12000", true},
		"IMPERIAL_PRODUCTS!7":  {"6.35", "50.8", "50.8", "6000", true},
		"IMPERIAL_PRODUCTS!8":  {"4.7625", "63.5", "50.8", "12000", true},
		"IMPERIAL_PRODUCTS!9":  {"3", "19.05", "19.05", "6000", true},
		"IMPERIAL_PRODUCTS!10": {"", "25.4", "25.4", "6000", true},
		"IMPERIAL_PRODUCTS!11": {"", "38.1", "38.1", "6000", true},
		"EDGE_CASES!3":         {"3", "40", "40", "6000", true},
		"EDGE_CASES!4":         {"4.75", "200", "75", "6000", true},
		"EDGE_CASES!5":         {"2.5", "60", "60", "6000", true},
		"EDGE_CASES!6":         {"5", "50", "50", "6000", true},
		"EDGE_CASES!7":         {"", "20", "", "6000", true},
		"EDGE_CASES!8":         {"5", "50", "", "6000", true},
		"EDGE_CASES!9":         {"2", "1250", "", "2500", true},
		"EDGE_CASES!10":        {"2.75", "1250", "", "2500", true},
		"EDGE_CASES!11":        {known: false},
		"EDGE_CASES!12":        {known: false},
	}
	type expectedCoil struct{ product, thickness, width, quantity string }
	coils := map[string]expectedCoil{
		"COIL_EDGE!3": {"GI", "0.35", "1000", "40"},
		"COIL_EDGE!4": {"HRC", "1.25", "1250", "41"},
		"COIL_EDGE!5": {"CRC", "0.8", "1219", "42"},
	}
	for _, row := range document.Rows {
		if want, ok := coils[row.SourceRef]; ok {
			item := map[string]string{}
			applySpreadsheetAnchors(row, item)
			for key, value := range map[string]string{
				"product": want.product, "thickness": want.thickness, "width": want.width,
				"custom.height_or_leg2": "", "length_or_form": "", "quantity": want.quantity, "quantity_unit": "MT",
			} {
				if item[key] != value {
					t.Fatalf("%s %s = %q, want %q", row.SourceRef, key, item[key], value)
				}
			}
			continue
		}
		want, ok := expected[row.SourceRef]
		if !ok {
			t.Fatalf("unexpected source row %s", row.SourceRef)
		}
		description := row.Cells["DESCRIPTION"]
		item := map[string]string{"thickness": "sentinel", "width": "sentinel", "custom.height_or_leg2": "sentinel", "length_or_form": "sentinel"}
		known := applyKnownProductDimensions(description, item)
		if known != want.known {
			t.Fatalf("%s known = %v, want %v (%q)", row.SourceRef, known, want.known, description)
		}
		if !known {
			for _, key := range []string{"thickness", "width", "custom.height_or_leg2", "length_or_form"} {
				if item[key] != "sentinel" {
					t.Fatalf("%s unknown product changed %s to %q", row.SourceRef, key, item[key])
				}
			}
			continue
		}
		for key, value := range map[string]string{
			"thickness": want.thickness, "width": want.width,
			"custom.height_or_leg2": want.height, "length_or_form": want.length,
		} {
			if item[key] != value {
				t.Fatalf("%s %s = %q, want %q (%q)", row.SourceRef, key, item[key], value, description)
			}
		}
	}
}

func TestSpreadsheetLengthIsCanonicalMillimetres(t *testing.T) {
	columns := SystemInquiryColumns()
	var lengthColumn InquiryColumn
	for _, column := range columns {
		if column.FieldKey == "length_or_form" {
			lengthColumn = column
		}
	}
	if lengthColumn.DisplayName != "长度(mm)" || lengthColumn.DataType != "NUMBER" {
		t.Fatalf("system length column = %#v", lengthColumn)
	}
	for _, test := range []struct {
		input, want string
	}{
		{"6M", "6000"}, {"6.00 metres", "6000"}, {"2400", "2400"}, {"1200 mm", "1200"},
	} {
		item := map[string]string{"length_or_form": test.input}
		normalizeSpreadsheetLength(item)
		if got := item["length_or_form"]; got != test.want {
			t.Fatalf("length %q = %q, want %q", test.input, got, test.want)
		}
	}
	item := map[string]string{"length_or_form": "6M LAC"}
	normalizeSpreadsheetLength(item)
	if item["length_or_form"] != "" || !strings.Contains(item["remarks"], "ORIGINAL LENGTH/FORM: 6M LAC") {
		t.Fatalf("ambiguous length/form was not isolated: %#v", item)
	}
}

type recordingSpreadsheetExtractor struct {
	batches  [][]string
	contexts [][]SpreadsheetContextBlock
}

func (e *recordingSpreadsheetExtractor) Extract(_ context.Context, in TableExtractionInput) (Extraction, error) {
	e.batches = append(e.batches, append([]string(nil), in.SourceRefs...))
	var source struct {
		Rows          []SpreadsheetSourceRow    `json:"rows"`
		ContextBlocks []SpreadsheetContextBlock `json:"context_blocks"`
	}
	if err := json.Unmarshal([]byte(in.Text), &source); err != nil {
		return Extraction{}, err
	}
	e.contexts = append(e.contexts, source.ContextBlocks)
	items := make([]map[string]string, len(source.Rows))
	for i, row := range source.Rows {
		items[i] = map[string]string{"source_ref": row.SourceRef, "product": row.Cells["product"]}
	}
	return Extraction{Inquiry: ExtractedInquiry{Title: "t", Summary: "s", Items: items}, Model: "test", Usage: ModelUsage{InputTokens: 1, OutputTokens: 2}}, nil
}

func TestSpreadsheetExtractionBatchesAndMergesOnlyCompleteRows(t *testing.T) {
	rows := make([]SpreadsheetSourceRow, 41)
	for i := range rows {
		rows[i] = SpreadsheetSourceRow{SourceRef: "S!" + strconv.Itoa(i+2), Cells: map[string]string{"product": "P"}}
	}
	extractor := &recordingSpreadsheetExtractor{}
	svc := &Service{tables: extractor}
	contexts := []SpreadsheetContextBlock{
		{SourceRef: "global!1", Scope: "sheet_global", global: true},
		{SourceRef: "lot!1", Scope: "lot", matchKeys: []string{"A.1"}},
		{SourceRef: "other!1", Scope: "lot", matchKeys: []string{"B.1"}},
	}
	rows[0].Cells["LOT"] = "A.1"
	got, err := svc.extractSpreadsheetRows(t.Context(), TableExtractionInput{SourceRows: rows, SourceContext: contexts}, []InquiryColumn{{FieldKey: "product", DisplayName: "产品", DataType: "TEXT"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(extractor.batches) != 3 {
		t.Fatalf("batch count = %d, want 3", len(extractor.batches))
	}
	if counts := []int{len(extractor.batches[0]), len(extractor.batches[1]), len(extractor.batches[2])}; counts[0] != 20 || counts[1] != 20 || counts[2] != 1 {
		t.Fatalf("batch sizes = %#v", counts)
	}
	if len(got.Workbook.Sheets[0].Rows) != 41 || got.Usage.InputTokens != 3 || got.Usage.OutputTokens != 6 {
		t.Fatalf("merged extraction = %#v", got)
	}
	if len(extractor.contexts[0]) != 2 || extractor.contexts[0][0].SourceRef != "global!1" || extractor.contexts[0][1].SourceRef != "lot!1" {
		t.Fatalf("first batch context = %#v", extractor.contexts[0])
	}
	if len(extractor.contexts[1]) != 1 || extractor.contexts[1][0].SourceRef != "global!1" {
		t.Fatalf("unrelated lot context leaked: %#v", extractor.contexts[1])
	}
}

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
	book := NewTemplateWorkbook(ExtractedInquiry{
		Title: "Customer inquiry", Summary: "HRC 1250 MT",
		Items: []map[string]string{{
			"product": "HRC", "material_standard": "ASTM A36 / JIS G 3132 SPHT-1",
			"thickness": "1.10", "width": "1200", "coil_weight": "10.50 MT max",
			"coil_id": "762 MM", "quantity_unit": "MT", "quantity": "1250",
		}, {
			// 第二行带单价：询盘阶段不会有，但模板被改成也收单价时公式
			// 照旧要出，而且位置要对。
			"product": "CRC", "quantity_unit": "MT", "quantity": "20", "unit_price": "612.5",
		}},
	}, nil)
	if err := validateWorkbook(&book); err != nil {
		t.Fatal(err)
	}
	sheet := book.Sheets[0]
	if len(sheet.Columns) != 22 {
		t.Fatalf("columns = %d", len(sheet.Columns))
	}
	if got := sheet.Columns[len(sheet.Columns)-3:]; strings.Join(got, ",") != "数量,单价,总价" {
		t.Fatalf("last columns = %v", got)
	}
	// 询盘阶段模型不给单价——单价空，总价就该空，而不是一个指着空格子的
	// 算式（那在 Excel 里算成 0，在页面预览里露出「=T2*U2」）。
	if sheet.Rows[0][20] != "" || sheet.Rows[0][21] != "" {
		t.Fatalf("没有单价时单价和总价都该为空，实际 %q, %q", sheet.Rows[0][20], sheet.Rows[0][21])
	}
	if sheet.PreviewRows[0][21] != "" {
		t.Fatalf("没有单价时预览的总价也该为空，实际 %q", sheet.PreviewRows[0][21])
	}
	// 有单价的那一行：文件里落公式，预览里落算出来的数。
	if sheet.Rows[1][21] != "=T3*U3" {
		t.Fatalf("有单价时该落公式 =T3*U3，实际 %q", sheet.Rows[1][21])
	}
	if sheet.PreviewRows[1][21] != "12250" {
		t.Fatalf("预览该显示 20×612.5=12250，实际 %q", sheet.PreviewRows[1][21])
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
	// 公式由服务端按模板列位算出来，模型碰不到；缓存值是真算出来的数，
	// 不重算公式的看表工具也能显示对。
	if !strings.Contains(xml, `<c r="V3" s="3"><f>T3*U3</f><v>12250</v></c>`) {
		t.Fatalf("trusted total formula missing: %s", xml)
	}
	// 没有单价的那一行连公式都不该有。
	if strings.Contains(xml, `r="V2" s="3"`) {
		t.Fatalf("没有单价的行不该落公式: %s", xml)
	}
}

func TestTemplateWorkbookFollowsTemplateColumns(t *testing.T) {
	columns := []InquiryColumn{
		{FieldKey: "product", DisplayName: "品名", DataType: "TEXT", IsRequired: true},
		{FieldKey: "quantity", DisplayName: "需求数量", DataType: "NUMBER", IsRequired: true},
		{FieldKey: "quantity_unit", DisplayName: "计量单位", DataType: "TEXT", IsRequired: true, DefaultValue: "MT"},
		{FieldKey: "custom.customer_part_no", DisplayName: "客户料号", DataType: "TEXT"},
		{FieldKey: "unit_price", DisplayName: "单价", DataType: "NUMBER"},
		{FieldKey: "total_price", DisplayName: "总价", DataType: "NUMBER"},
	}
	book := NewTemplateWorkbook(ExtractedInquiry{
		Title: "t",
		Items: []map[string]string{{
			"product": "镀锌卷", "quantity": "25", "custom.customer_part_no": "CP-99887",
			"unit_price": "480",
		}},
	}, columns)
	if err := validateWorkbook(&book); err != nil {
		t.Fatal(err)
	}
	sheet := book.Sheets[0]
	if strings.Join(sheet.Columns, ",") != "品名,需求数量,计量单位,客户料号,单价,总价" {
		t.Fatalf("columns should follow the template, got %v", sheet.Columns)
	}
	if sheet.ColumnKeys[3] != "custom.customer_part_no" {
		t.Fatalf("column keys should follow the template, got %v", sheet.ColumnKeys)
	}
	row := sheet.Rows[0]
	if row[2] != "MT" {
		t.Fatalf("template default value should fill the unit, got %q", row[2])
	}
	if row[5] != "=B2*E2" || sheet.ColumnTypes[5] != "formula" {
		t.Fatalf("total formula should follow template column positions, got %q type %s", row[5], sheet.ColumnTypes[5])
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
