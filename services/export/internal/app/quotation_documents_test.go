package app

import (
	"bytes"
	"testing"

	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func quotationDocumentFixture() (store.GetQuotationRow, []store.ListQuotationItemsRow) {
	return store.GetQuotationRow{
			QuoteNo: "QT-TEST-1", CustomerName: "Test Customer", Currency: "USD", Incoterm: "CFR",
			PortOfLoading: "Shanghai", PortOfDischarge: "Valparaiso", PaymentMethod: "T/T",
			TotalAmount: "625.00",
		}, []store.ListQuotationItemsRow{{
			LineNo: 1, ProductCode: "P-1", ProductName: "Steel Coil", Spec: "ASTM A653",
			Qty: "10", UomCode: "TON", UnitPrice: "62.5", Amount: "625.00",
		}}
}

func TestBuildQuotationWorkbookKeepsServerAmounts(t *testing.T) {
	q, items := quotationDocumentFixture()
	data, err := buildQuotationWorkbook(q, items)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if rows[4][7] != "625.00" || rows[5][7] != "625.00" {
		t.Fatalf("line/total = %q/%q, want 625.00/625.00", rows[4][7], rows[5][7])
	}
}

func TestBuildQuotationPDF(t *testing.T) {
	q, items := quotationDocumentFixture()
	data, err := buildQuotationPDF(q, items)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) || len(data) < 800 {
		t.Fatalf("invalid PDF output: prefix=%q bytes=%d", data[:min(5, len(data))], len(data))
	}
}
