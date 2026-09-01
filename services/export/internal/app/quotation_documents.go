package app

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"codeberg.org/go-pdf/fpdf"

	"github.com/sgao19/erp-go/pkg/pdffont"
	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func (s *Service) GetQuotationWorkbook(ctx context.Context, tenantID, id int64, op Operator) (string, []byte, error) {
	q, items, err := s.GetQuotationFor(ctx, tenantID, id, op)
	if err != nil {
		return "", nil, err
	}
	shipments, err := s.ListQuotationShipments(ctx, tenantID, id)
	if err != nil {
		return "", nil, err
	}
	data, err := buildQuotationWorkbookWithShipments(q, items, shipments)
	return q.QuoteNo + ".xlsx", data, err
}

func buildQuotationWorkbook(q store.GetQuotationRow, items []store.ListQuotationItemsRow) ([]byte, error) {
	return buildQuotationWorkbookWithShipments(q, items, nil)
}

func buildQuotationWorkbookWithShipments(q store.GetQuotationRow, items []store.ListQuotationItemsRow, shipments []store.ListQuotationShipmentsRow) ([]byte, error) {
	rows := [][]string{
		{"Quotation No", "Customer", "Currency", "Incoterm", "Port of Loading", "Port of Discharge", "Payment Terms", "Valid Until"},
		{q.QuoteNo, q.CustomerName, q.Currency, q.Incoterm, q.PortOfLoading, q.PortOfDischarge, q.PaymentMethod, q.ValidUntil},
		{},
		{"Line", "Product Code", "Product", "Specification", "Quantity", "Unit", "Unit Price", "Amount", "Remark"},
	}
	formulas := make(map[string]xlsx.Formula, len(items)+1)
	for index, item := range items {
		rowNumber := index + 5
		rows = append(rows, []string{
			strconv.Itoa(int(item.LineNo)), item.ProductCode, item.ProductName, item.Spec,
			item.Qty, item.UomCode, item.UnitPrice, item.Amount, item.Remark,
		})
		formulas[fmt.Sprintf("H%d", rowNumber)] = xlsx.Formula{
			Expression: fmt.Sprintf("E%d*G%d", rowNumber, rowNumber), CachedValue: item.Amount,
		}
	}
	if len(shipments) > 0 {
		rows = append(rows, []string{}, []string{"Freight batches"}, []string{"Batch", "Carrier / Forwarder", "Service", "Freight", "Charge basis", "Port of loading", "Port of discharge", "ETD", "ETA", "Valid until", "Remark"})
		for _, shipment := range shipments {
			carrier := shipment.CarrierForwarder
			if shipment.CustomerManaged {
				carrier = "Customer managed"
			}
			rows = append(rows, []string{strconv.Itoa(int(shipment.BatchNo)), carrier, shipment.ServiceOptionName, shipment.FreightAmount,
				shipment.ChargeBasis, shipment.PortOfLoading, shipment.PortOfDischarge, shipment.EstimatedDeparture,
				shipment.EstimatedArrival, shipment.ValidUntil, shipment.Remark})
		}
	}
	rows = append(rows, []string{"", "", "", "", "", "", "Total", q.TotalAmount, ""})
	return xlsx.BuildWithFormulas("Customer Quotation", rows, formulas)
}

func (s *Service) GetQuotationPDF(ctx context.Context, tenantID, id int64, op Operator) (string, []byte, error) {
	q, items, err := s.GetQuotationFor(ctx, tenantID, id, op)
	if err != nil {
		return "", nil, err
	}
	shipments, err := s.ListQuotationShipments(ctx, tenantID, id)
	if err != nil {
		return "", nil, err
	}
	data, err := buildQuotationPDFWithShipments(q, items, shipments)
	return q.QuoteNo + ".pdf", data, err
}

func buildQuotationPDF(q store.GetQuotationRow, items []store.ListQuotationItemsRow) ([]byte, error) {
	return buildQuotationPDFWithShipments(q, items, nil)
}

