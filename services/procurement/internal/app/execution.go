package app

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

//go:embed fonts/NotoSansSC-VF.ttf
var purchaseOrderPDFFont []byte

const (
	PurchaseOrderDocumentVersion = "PO_SUPPLIER_V1"
	BizTypePurchaseOrderChange   = "PURCHASE_ORDER_CHANGE"
)

type OrderDocuments struct {
	XLSXFileName, PDFFileName, Version string
	XLSXData, PDFData                  []byte
}

type SupplierConfirmationLine struct {
	POItemID                         int64
	ConfirmedQty, ConfirmedUnitPrice string
}

type SupplierConfirmation struct {
	ID, ApprovalInstanceID              int64
	Status, ConfirmedDate, ExpectedDate string
	Remark, CreatedBy, CreatedAt        string
	Lines                               []SupplierConfirmationLine
}

type ProductionAttachment struct{ FileName, FileURL, ContentType string }

type ProductionMilestone struct {
	ID, OwnerID                                      int64
	Node, PlannedDate, ActualDate, OwnerName, Remark string
	Delayed                                          bool
	Attachments                                      []ProductionAttachment
	UpdatedAt                                        string
}

type ReceiptException struct {
	ID, ReceiptID, POItemID                                    int64
	Type, Qty, ActualProduct, ActualUOM, Description, Status   string
	Resolution, ReportedBy, ReportedAt, ResolvedBy, ResolvedAt string
}

type ProductionReminder struct {
	ID, BuyerID                                                       int64
	Node, PlannedDate, BuyerName, RelatedContracts, Status, CreatedAt string
}

type OrderExecution struct {
	Confirmations []SupplierConfirmation
	Milestones    []ProductionMilestone
	Exceptions    []ReceiptException
	Reminders     []ProductionReminder
}

func (s *Service) GetOrderDocuments(ctx context.Context, tenantID, id int64) (OrderDocuments, error) {
	head, err := s.GetOrder(ctx, tenantID, id)
	if err != nil {
		return OrderDocuments{}, err
	}
	if head.Status == poDraft || head.Status == poPending || head.Status == "REJECTED" || head.Status == "CANCELLED" {
		return OrderDocuments{}, apierr.Conflict("PO_DOCUMENT_NOT_APPROVED", "采购单审批通过后才能生成供应商文件")
	}
	items, err := s.OrderItems(ctx, tenantID, id)
	if err != nil {
		return OrderDocuments{}, err
	}
	return buildPurchaseOrderDocuments(head, items)
}

// buildPurchaseOrderDocuments 使用同一份采购单快照生成 Excel 和 PDF，保证两种下载内容一致。
func buildPurchaseOrderDocuments(head store.GetPurchaseOrderRow, items []store.PurchaseOrderItemsRow) (OrderDocuments, error) {
	rows := [][]string{
		{"模板版本 / Version", "采购单号 / Purchase Order", "供应商 / Supplier", "币种 / Currency", "要求到货日期 / Required Date", "采购员 / Buyer"},
		{PurchaseOrderDocumentVersion, head.PoNo, head.SupplierName, head.Currency, head.ExpectedDate, head.BuyerName},
		{},
		{"序号 / Line", "产品编码 / Product Code", "产品 / Product", "规格 / Specification", "数量 / Quantity", "单位 / Unit", "单价 / Unit Price", "金额 / Amount"},
	}
	formulas := make(map[string]xlsx.Formula, len(items)+1)
	for index, item := range items {
		rowNo := index + 5
		rows = append(rows, []string{strconv.Itoa(index + 1), item.ProductCode, item.ProductName, item.Spec, item.Qty, item.UomCode, item.UnitPrice, item.Amount})
		formulas[fmt.Sprintf("H%d", rowNo)] = xlsx.Formula{Expression: fmt.Sprintf("E%d*G%d", rowNo, rowNo), CachedValue: item.Amount}
	}
	totalRow := len(rows) + 1
	rows = append(rows, []string{"", "", "", "", "", "", "合计 / Total", head.TotalAmount})
	if len(items) > 0 {
		formulas[fmt.Sprintf("H%d", totalRow)] = xlsx.Formula{Expression: fmt.Sprintf("SUM(H5:H%d)", totalRow-1), CachedValue: head.TotalAmount}
	}
	xlsxData, err := xlsx.BuildWithFormulas("采购订单 Purchase Order", rows, formulas)
	if err != nil {
		return OrderDocuments{}, err
	}
	pdfData, err := buildPurchaseOrderPDF(head, items)
	if err != nil {
		return OrderDocuments{}, err
	}
	return OrderDocuments{XLSXFileName: head.PoNo + ".xlsx", XLSXData: xlsxData, PDFFileName: head.PoNo + ".pdf", PDFData: pdfData, Version: PurchaseOrderDocumentVersion}, nil
}

