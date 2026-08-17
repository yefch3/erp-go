package app

import (
	"strings"
	"testing"

	"github.com/sgao19/erp-go/pkg/xlsx"
)

func validPurchaseTemplateRow() []string {
	return []string{purchaseTemplateVersion, "301", "11", "0", "7", "CT-1", "P-1", "Coil", "G60", "10", "8", "TON", "12", "SUP-12", "USD", "520", "2026-09-01", "5", "30 days", ""}
}

func TestParsePurchaseTemplate(t *testing.T) {
	data, err := xlsx.Build("Purchase Import", [][]string{purchaseTemplateHeaders, validPurchaseTemplateRow()})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parsePurchaseTemplate(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].requirementID != 301 || rows[0].supplierID != 12 || rows[0].qty != "8" || rows[0].unitPrice != "520" {
		t.Fatalf("unexpected parsed row: %#v", rows)
	}
}

func TestParsePurchaseTemplateRejectsHeaderAndVersion(t *testing.T) {
	t.Run("header", func(t *testing.T) {
		headers := append([]string(nil), purchaseTemplateHeaders...)
		headers[1] = "Changed ID"
		data, _ := xlsx.Build("Purchase Import", [][]string{headers, validPurchaseTemplateRow()})
		if _, err := parsePurchaseTemplate(data); err == nil {
			t.Fatal("expected changed header to be rejected")
		}
	})
	t.Run("version becomes row error", func(t *testing.T) {
		row := validPurchaseTemplateRow()
		row[0] = "PO_TEMPLATE_V0"
		data, _ := xlsx.Build("Purchase Import", [][]string{purchaseTemplateHeaders, row})
		rows, err := parsePurchaseTemplate(data)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rows[0].err, "版本") {
			t.Fatalf("expected version error, got %q", rows[0].err)
		}
	})
}

func TestBuildPurchaseTemplateErrorsAddsErrorColumn(t *testing.T) {
	row := validPurchaseTemplateRow()
	data, err := buildPurchaseTemplateErrors([]parsedPurchaseTemplateRow{{cells: row, err: "供应商编码不匹配"}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[1][19]; got != "供应商编码不匹配" {
		t.Fatalf("error column = %q", got)
	}
}