func buildQuotationPDFWithShipments(q store.GetQuotationRow, items []store.ListQuotationItemsRow, shipments []store.ListQuotationShipmentsRow) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 12)
	// 字体随程序嵌入（见 pkg/pdffont）：Docker 容器和开发电脑才能生成完全一致的中文报价单。
	pdffont.Register(pdf)
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("load quotation PDF font: %w", err)
	}
	pdf.AddPage()
	pdf.SetFont(pdffont.Name, "B", 16)
	pdf.CellFormat(0, 10, "客户报价单 / CUSTOMER QUOTATION", "", 1, "C", false, 0, "")
	pdf.SetFont(pdffont.Name, "", 9)
	meta := []string{
		"报价单号: " + q.QuoteNo,
		"客户: " + pdfText(q.CustomerName),
		"币种: " + q.Currency + "    贸易术语: " + q.Incoterm,
		"装运港: " + pdfText(q.PortOfLoading) + "    目的港: " + pdfText(q.PortOfDischarge),
		"付款方式: " + pdfText(q.PaymentMethod) + "    有效期至: " + q.ValidUntil,
	}
	for _, line := range meta {
		pdf.MultiCell(0, 5, line, "", "L", false)
	}
	pdf.Ln(3)
	// 所有列宽之和严格等于 A4 可用宽度 186mm，避免数量和金额互相覆盖。
	widths := []float64{8, 17, 30, 45, 17, 12, 25, 32}
	headers := []string{"#", "编码", "产品", "规格", "数量", "单位", "单价", "金额"}
	drawQuotationPDFRow(pdf, headers, widths, true)
	pdf.SetFont(pdffont.Name, "", 8)
	for _, item := range items {
		values := []string{
			strconv.Itoa(int(item.LineNo)), item.ProductCode, pdfText(item.ProductName), pdfText(item.Spec),
			item.Qty, item.UomCode, item.UnitPrice, item.Amount,
		}
		rowHeight := quotationPDFRowHeight(pdf, values, widths, 4.5)
		if pdf.GetY()+rowHeight > 285 {
			pdf.AddPage()
			drawQuotationPDFRow(pdf, headers, widths, true)
			pdf.SetFont(pdffont.Name, "", 8)
		}
		drawQuotationPDFRow(pdf, values, widths, false)
	}
	if len(shipments) > 0 {
		pdf.Ln(4)
		pdf.SetFont(pdffont.Name, "B", 10)
		pdf.CellFormat(0, 7, "货运批次 / FREIGHT BATCHES", "", 1, "L", false, 0, "")
		shipWidths := []float64{10, 31, 25, 24, 30, 26, 20, 20}
		shipHeaders := []string{"批次", "承运人/货代", "服务", "运费", "航线", "ETD / ETA", "有效期", "备注"}
		drawQuotationPDFRow(pdf, shipHeaders, shipWidths, true)
		pdf.SetFont(pdffont.Name, "", 8)
		for _, shipment := range shipments {
			carrier := shipment.CarrierForwarder
			if shipment.CustomerManaged {
				carrier = "客户自理运输"
			}
			values := []string{strconv.Itoa(int(shipment.BatchNo)), carrier, shipment.ServiceOptionName,
				shipment.Currency + " " + shipment.FreightAmount, shipment.PortOfLoading + " → " + shipment.PortOfDischarge,
				shipment.EstimatedDeparture + " / " + shipment.EstimatedArrival, shipment.ValidUntil, shipment.Remark}
			rowHeight := quotationPDFRowHeight(pdf, values, shipWidths, 4.5)
			if pdf.GetY()+rowHeight > 285 {
				pdf.AddPage()
				drawQuotationPDFRow(pdf, shipHeaders, shipWidths, true)
				pdf.SetFont(pdffont.Name, "", 8)
			}
			drawQuotationPDFRow(pdf, values, shipWidths, false)
		}
	}
	pdf.SetFont(pdffont.Name, "B", 9)
	pdf.CellFormat(154, 8, "合计 "+q.Currency, "1", 0, "R", false, 0, "")
	pdf.CellFormat(32, 8, q.TotalAmount, "1", 1, "R", false, 0, "")
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func quotationPDFRowHeight(pdf *fpdf.Fpdf, values []string, widths []float64, lineHeight float64) float64 {
	maxLines := 1
	for index, value := range values {
		lines := pdf.SplitText(pdfText(value), widths[index]-2)
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	return float64(maxLines)*lineHeight + 2
}

func drawQuotationPDFRow(pdf *fpdf.Fpdf, values []string, widths []float64, header bool) {
	lineHeight := 4.5
	if header {
		pdf.SetFont(pdffont.Name, "B", 8)
	}
	rowHeight := quotationPDFRowHeight(pdf, values, widths, lineHeight)
	startX, startY := pdf.GetX(), pdf.GetY()
	leftX := startX
	for index, value := range values {
		width := widths[index]
		pdf.Rect(startX, startY, width, rowHeight, "")
		pdf.SetXY(startX+1, startY+1)
		align := "L"
		if header {
			align = "C"
		} else if index >= 4 {
			align = "R"
		}
		pdf.MultiCell(width-2, lineHeight, pdfText(value), "", align, false)
		startX += width
	}
	pdf.SetXY(leftX, startY+rowHeight)
}

func pdfText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, value)
}