// buildPurchaseOrderPDF 生成支持中文、自动换行且不会越过 A4 可用宽度的采购订单。
func buildPurchaseOrderPDF(head store.GetPurchaseOrderRow, items []store.PurchaseOrderItemsRow) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 12)
	pdf.AddUTF8FontFromBytes("NotoSansSC", "", purchaseOrderPDFFont)
	pdf.AddUTF8FontFromBytes("NotoSansSC", "B", purchaseOrderPDFFont)
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("load purchase order PDF font: %w", err)
	}
	pdf.AddPage()
	pdf.SetFont("NotoSansSC", "B", 16)
	pdf.CellFormat(0, 10, "采购订单 / PURCHASE ORDER", "", 1, "C", false, 0, "")
	pdf.SetFont("NotoSansSC", "", 9)
	for _, line := range []string{
		"采购单号 / Order: " + head.PoNo,
		"供应商 / Supplier: " + executionPDFText(head.SupplierName),
		"币种 / Currency: " + head.Currency,
		"要求到货日期 / Required date: " + head.ExpectedDate,
		"采购员 / Buyer: " + executionPDFText(head.BuyerName),
	} {
		pdf.MultiCell(0, 5, line, "", "L", false)
	}
	pdf.Ln(3)
	// 所有列宽之和严格等于 A4 可用宽度 186mm，数量和单位单独成列。
	widths := []float64{8, 17, 30, 45, 17, 12, 25, 32}
	headers := []string{"#", "编码", "产品", "规格", "数量", "单位", "单价", "金额"}
	drawPurchaseOrderPDFRow(pdf, headers, widths, true)
	pdf.SetFont("NotoSansSC", "", 8)
	for index, item := range items {
		values := []string{strconv.Itoa(index + 1), item.ProductCode, executionPDFText(item.ProductName), executionPDFText(item.Spec), item.Qty, item.UomCode, item.UnitPrice, item.Amount}
		rowHeight := purchaseOrderPDFRowHeight(pdf, values, widths, 4.5)
		if pdf.GetY()+rowHeight > 285 {
			pdf.AddPage()
			drawPurchaseOrderPDFRow(pdf, headers, widths, true)
			pdf.SetFont("NotoSansSC", "", 8)
		}
		drawPurchaseOrderPDFRow(pdf, values, widths, false)
	}
	pdf.SetFont("NotoSansSC", "B", 9)
	pdf.CellFormat(154, 8, "合计 / Total "+head.Currency, "1", 0, "R", false, 0, "")
	pdf.CellFormat(32, 8, head.TotalAmount, "1", 1, "R", false, 0, "")
	var pdfOut bytes.Buffer
	if err := pdf.Output(&pdfOut); err != nil {
		return nil, err
	}
	return pdfOut.Bytes(), nil
}

