package app

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/sgao19/erp-go/pkg/xlsx"
)

func TestSpreadsheetSourceRecognizesMixedHeadersOnOneSheet(t *testing.T) {
	data, err := xlsx.Build("Mixed", [][]string{
		{"ITEM", "Width (MM)", "Length (MM)", "Invoicing Thickness (MM)(Real)", "Tons(MT) Requirement"},
		{"GALVANIZED", "800", "3600", "0.22", "250"},
		{"Total Tons", "", "", "", "250"},
		{"Product description", "Qty", "UOM", "Width (mm)", "Length (mm)"},
		{"Steel plate", "20", "PCS", "900", "3000"},
		{"Subtotal", "20"},
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseSpreadsheetSource(data)
	if err != nil || len(document.Rows) != 2 || document.ModelText != "" {
		t.Fatalf("mixed workbook: %+v, error: %v", document, err)
	}
	for i, want := range []struct{ ref, quantity, unit, width string }{
		{"Mixed!2", "250", "MT", "800"}, {"Mixed!5", "20", "PCS", "900"},
	} {
		row := document.Rows[i]
		item := map[string]string{"quantity": "wrong", "quantity_unit": "wrong"}
		applySpreadsheetAnchors(row, item)
		if row.SourceRef != want.ref || item["quantity"] != want.quantity || item["quantity_unit"] != want.unit || item["width"] != want.width {
			t.Errorf("row %s mapped incorrectly: %v", row.SourceRef, item)
		}
	}
}

func TestSpreadsheetSourceReportsConflictingQuantityColumns(t *testing.T) {
	rows := [][]string{{"Product", "Qty", "Quantity", "Unit"}, {"Coil", "20", "50", "MT"}}
	book, err := xlsx.Build("Orders", rows)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseSpreadsheetSource(book)
	if _, ok := err.(*spreadsheetQuantityConflict); !ok || !strings.Contains(err.Error(), "B、C") {
		t.Fatalf("expected conflicting columns B and C, got %v", err)
	}
	rows[0][2] = "INQ Q'ty"
	book, err = xlsx.Build("Orders", rows)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := SpreadsheetSourceRows(book)
	if err != nil || len(parsed) != 1 {
		t.Fatalf("explicit quantity rows: %+v, error: %v", parsed, err)
	}
	item := map[string]string{}
	applySpreadsheetAnchors(parsed[0], item)
	if item["quantity"] != "50" {
		t.Fatalf("explicit quantity must take precedence: %v", item)
	}
}

func TestSpreadsheetSourceProvidesRawCellsForUnknownHeaders(t *testing.T) {
	data, err := xlsx.Build("Unknown", [][]string{{"Article code", "Order volume"}, {}, {"P-1", "42"}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseSpreadsheetSource(data)
	if err != nil || len(document.Rows) != 0 || !strings.Contains(document.ModelText, "Unknown!3") || !strings.Contains(document.ModelText, "Order volume") {
		t.Fatalf("unknown workbook input = %+v, error: %v", document, err)
	}
}

func TestSpreadsheetSourceRecognizesRequestedTonnage(t *testing.T) {
	const sheet = `<worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c><c r="G1" t="s"><v>6</v></c><c r="H1" t="s"><v>7</v></c></row>
<row r="2"><c r="A2" t="s"><v>8</v></c><c r="B2"><v>800</v></c><c r="C2"><v>3600</v></c><c r="D2"><v>0.14</v></c><c r="E2"><v>340</v></c><c r="F2"><v>0.18</v></c><c r="G2" t="s"><v>9</v></c><c r="H2"><v>250</v></c></row>
<row r="3"><c r="A3" s="1" t="s"/><c r="B3" t="s"><v></v></c><c r="G3" t="s"><v>10</v></c><c r="H3"><v>250</v></c></row>
</sheetData><mergeCells count="1"><mergeCell ref="A3:F3"/></mergeCells></worksheet>`
	makeBook := func(sheet string) []byte {
		t.Helper()
		var data bytes.Buffer
		w := zip.NewWriter(&data)
		for name, contents := range map[string]string{
			"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" r:id="rId1"/></sheets></workbook>`,
			"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
			"xl/sharedStrings.xml":       `<sst><si><t>ITEM</t></si><si><t>Width&#10;(MM)</t></si><si><t>Length&#10;(MM)</t></si><si><t>Invocing&#10;Thickness&#10;(MM)(Real)</t></si><si><t>Sheets per MT</t></si><si><t>Label Thickness</t></si><si><t>Spec</t></si><si><t>Tons(MT)&#10;Requerim&#10;ent</t></si><si><t>GALVANIZED</t></si><si><t>Z40</t></si><si><t>Total Tons</t></si></sst>`,
			"xl/worksheets/sheet1.xml":   sheet,
		} {
			entry, err := w.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte(contents)); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}

	rows, err := SpreadsheetSourceRows(makeBook(sheet))
	if err != nil || len(rows) != 1 || rows[0].SourceRef != "Sheet1!2" {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
	item := map[string]string{"quantity": "340", "thickness": "0.18"}
	applySpreadsheetAnchors(rows[0], item)
	for key, want := range map[string]string{
		"product": "GALVANIZED", "quantity": "250", "quantity_unit": "MT",
		"thickness": "0.14", "width": "800", "length_or_form": "3600",
	} {
		if item[key] != want {
			t.Errorf("%s = %q, want %q", key, item[key], want)
		}
	}
	if rows[0].Cells["Spec"] != "Z40" {
		t.Errorf("source specification lost: %+v", rows[0])
	}
	extractor := &recordingSpreadsheetExtractor{}
	svc := &Service{tables: extractor}
	extracted, err := svc.extractSpreadsheetRows(t.Context(), TableExtractionInput{SourceRows: rows}, SystemInquiryColumns())
	if err != nil {
		t.Fatal(err)
	}
	if len(extracted.Workbook.Sheets) != 1 || len(extracted.Workbook.Sheets[0].Rows) != 1 ||
		len(extractor.batches) != 1 || len(extractor.batches[0]) != 1 || extractor.batches[0][0] != "Sheet1!2" {
		t.Fatalf("unexpected extraction: %+v, batches = %v", extracted, extractor.batches)
	}
	_, err = SpreadsheetSourceRows(makeBook(strings.Replace(sheet, "<v>8</v>", "<v>999</v>", 1)))
	if err == nil || !strings.Contains(err.Error(), "invalid shared string index") {
		t.Fatalf("invalid nonempty shared string index = %v", err)
	}
}
