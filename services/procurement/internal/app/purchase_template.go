package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/xlsx"
)

const purchaseTemplateVersion = "PO_TEMPLATE_V1"

var purchaseTemplateHeaders = []string{
	"Template Version", "Requirement ID", "Product ID", "SKU ID", "UOM ID",
	"Contract No", "Product Code", "Product Name", "Specification", "Available Quantity",
	"Order Quantity", "Unit", "Supplier ID", "Supplier Code", "Currency", "Unit Price",
	"Expected Date", "MOQ", "Payment Terms", "Error",
}

type PurchaseTemplateWorkbook struct {
	FileName, Version string
	Data              []byte
}

func (s *Service) ExportPurchaseTemplate(ctx context.Context, tenantID int64, requirementIDs []int64) (PurchaseTemplateWorkbook, error) {
	if len(requirementIDs) == 0 || len(requirementIDs) > 200 {
		return PurchaseTemplateWorkbook{}, apierr.Invalid("PO_TEMPLATE_REQUIREMENTS_REQUIRED", "请选择 1 至 200 条待采购需求")
	}
	seen := make(map[int64]bool, len(requirementIDs))
	rows := [][]string{purchaseTemplateHeaders}
	for _, id := range requirementIDs {
		if id <= 0 || seen[id] {
			return PurchaseTemplateWorkbook{}, apierr.Invalid("PO_TEMPLATE_REQUIREMENT_INVALID", "采购需求选择无效或重复")
		}
		seen[id] = true
		req, err := s.GetRequirement(ctx, tenantID, id)
		if err != nil {
			return PurchaseTemplateWorkbook{}, err
		}
		open, err := openRequirementQty(req.RequiredQty, req.OrderedQty)
		if err != nil || open.LessThanOrEqual(decimal.Zero) || (req.Status != "PENDING" && req.Status != "PARTIALLY_ORDERED") {
			return PurchaseTemplateWorkbook{}, apierr.Conflict("PO_TEMPLATE_REQUIREMENT_CLOSED", "所选采购需求已没有可下单数量")
		}
		rows = append(rows, []string{
			purchaseTemplateVersion, strconv.FormatInt(req.ID, 10), strconv.FormatInt(req.ProductID, 10), strconv.FormatInt(req.SkuID, 10), strconv.FormatInt(req.UomID, 10),
			req.ContractNo, req.ProductCode, req.ProductName, req.Spec, open.String(), open.String(), req.UomCode,
			"", "", "", "", req.RequiredDate, "", "", "",
		})
	}
	data, err := xlsx.Build("Purchase Import", rows)
	if err != nil {
		return PurchaseTemplateWorkbook{}, err
	}
	return PurchaseTemplateWorkbook{FileName: fmt.Sprintf("purchase-import-%s.xlsx", time.Now().Format("20060102-150405")), Data: data, Version: purchaseTemplateVersion}, nil
}

type PurchaseTemplateImportLine struct {
	RowNo                                     int32
	RequirementID                             int64
	ProductName, Qty, UomCode, UnitPrice, MOQ string
}
type PurchaseTemplateImportGroup struct {
	ImportToken                          string
	Supplier                             Supplier
	Currency, ExpectedDate, PaymentTerms string
	Lines                                []PurchaseTemplateImportLine
}
type PurchaseTemplateImportResult struct {
	Version, ErrorFileName string
	ErrorFileData          []byte
	Groups                 []PurchaseTemplateImportGroup
}

type parsedPurchaseTemplateRow struct {
	rowNo                                                                   int32
	cells                                                                   []string
	requirementID, productID, skuID, uomID, supplierID                      int64
	qty, supplierCode, currency, unitPrice, expectedDate, moq, paymentTerms string
	requirementName, uomCode                                                string
	err                                                                     string
}