func purchaseOrderPDFRowHeight(pdf *fpdf.Fpdf, values []string, widths []float64, lineHeight float64) float64 {
	maxLines := 1
	for index, value := range values {
		lines := pdf.SplitText(executionPDFText(value), widths[index]-2)
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	return float64(maxLines)*lineHeight + 2
}

func drawPurchaseOrderPDFRow(pdf *fpdf.Fpdf, values []string, widths []float64, header bool) {
	lineHeight := 4.5
	if header {
		pdf.SetFont("NotoSansSC", "B", 8)
	}
	rowHeight := purchaseOrderPDFRowHeight(pdf, values, widths, lineHeight)
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
		pdf.MultiCell(width-2, lineHeight, executionPDFText(value), "", align, false)
		startX += width
	}
	pdf.SetXY(leftX, startY+rowHeight)
}

func executionPDFText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, value)
}

func (s *Service) BeginOrderSend(ctx context.Context, tenantID, poID int64, recipient string, senderID int64, senderName string) (int64, error) {
	if _, err := mail.ParseAddress(strings.TrimSpace(recipient)); err != nil {
		return 0, apierr.Invalid("PO_SEND_EMAIL_INVALID", "请填写有效的供应商邮箱")
	}
	if senderID <= 0 {
		return 0, apierr.Invalid("PO_SEND_SENDER_REQUIRED", "请选择采购发件邮箱")
	}
	var attemptID int64
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var status, sendStatus string
		err := tx.QueryRow(ctx, `SELECT status, send_status FROM purchase_orders WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, poID).Scan(&status, &sendStatus)
		if err == pgx.ErrNoRows {
			return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
		}
		if err != nil {
			return err
		}
		if status != poOrdered && status != poPartial && status != poReceived {
			return apierr.Conflict("PO_SEND_NOT_APPROVED", "采购单审批通过后才能正式发单")
		}
		if sendStatus == "SENT" {
			return apierr.Conflict("PO_ALREADY_SENT", "采购单已经发送给供应商")
		}
		if sendStatus == "SENDING" {
			return apierr.Conflict("PO_SEND_IN_PROGRESS", "采购单正在发送，请勿重复操作")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO purchase_order_send_attempts (tenant_id,po_id,recipient_email,sender_employee_id,sender_name,status) VALUES ($1,$2,$3,$4,$5,'SENDING') RETURNING id`, tenantID, poID, strings.TrimSpace(recipient), senderID, strings.TrimSpace(senderName)).Scan(&attemptID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE purchase_orders SET send_status='SENDING',sent_to=$3,sent_by_id=$4,sent_by_name=$5,send_error='',updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, poID, strings.TrimSpace(recipient), senderID, strings.TrimSpace(senderName))
		return err
	})
	return attemptID, err
}

func (s *Service) CompleteOrderSend(ctx context.Context, tenantID, poID, attemptID int64, success bool, campaignID int64, campaignNo, errorMessage string, attachments []string, version string) (string, error) {
	status := "FAILED"
	if success {
		status = "SENT"
	}
	attachmentJSON, _ := json.Marshal(attachments)
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var attemptStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM purchase_order_send_attempts WHERE tenant_id=$1 AND po_id=$2 AND id=$3 FOR UPDATE`, tenantID, poID, attemptID).Scan(&attemptStatus); err == pgx.ErrNoRows {
			return apierr.NotFound("PO_SEND_ATTEMPT_NOT_FOUND", "发单记录不存在")
		} else if err != nil {
			return err
		}
		if attemptStatus != "SENDING" {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE purchase_order_send_attempts SET status=$4,campaign_id=$5,campaign_no=$6,attachment_names=$7,template_version=$8,error_message=$9,completed_at=now() WHERE tenant_id=$1 AND po_id=$2 AND id=$3`, tenantID, poID, attemptID, status, campaignID, campaignNo, attachmentJSON, version, strings.TrimSpace(errorMessage))
		if err != nil {
			return err
		}
		if success {
			_, err = tx.Exec(ctx, `UPDATE purchase_orders SET send_status='SENT',sent_at=now(),send_error='',send_template_version=$3,send_attachment_names=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, poID, version, attachmentJSON)
		} else {
			_, err = tx.Exec(ctx, `UPDATE purchase_orders SET send_status='FAILED',send_error=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, poID, strings.TrimSpace(errorMessage))
		}
		return err
	})
	if err == nil {
		s.nudge(ctx, tenantID)
	}
	return status, err
}

