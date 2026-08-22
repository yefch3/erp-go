package app

import (
	"strings"
	"testing"

	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestParseSupplierQuoteWorkbook(t *testing.T) {
	expected := []store.FactoryRFQLinesRow{
		{SourcingLineID: 11, Qty: "5.000000", UomCode: "MT", SpecSnapshot: "GI / ASTM A653"},
		{SourcingLineID: 12, Qty: "3.000000", UomCode: "MT", SpecSnapshot: "CRC / SPCC"},
	}
	data, err := xlsx.Build("Supplier Quote", [][]string{
		supplierQuoteHeaders,
		{"RFQ-1", "11", expected[0].SpecSnapshot, expected[0].Qty, "MT", "610.50", "10", "21 days", "first"},
		{"RFQ-1", "12", expected[1].SpecSnapshot, expected[1].Qty, "MT", "620", "", "28 days", "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines, err := parseSupplierQuoteWorkbook(data, "RFQ-1", expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].SourcingLineID != 11 || lines[0].UnitPrice != "610.50" || lines[1].LeadTime != "28 days" {
		t.Fatalf("unexpected import: %#v", lines)
	}
}

func TestParseSupplierQuoteWorkbookRejectsModifiedIdentityAndMissingPrice(t *testing.T) {
	expected := []store.FactoryRFQLinesRow{{SourcingLineID: 11, Qty: "5", UomCode: "MT"}}
	for name, row := range map[string][]string{
		"wrong rfq":     {"RFQ-OTHER", "11", "GI", "5", "MT", "610", "", "", ""},
		"missing price": {"RFQ-1", "11", "GI", "5", "MT", "", "", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := xlsx.Build("Supplier Quote", [][]string{supplierQuoteHeaders, row})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseSupplierQuoteWorkbook(data, "RFQ-1", expected); err == nil {
				t.Fatal("expected invalid supplier workbook")
			}
		})
	}
}

// "报价明细与工厂询价不一致" with no row number is not an error message a
// clerk can act on (A2 §1). Every problem now names the Excel row it lives
// on, all problems come back in one pass, and a row that was complained
// about is not additionally reported as missing.
func TestParseSupplierQuoteWorkbookReportsRowNumbers(t *testing.T) {
	expected := []store.FactoryRFQLinesRow{
		{SourcingLineID: 11, Qty: "5", UomCode: "MT", SpecSnapshot: "GI"},
		{SourcingLineID: 12, Qty: "3", UomCode: "MT", SpecSnapshot: "CRC"},
		{SourcingLineID: 13, Qty: "2", UomCode: "MT", SpecSnapshot: "HRC"},
	}
	data, err := xlsx.Build("Supplier Quote", [][]string{
		supplierQuoteHeaders,
		{"RFQ-1", "11", "GI", "9", "MT", "610", "", "", ""}, // Excel row 2: qty tampered
		{"RFQ-1", "12", "CRC", "3", "MT", "", "", "", ""},   // Excel row 3: price missing
		// line 13 never appears
	})
	if err != nil {
		t.Fatal(err)
	}
	_, perr := parseSupplierQuoteWorkbook(data, "RFQ-1", expected)
	if perr == nil {
		t.Fatal("expected row problems")
	}
	msg := perr.Error()
	for _, want := range []string{"第 2 行", "数量或单位被修改", "第 3 行", "单价未填写", "行号 13", "缺少报价"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error should mention %q, got %s", want, msg)
		}
	}
	// Row 3 had its own complaint; it must not ALSO count as missing.
	if strings.Contains(msg, "行号 12") {
		t.Fatalf("a complained-about row must not double as missing: %s", msg)
	}
}