func (s *Service) PreviewPurchaseTemplateImport(ctx context.Context, tenantID int64, data []byte, sourceFileName string, op Operator) (PurchaseTemplateImportResult, error) {
	rows, err := parsePurchaseTemplate(data)
	if err != nil {
		return PurchaseTemplateImportResult{}, err
	}
	result := PurchaseTemplateImportResult{Version: purchaseTemplateVersion, Groups: []PurchaseTemplateImportGroup{}}
	groups := make(map[int64]*PurchaseTemplateImportGroup)
	seenRequirement := make(map[int64]bool)
	for i := range rows {
		row := &rows[i]
		if seenRequirement[row.requirementID] {
			row.err = "采购需求重复"
		} else {
			seenRequirement[row.requirementID] = true
		}
		req, getErr := s.GetRequirement(ctx, tenantID, row.requirementID)
		if getErr != nil {
			row.err = "采购需求不存在"
		} else {
			open, openErr := openRequirementQty(req.RequiredQty, req.OrderedQty)
			qty, qtyErr := decimal.NewFromString(row.qty)
			switch {
			case req.ProductID != row.productID || req.SkuID != row.skuID || req.UomID != row.uomID:
				row.err = "内部产品、SKU 或单位 ID 已被修改"
			case req.Status != "PENDING" && req.Status != "PARTIALLY_ORDERED":
				row.err = "采购需求已关闭"
			case openErr != nil || qtyErr != nil || qty.LessThanOrEqual(decimal.Zero) || qty.GreaterThan(open):
				row.err = "下单数量无效或超过可下单数量"
			default:
				row.requirementName, row.uomCode = req.ProductName, req.UomCode
			}
		}
		price, priceErr := decimal.NewFromString(orZero(row.unitPrice))
		if row.err == "" && (priceErr != nil || price.IsNegative()) {
			row.err = "单价必须为非负数"
		}
		if row.err == "" && row.moq != "" {
			moq, moqErr := decimal.NewFromString(row.moq)
			if moqErr != nil || moq.IsNegative() {
				row.err = "MOQ 必须为非负数"
			}
		}
		if row.err == "" && row.expectedDate != "" {
			if _, dateErr := time.Parse("2006-01-02", row.expectedDate); dateErr != nil {
				row.err = "预计交期必须为 YYYY-MM-DD"
			}
		}
		var supplier Supplier
		if row.err == "" {
			if row.supplierID <= 0 || row.supplierCode == "" {
				row.err = "供应商 ID 和编码不能为空"
			} else {
				supplier, err = s.suppliers.Get(ctx, row.supplierID)
				if err != nil || supplier.Status != "ACTIVE" || !strings.EqualFold(strings.TrimSpace(supplier.Code), row.supplierCode) {
					row.err = "供应商不存在、已停用或编码不匹配"
				}
			}
		}
		if row.currency == "" && supplier.Currency != "" {
			row.currency = strings.ToUpper(supplier.Currency)
			row.cells[14] = row.currency
		}
		if row.err == "" && len(row.currency) != 3 {
			row.err = "币种必须为三位代码"
		}
		if row.err != "" {
			continue
		}
		group := groups[supplier.ID]
		if group != nil && (group.Currency != row.currency || group.ExpectedDate != row.expectedDate || group.PaymentTerms != row.paymentTerms) {
			row.err = "同一供应商的币种、预计交期和付款条件必须一致"
			continue
		}
		if group == nil {
			group = &PurchaseTemplateImportGroup{Supplier: supplier, Currency: row.currency, ExpectedDate: row.expectedDate, PaymentTerms: row.paymentTerms}
			groups[supplier.ID] = group
		}
		group.Lines = append(group.Lines, PurchaseTemplateImportLine{RowNo: row.rowNo, RequirementID: row.requirementID, ProductName: row.requirementName, Qty: row.qty, UomCode: row.uomCode, UnitPrice: row.unitPrice, MOQ: row.moq})
	}
	for _, row := range rows {
		if row.err != "" {
			result.ErrorFileName = "purchase-import-errors.xlsx"
			result.ErrorFileData, _ = buildPurchaseTemplateErrors(rows)
			return result, nil
		}
	}
	hash := sha256.Sum256(data)
	for _, group := range groups {
		importRows := make([]OrderImportRow, 0, len(group.Lines))
		for _, line := range group.Lines {
			for _, parsed := range rows {
				if parsed.rowNo == line.RowNo {
					importRows = append(importRows, OrderImportRow{RowNo: line.RowNo, Product: line.ProductName, QuantityUnit: line.UomCode, Quantity: line.Qty, UnitPrice: line.UnitPrice, RequirementID: line.RequirementID, ProductID: parsed.productID, SKUID: parsed.skuID, UomID: parsed.uomID})
					break
				}
			}
		}
		preview, previewErr := s.PreviewOrderImport(ctx, tenantID, PreviewOrderImportInput{SourceType: "PURCHASE_TEMPLATE", SourceFileName: sourceFileName, FileSHA256: hex.EncodeToString(hash[:]), Rows: importRows}, op)
		if previewErr != nil {
			return PurchaseTemplateImportResult{}, previewErr
		}
		for _, row := range preview.Rows {
			if row.Result != "MATCHED" {
				return PurchaseTemplateImportResult{}, apierr.Conflict("PO_TEMPLATE_REQUIREMENT_CHANGED", "采购需求状态已变化，请重新导出模板")
			}
		}
		group.ImportToken = preview.ImportToken
		result.Groups = append(result.Groups, *group)
	}
	return result, nil
}