func (s *Service) RecordSupplierConfirmation(ctx context.Context, tenantID, poID int64, confirmedDate, expectedDate, remark string, lines []SupplierConfirmationLine, op Operator) (SupplierConfirmation, error) {
	if confirmedDate == "" {
		confirmedDate = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", confirmedDate); err != nil {
		return SupplierConfirmation{}, apierr.Invalid("PO_CONFIRM_DATE_INVALID", "供应商确认日期无效")
	}
	if expectedDate != "" {
		if _, err := time.Parse("2006-01-02", expectedDate); err != nil {
			return SupplierConfirmation{}, apierr.Invalid("PO_CONFIRM_EXPECTED_INVALID", "供应商确认交期无效")
		}
	}
	head, err := s.GetOrder(ctx, tenantID, poID)
	if err != nil {
		return SupplierConfirmation{}, err
	}
	// 采购审批通过并生成采购单后，采购单已经进入履约阶段。发送邮件只是
	// 沟通方式之一（也可能通过电话、微信或线下完成），不能作为供应商确认的前置条件。
	if head.Status != poOrdered && head.Status != poPartial && head.Status != poReceived {
		return SupplierConfirmation{}, apierr.Conflict("PO_CONFIRM_NOT_ORDERED", "只有已下单采购单可以记录供应商确认")
	}
	items, err := s.OrderItems(ctx, tenantID, poID)
	if err != nil {
		return SupplierConfirmation{}, err
	}
	if len(lines) != len(items) {
		return SupplierConfirmation{}, apierr.Invalid("PO_CONFIRM_LINES_REQUIRED", "请确认采购单全部明细")
	}
	byID := make(map[int64]SupplierConfirmationLine, len(lines))
	for _, line := range lines {
		byID[line.POItemID] = line
	}
	for _, item := range items {
		line, ok := byID[item.ID]
		if !ok {
			return SupplierConfirmation{}, apierr.Invalid("PO_CONFIRM_ITEM_INVALID", "供应商确认明细不属于采购单")
		}
		qty, qtyErr := decimal.NewFromString(line.ConfirmedQty)
		price, priceErr := decimal.NewFromString(line.ConfirmedUnitPrice)
		if qtyErr != nil || qty.LessThanOrEqual(decimal.Zero) || priceErr != nil || price.IsNegative() {
			return SupplierConfirmation{}, apierr.Invalid("PO_CONFIRM_VALUE_INVALID", "供应商确认数量或价格无效")
		}
	}
	// 供应商确认属于采购单审批完成后的履约记录，不再次发起审批。
	// 原采购数量、单价和交期不会被覆盖；确认值会与原值同时保存，供进度计算和差异追溯。
	const status = "MATCHED"
	const instanceID int64 = 0
	var confirmationID int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO purchase_supplier_confirmations (tenant_id,po_id,status,confirmed_date,confirmed_expected_date,remark,approval_instance_id,created_by_id,created_by_name) VALUES ($1,$2,$3,$4::date,nullif($5,'')::date,$6,nullif($7,0),$8,$9) RETURNING id`, tenantID, poID, status, confirmedDate, expectedDate, strings.TrimSpace(remark), instanceID, op.ID, op.Name).Scan(&confirmationID); err != nil {
			return err
		}
		for _, item := range items {
			line := byID[item.ID]
			if _, err := tx.Exec(ctx, `INSERT INTO purchase_supplier_confirmation_lines (tenant_id,confirmation_id,po_item_id,original_qty,original_unit_price,confirmed_qty,confirmed_unit_price) VALUES ($1,$2,$3,$4::numeric,$5::numeric,$6::numeric,$7::numeric)`, tenantID, confirmationID, item.ID, item.Qty, item.UnitPrice, line.ConfirmedQty, line.ConfirmedUnitPrice); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return SupplierConfirmation{}, err
	}
	s.nudge(ctx, tenantID)
	execution, err := s.GetOrderExecution(ctx, tenantID, poID)
	if err != nil {
		return SupplierConfirmation{}, err
	}
	for _, confirmation := range execution.Confirmations {
		if confirmation.ID == confirmationID {
			return confirmation, nil
		}
	}
	return SupplierConfirmation{}, apierr.Internal("PO_CONFIRM_READ_FAILED", "供应商确认保存后无法读取")
}

func (s *Service) ApplyConfirmationApproval(ctx context.Context, tenantID, poID, instanceID int64, result string) error {
	status := "REJECTED"
	if result == "APPROVED" {
		status = "APPROVED"
	}
	command, err := s.pool.Exec(ctx, `UPDATE purchase_supplier_confirmations SET status=$4 WHERE tenant_id=$1 AND po_id=$2 AND approval_instance_id=$3 AND status='PENDING_APPROVAL'`, tenantID, poID, instanceID, status)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return nil
	}
	s.nudge(ctx, tenantID)
	return nil
}

var productionNodes = map[string]bool{"PENDING_SCHEDULE": true, "SCHEDULED": true, "IN_PRODUCTION": true, "QUALITY_INSPECTION": true, "READY_TO_SHIP": true, "SENT_TO_PORT": true}

func (s *Service) SaveProductionMilestone(ctx context.Context, tenantID, poID int64, milestone ProductionMilestone, op Operator) (ProductionMilestone, error) {
	if !productionNodes[milestone.Node] {
		return ProductionMilestone{}, apierr.Invalid("PO_PRODUCTION_NODE_INVALID", "生产节点无效")
	}
	for _, value := range []string{milestone.PlannedDate, milestone.ActualDate} {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return ProductionMilestone{}, apierr.Invalid("PO_PRODUCTION_DATE_INVALID", "生产节点日期无效")
			}
		}
	}
	head, err := s.GetOrder(ctx, tenantID, poID)
	if err != nil {
		return ProductionMilestone{}, err
	}
	if head.Status != poOrdered && head.Status != poPartial && head.Status != poReceived {
		return ProductionMilestone{}, apierr.Conflict("PO_PRODUCTION_NOT_ORDERED", "只有已下单采购单可以维护生产进度")
	}
	attachments, _ := json.Marshal(milestone.Attachments)
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO purchase_production_milestones (tenant_id,po_id,node,planned_date,actual_date,owner_id,owner_name,remark,attachments,updated_by_id,updated_by_name) VALUES ($1,$2,$3,nullif($4,'')::date,nullif($5,'')::date,$6,$7,$8,$9,$10,$11) ON CONFLICT (tenant_id,po_id,node) DO UPDATE SET planned_date=EXCLUDED.planned_date,actual_date=EXCLUDED.actual_date,owner_id=EXCLUDED.owner_id,owner_name=EXCLUDED.owner_name,remark=EXCLUDED.remark,attachments=EXCLUDED.attachments,updated_by_id=EXCLUDED.updated_by_id,updated_by_name=EXCLUDED.updated_by_name,updated_at=now() RETURNING id`, tenantID, poID, milestone.Node, milestone.PlannedDate, milestone.ActualDate, milestone.OwnerID, milestone.OwnerName, strings.TrimSpace(milestone.Remark), attachments, op.ID, op.Name).Scan(&id); err != nil {
			return err
		}
		delayed := false
		if milestone.PlannedDate != "" && milestone.ActualDate == "" {
			planned, _ := time.Parse("2006-01-02", milestone.PlannedDate)
			delayed = planned.Before(time.Now().Truncate(24 * time.Hour))
		}
		if delayed {
			var contracts string
			_ = tx.QueryRow(ctx, `SELECT coalesce(string_agg(DISTINCT nullif(r.contract_no,''), ', '),'') FROM purchase_order_items i JOIN purchase_requirements r ON r.id=i.requirement_id WHERE i.tenant_id=$1 AND i.po_id=$2`, tenantID, poID).Scan(&contracts)
			command, err := tx.Exec(ctx, `INSERT INTO purchase_production_reminders (tenant_id,po_id,milestone_id,node,planned_date,buyer_id,buyer_name,related_contracts) VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8) ON CONFLICT (tenant_id,milestone_id) DO NOTHING`, tenantID, poID, id, milestone.Node, milestone.PlannedDate, head.BuyerID, head.BuyerName, contracts)
			if err != nil {
				return err
			}
			if command.RowsAffected() > 0 {
				payload, _ := json.Marshal(map[string]any{"po_id": poID, "po_no": head.PoNo, "node": milestone.Node, "planned_date": milestone.PlannedDate, "buyer_id": head.BuyerID, "buyer_name": head.BuyerName, "related_contracts": contracts})
				if _, err := tx.Exec(ctx, `INSERT INTO outbox_events (tenant_id,aggregate_type,aggregate_id,event_type,payload) VALUES ($1,'PURCHASE_ORDER',$2,'procurement.production.delayed',$3)`, tenantID, strconv.FormatInt(poID, 10), payload); err != nil {
					return err
				}
			}
		} else {
			_, err := tx.Exec(ctx, `UPDATE purchase_production_reminders SET status='CLOSED',closed_at=now() WHERE tenant_id=$1 AND milestone_id=$2 AND status='OPEN'`, tenantID, id)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ProductionMilestone{}, err
	}
	s.nudge(ctx, tenantID)
	execution, err := s.GetOrderExecution(ctx, tenantID, poID)
	if err != nil {
		return ProductionMilestone{}, err
	}
	for _, row := range execution.Milestones {
		if row.ID == id {
			return row, nil
		}
	}
	return ProductionMilestone{}, apierr.Internal("PO_PRODUCTION_READ_FAILED", "生产节点保存后无法读取")
}

