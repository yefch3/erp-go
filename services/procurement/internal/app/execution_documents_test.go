package app

import (
	"bytes"
	"testing"

	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func purchaseOrderDocumentFixture() (store.GetPurchaseOrderRow, []store.PurchaseOrderItemsRow) {
	head := store.GetPurchaseOrderRow{
		PoNo: "PO-202608-TEST", SupplierName: "天津测试供应商", Currency: "USD",
		ExpectedDate: "2026-08-23", BuyerName: "系统管理员", TotalAmount: "91120.00",
	}
	items := []store.PurchaseOrderItemsRow{
		{ProductCode: "HG-001", ProductName: "热镀锌钢卷", Spec: "EN 10346 / S350GD+Z / 1.2 / 1250 / Coil", Qty: "40", UomCode: "MT", UnitPrice: "100", Amount: "4000"},
		{ProductCode: "FASTENER-002", ProductName: "工业紧固件组合包", Spec: "ISO 898-1 / 10.9 / M12×60", Qty: "200", UomCode: "SET", UnitPrice: "414", Amount: "82800"},
	}
	return head, items
}

func TestBuildPurchaseOrderDocuments(t *testing.T) {
	head, items := purchaseOrderDocumentFixture()
	documents, err := buildPurchaseOrderDocuments(head, items)
	if err != nil {
		t.Fatalf("build purchase order documents: %v", err)
	}
	if !bytes.HasPrefix(documents.PDFData, []byte("%PDF-")) {
		t.Fatal("purchase order PDF signature is invalid")
	}
	if len(documents.PDFData) < 10000 {
		t.Fatalf("purchase order PDF is unexpectedly small: %d", len(documents.PDFData))
	}
	rows, err := xlsx.Parse(documents.XLSXData)
	if err != nil {
		t.Fatalf("parse purchase order XLSX: %v", err)
	}
	if got := rows[1][2]; got != "天津测试供应商" {
		t.Fatalf("supplier=%q", got)
	}
	if got := rows[4][2]; got != "热镀锌钢卷" {
		t.Fatalf("product=%q", got)
	}
	if got := rows[4][5]; got != "MT" {
		t.Fatalf("unit=%q", got)
	}
	if got := rows[len(rows)-1][7]; got != "91120.00" {
		t.Fatalf("total=%q", got)
	}
}

func TestExecutionPDFTextPreservesChinese(t *testing.T) {
	if got := executionPDFText("热镀锌\n钢卷"); got != "热镀锌 钢卷" {
		t.Fatalf("executionPDFText=%q", got)
	}
}