func parsePurchaseTemplate(data []byte) ([]parsedPurchaseTemplateRow, error) {
	rows, err := xlsx.Parse(data)
	if err != nil {
		return nil, apierr.Invalid("PO_TEMPLATE_FILE_INVALID", "文件不是有效的 XLSX")
	}
	if len(rows) < 2 || len(rows[0]) < len(purchaseTemplateHeaders) {
		return nil, apierr.Invalid("PO_TEMPLATE_VERSION_INVALID", "请使用系统导出的采购模板")
	}
	for i, header := range purchaseTemplateHeaders {
		if strings.TrimSpace(rows[0][i]) != header {
			return nil, apierr.Invalid("PO_TEMPLATE_HEADER_INVALID", "采购模板列名或顺序已被修改")
		}
	}
	result := make([]parsedPurchaseTemplateRow, 0, len(rows)-1)
	for index, cells := range rows[1:] {
		for len(cells) < len(purchaseTemplateHeaders) {
			cells = append(cells, "")
		}
		if strings.TrimSpace(strings.Join(cells, "")) == "" {
			continue
		}
		row := parsedPurchaseTemplateRow{rowNo: int32(index + 2), cells: cells, qty: strings.TrimSpace(cells[10]), supplierCode: strings.TrimSpace(cells[13]), currency: strings.ToUpper(strings.TrimSpace(cells[14])), unitPrice: strings.TrimSpace(cells[15]), expectedDate: strings.TrimSpace(cells[16]), moq: strings.TrimSpace(cells[17]), paymentTerms: strings.TrimSpace(cells[18])}
		if cells[0] != purchaseTemplateVersion {
			row.err = "模板版本不受支持"
		}
		ids := []*int64{&row.requirementID, &row.productID, &row.skuID, &row.uomID, &row.supplierID}
		indexes := []int{1, 2, 3, 4, 12}
		for i, target := range ids {
			value, convErr := strconv.ParseInt(strings.TrimSpace(cells[indexes[i]]), 10, 64)
			if convErr != nil || (i != 2 && value <= 0) {
				row.err = "内部 ID 格式无效"
			} else {
				*target = value
			}
		}
		result = append(result, row)
	}
	if len(result) == 0 {
		return nil, apierr.Invalid("PO_TEMPLATE_LINES_REQUIRED", "采购模板没有明细")
	}
	return result, nil
}

func buildPurchaseTemplateErrors(rows []parsedPurchaseTemplateRow) ([]byte, error) {
	out := [][]string{purchaseTemplateHeaders}
	for _, row := range rows {
		cells := append([]string(nil), row.cells...)
		for len(cells) < len(purchaseTemplateHeaders) {
			cells = append(cells, "")
		}
		cells[19] = row.err
		out = append(out, cells[:len(purchaseTemplateHeaders)])
	}
	return xlsx.Build("Purchase Import Errors", out)
}

func openRequirementQty(required, ordered string) (decimal.Decimal, error) {
	r, err := decimal.NewFromString(required)
	if err != nil {
		return decimal.Zero, err
	}
	o, err := decimal.NewFromString(ordered)
	if err != nil {
		return decimal.Zero, err
	}
	return r.Sub(o), nil
}