var receiptExceptionTypes = map[string]bool{"WRONG_PRODUCT": true, "UNIT_MISMATCH": true, "SHORT_SHIPMENT": true, "DAMAGE": true, "QUALITY_DISPUTE": true, "RETURN": true}

func (s *Service) ReportReceiptException(ctx context.Context, tenantID, poID int64, in ReceiptException, op Operator) (ReceiptException, error) {
	if !receiptExceptionTypes[in.Type] {
		return ReceiptException{}, apierr.Invalid("PO_EXCEPTION_TYPE_INVALID", "到货异常类型无效")
	}
	if strings.TrimSpace(in.Description) == "" {
		return ReceiptException{}, apierr.Invalid("PO_EXCEPTION_DESCRIPTION_REQUIRED", "请填写异常说明")
	}
	qty, err := decimal.NewFromString(orZero(in.Qty))
	if err != nil || qty.IsNegative() {
		return ReceiptException{}, apierr.Invalid("PO_EXCEPTION_QTY_INVALID", "异常数量不能为负数")
	}
	if _, err = s.GetOrder(ctx, tenantID, poID); err != nil {
		return ReceiptException{}, err
	}
	if in.ReceiptID != 0 {
		var exists bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_receipts WHERE tenant_id=$1 AND po_id=$2 AND id=$3)`, tenantID, poID, in.ReceiptID).Scan(&exists); err != nil || !exists {
			return ReceiptException{}, apierr.Invalid("PO_EXCEPTION_RECEIPT_INVALID", "到货记录不属于采购单")
		}
	}
	if in.POItemID != 0 {
		var exists bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2 AND id=$3)`, tenantID, poID, in.POItemID).Scan(&exists); err != nil || !exists {
			return ReceiptException{}, apierr.Invalid("PO_EXCEPTION_ITEM_INVALID", "采购明细不属于采购单")
		}
	}
	var id int64
	err = s.pool.QueryRow(ctx, `INSERT INTO purchase_receipt_exceptions (tenant_id,po_id,receipt_id,po_item_id,exception_type,qty,actual_product,actual_uom,description,reported_by_id,reported_by_name) VALUES ($1,$2,nullif($3,0),nullif($4,0),$5,$6::numeric,$7,$8,$9,$10,$11) RETURNING id`, tenantID, poID, in.ReceiptID, in.POItemID, in.Type, qty.String(), strings.TrimSpace(in.ActualProduct), strings.TrimSpace(in.ActualUOM), strings.TrimSpace(in.Description), op.ID, op.Name).Scan(&id)
	if err != nil {
		return ReceiptException{}, err
	}
	s.nudge(ctx, tenantID)
	return s.receiptExceptionByID(ctx, tenantID, poID, id)
}

