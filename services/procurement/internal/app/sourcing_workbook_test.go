package app

import (
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
