package xlsx

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestBuildParseRoundTripPreservesFixedTemplate(t *testing.T) {
	want := [][]string{
		{"RFQ No", "Line ID", "Specification", "Quantity", "Unit", "Unit Price", "MOQ", "Lead Time", "Remark"},
		{"RFQ-1", "42", "HRC <ASTM> & G60", "12.5000", "TON", "", "5", "20 days", "keep dry"},
	}
	data, err := Build("Supplier Quote", want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d", len(got), len(want))
	}
	for r := range want {
		for c := range want[r] {
			if got[r][c] != want[r][c] {
				t.Fatalf("cell %d,%d = %q, want %q", r, c, got[r][c], want[r][c])
			}
		}
	}
}

func TestParseRejectsNonWorkbook(t *testing.T) {
	if _, err := Parse([]byte("not an xlsx")); err == nil {
		t.Fatal("invalid archive was accepted")
	}
}

func TestBuildWithFormulasKeepsCachedAmount(t *testing.T) {
	rows := [][]string{{"Quantity", "Unit Price", "Amount"}, {"10", "6.25", "62.50"}}
	data, err := BuildWithFormulas("Quotation", rows, map[string]Formula{
		"C2": {Expression: "A2*B2", CachedValue: "62.50"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got[1][2] != "62.50" {
		t.Fatalf("cached formula amount = %q, want 62.50", got[1][2])
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range zr.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		xmlData, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(xmlData), "<f>A2*B2</f><v>62.50</v>") {
			t.Fatalf("worksheet formula missing: %s", xmlData)
		}
		return
	}
	t.Fatal("worksheet part missing")
}

func TestBuildWithFormulasAndNumbersWritesNumericCells(t *testing.T) {
	rows := [][]string{{"Quantity", "Unit Price", "Amount"}, {"50", "695.7600", "34788"}}
	data, err := BuildWithFormulasAndNumbers("Quotation", rows, map[string]Formula{
		"C2": {Expression: "A2*B2", CachedValue: "34788"},
	}, map[string]string{"A2": "50", "B2": "695.7600"})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, file := range zr.File {
		if file.Name != "xl/worksheets/sheet1.xml" && file.Name != "xl/styles.xml" {
			continue
		}
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		part, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		parts[file.Name] = string(part)
	}
	sheet := parts["xl/worksheets/sheet1.xml"]
	for _, want := range []string{`<c r="A2" s="2"><v>50</v></c>`, `<c r="B2" s="2"><v>695.7600</v></c>`, `<c r="C2" s="2"><f>A2*B2</f><v>34788</v></c>`} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("numeric worksheet cell missing %s: %s", want, sheet)
		}
	}
	if strings.Contains(sheet, `r="A2" t="inlineStr"`) || strings.Contains(sheet, `r="B2" t="inlineStr"`) {
		t.Fatalf("numeric cells were written as text: %s", sheet)
	}
	if !strings.Contains(parts["xl/styles.xml"], `formatCode="#,##0.####"`) {
		t.Fatalf("numeric format missing: %s", parts["xl/styles.xml"])
	}
}

func TestBuildWithFormulasAndNumbersRejectsInvalidNumber(t *testing.T) {
	if _, err := BuildWithFormulasAndNumbers("Quotation", [][]string{{"Quantity"}, {"not-a-number"}}, nil, map[string]string{"A2": "not-a-number"}); err == nil {
		t.Fatal("invalid numeric cell was accepted")
	}
}