func (s *Service) ResolveReceiptException(ctx context.Context, tenantID, poID, exceptionID int64, resolution string, op Operator) (ReceiptException, error) {
	if strings.TrimSpace(resolution) == "" {
		return ReceiptException{}, apierr.Invalid("PO_EXCEPTION_RESOLUTION_REQUIRED", "请填写异常处理结果")
	}
	command, err := s.pool.Exec(ctx, `UPDATE purchase_receipt_exceptions SET status='RESOLVED',resolution=$4,resolved_by_id=$5,resolved_by_name=$6,resolved_at=now() WHERE tenant_id=$1 AND po_id=$2 AND id=$3 AND status='OPEN'`, tenantID, poID, exceptionID, strings.TrimSpace(resolution), op.ID, op.Name)
	if err != nil {
		return ReceiptException{}, err
	}
	if command.RowsAffected() == 0 {
		return ReceiptException{}, apierr.Conflict("PO_EXCEPTION_NOT_OPEN", "异常不存在或已经处理")
	}
	s.nudge(ctx, tenantID)
	return s.receiptExceptionByID(ctx, tenantID, poID, exceptionID)
}

func (s *Service) receiptExceptionByID(ctx context.Context, tenantID, poID, id int64) (ReceiptException, error) {
	var row ReceiptException
	err := s.pool.QueryRow(ctx, `SELECT id,coalesce(receipt_id,0),coalesce(po_item_id,0),exception_type,qty::text,actual_product,actual_uom,description,status,resolution,reported_by_name,reported_at::text,resolved_by_name,coalesce(resolved_at::text,'') FROM purchase_receipt_exceptions WHERE tenant_id=$1 AND po_id=$2 AND id=$3`, tenantID, poID, id).Scan(&row.ID, &row.ReceiptID, &row.POItemID, &row.Type, &row.Qty, &row.ActualProduct, &row.ActualUOM, &row.Description, &row.Status, &row.Resolution, &row.ReportedBy, &row.ReportedAt, &row.ResolvedBy, &row.ResolvedAt)
	if err == pgx.ErrNoRows {
		return ReceiptException{}, apierr.NotFound("PO_EXCEPTION_NOT_FOUND", "到货异常不存在")
	}
	return row, err
}

