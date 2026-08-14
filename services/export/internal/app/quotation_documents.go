package app

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"codeberg.org/go-pdf/fpdf"

	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func (s *Service) GetQuotationWorkbook(ctx context.Context, tenantID, id int64, op Operator) (string, []byte, error) {
	q, items, err := s.GetQuotationFor(ctx, tenantID, id, op)
	if err != nil {
		return "", nil, err
	}
	data, err := buildQuotationWorkbook(q, items)
	return q.QuoteNo + ".xlsx", data, err
}

func buildQuotationWorkbook(q store.GetQuotationRow, items []store.ListQuotationItemsRow) ([]byte, error) {
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
	totalRow := len(rows) + 1
	rows = append(rows, []string{"", "", "", "", "", "", "Total", q.TotalAmount, ""})
	if len(items) > 0 {
		formulas[fmt.Sprintf("H%d", totalRow)] = xlsx.Formula{
			Expression: fmt.Sprintf("SUM(H5:H%d)", totalRow-1), CachedValue: q.TotalAmount,
		}
	}
	return xlsx.BuildWithFormulas("Customer Quotation", rows, formulas)
}

func (s *Service) GetQuotationPDF(ctx context.Context, tenantID, id int64, op Operator) (string, []byte, error) {
	q, items, err := s.GetQuotationFor(ctx, tenantID, id, op)
	if err != nil {
		return "", nil, err
	}
	data, err := buildQuotationPDF(q, items)
	return q.QuoteNo + ".pdf", data, err
}

func buildQuotationPDF(q store.GetQuotationRow, items []store.ListQuotationItemsRow) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(12, 12, 12)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 10, "CUSTOMER QUOTATION", "", 1, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	meta := []string{
		"Quotation: " + q.QuoteNo,
		"Customer: " + pdfText(q.CustomerName),
		"Currency: " + q.Currency + "    Incoterm: " + q.Incoterm,
		"Loading port: " + pdfText(q.PortOfLoading) + "    Discharge port: " + pdfText(q.PortOfDischarge),
		"Payment terms: " + pdfText(q.PaymentMethod) + "    Valid until: " + q.ValidUntil,
	}
	for _, line := range meta {
		pdf.CellFormat(0, 5, line, "", 1, "L", false, 0, "")
	}
	pdf.Ln(3)
	widths := []float64{10, 25, 48, 48, 18, 20, 22}
	headers := []string{"#", "Code", "Product", "Specification", "Qty", "Unit price", "Amount"}
	pdf.SetFont("Helvetica", "B", 8)
	for i, header := range headers {
		pdf.CellFormat(widths[i], 7, header, "1", 0, "C", false, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetFont("Helvetica", "", 8)
	for _, item := range items {
		values := []string{strconv.Itoa(int(item.LineNo)), item.ProductCode, pdfText(item.ProductName), pdfText(item.Spec), item.Qty + " " + item.UomCode, item.UnitPrice, item.Amount}
		for i, value := range values {
			pdf.CellFormat(widths[i], 7, value, "1", 0, map[bool]string{true: "R", false: "L"}[i >= 4], false, 0, "")
		}
		pdf.Ln(-1)
	}
	pdf.SetFont("Helvetica", "B", 9)
	pdf.CellFormat(169, 8, "Total "+q.Currency, "1", 0, "R", false, 0, "")
	pdf.CellFormat(22, 8, q.TotalAmount, "1", 1, "R", false, 0, "")
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func pdfText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r > 255 {
			return '?'
		}
		return r
	}, value)
}
