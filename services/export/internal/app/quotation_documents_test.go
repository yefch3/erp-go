package app

import (
	"bytes"
	"os"
	"testing"

	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func quotationDocumentFixture() (store.GetQuotationRow, []store.ListQuotationItemsRow) {
	return store.GetQuotationRow{
		QuoteNo: "QT-TEST-1", CustomerName: "测试客户", Currency: "USD", Incoterm: "CFR",
		PortOfLoading: "宁波", PortOfDischarge: "洛杉矶", PaymentMethod: "即期信用证",
		TotalAmount: "625.00",
	}, []store.ListQuotationItemsRow{{
		LineNo: 1, ProductCode: "P-1", ProductName: "热镀锌钢卷", Spec: "EN 10346 / S350GD+Z / 1.2 × 1450mm",
		Qty: "10", UomCode: "吨", UnitPrice: "62.5", Amount: "625.00",
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
	if output := os.Getenv("QUOTATION_PDF_TEST_OUTPUT"); output != "" {
		if err := os.WriteFile(output, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPDFTextPreservesChinese(t *testing.T) {
	if got := pdfText("测试客户\t热镀锌钢卷"); got != "测试客户 热镀锌钢卷" {
		t.Fatalf("pdfText() = %q", got)
	}
}