func (s *Service) GetOrderExecution(ctx context.Context, tenantID, poID int64) (OrderExecution, error) {
	if _, err := s.GetOrder(ctx, tenantID, poID); err != nil {
		return OrderExecution{}, err
	}
	var out OrderExecution
	confirmations, err := s.pool.Query(ctx, `SELECT id,status,confirmed_date::text,coalesce(confirmed_expected_date::text,''),remark,coalesce(approval_instance_id,0),created_by_name,created_at::text FROM purchase_supplier_confirmations WHERE tenant_id=$1 AND po_id=$2 ORDER BY created_at DESC,id DESC`, tenantID, poID)
	if err != nil {
		return out, err
	}
	for confirmations.Next() {
		var row SupplierConfirmation
		if err = confirmations.Scan(&row.ID, &row.Status, &row.ConfirmedDate, &row.ExpectedDate, &row.Remark, &row.ApprovalInstanceID, &row.CreatedBy, &row.CreatedAt); err != nil {
			confirmations.Close()
			return out, err
		}
		lineRows, lineErr := s.pool.Query(ctx, `SELECT po_item_id,confirmed_qty::text,confirmed_unit_price::text FROM purchase_supplier_confirmation_lines WHERE tenant_id=$1 AND confirmation_id=$2 ORDER BY id`, tenantID, row.ID)
		if lineErr != nil {
			confirmations.Close()
			return out, lineErr
		}
		for lineRows.Next() {
			var line SupplierConfirmationLine
			if lineErr = lineRows.Scan(&line.POItemID, &line.ConfirmedQty, &line.ConfirmedUnitPrice); lineErr != nil {
				lineRows.Close()
				confirmations.Close()
				return out, lineErr
			}
			row.Lines = append(row.Lines, line)
		}
		lineRows.Close()
		out.Confirmations = append(out.Confirmations, row)
	}
	confirmations.Close()
	milestones, err := s.pool.Query(ctx, `SELECT id,node,coalesce(planned_date::text,''),coalesce(actual_date::text,''),owner_id,owner_name,remark,attachments,updated_at::text,(planned_date < current_date AND actual_date IS NULL) FROM purchase_production_milestones WHERE tenant_id=$1 AND po_id=$2 ORDER BY created_at,id`, tenantID, poID)
	if err != nil {
		return out, err
	}
	for milestones.Next() {
		var row ProductionMilestone
		var raw []byte
		if err = milestones.Scan(&row.ID, &row.Node, &row.PlannedDate, &row.ActualDate, &row.OwnerID, &row.OwnerName, &row.Remark, &raw, &row.UpdatedAt, &row.Delayed); err != nil {
			milestones.Close()
			return out, err
		}
		_ = json.Unmarshal(raw, &row.Attachments)
		out.Milestones = append(out.Milestones, row)
	}
	milestones.Close()
	exceptions, err := s.pool.Query(ctx, `SELECT id,coalesce(receipt_id,0),coalesce(po_item_id,0),exception_type,qty::text,actual_product,actual_uom,description,status,resolution,reported_by_name,reported_at::text,resolved_by_name,coalesce(resolved_at::text,'') FROM purchase_receipt_exceptions WHERE tenant_id=$1 AND po_id=$2 ORDER BY reported_at DESC,id DESC`, tenantID, poID)
	if err != nil {
		return out, err
	}
	for exceptions.Next() {
		var row ReceiptException
		if err = exceptions.Scan(&row.ID, &row.ReceiptID, &row.POItemID, &row.Type, &row.Qty, &row.ActualProduct, &row.ActualUOM, &row.Description, &row.Status, &row.Resolution, &row.ReportedBy, &row.ReportedAt, &row.ResolvedBy, &row.ResolvedAt); err != nil {
			exceptions.Close()
			return out, err
		}
		out.Exceptions = append(out.Exceptions, row)
	}
	exceptions.Close()
	reminders, err := s.pool.Query(ctx, `SELECT id,node,planned_date::text,buyer_id,buyer_name,related_contracts,status,created_at::text FROM purchase_production_reminders WHERE tenant_id=$1 AND po_id=$2 ORDER BY created_at DESC,id DESC`, tenantID, poID)
	if err != nil {
		return out, err
	}
	for reminders.Next() {
		var row ProductionReminder
		if err = reminders.Scan(&row.ID, &row.Node, &row.PlannedDate, &row.BuyerID, &row.BuyerName, &row.RelatedContracts, &row.Status, &row.CreatedAt); err != nil {
			reminders.Close()
			return out, err
		}
		out.Reminders = append(out.Reminders, row)
	}
	reminders.Close()
	return out, nil
}
