package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

var supplierQuoteHeaders = []string{"RFQ No", "Line ID", "Specification", "Quantity", "Unit", "Unit Price", "MOQ", "Lead Time", "Remark"}

type FactoryRFQWorkbook struct {
	FileName, RFQNo, SupplierName, ContactEmail, Currency, ResponseDueAt string
	Data                                                                 []byte
}

func (s *Service) GetFactoryRFQWorkbook(ctx context.Context, tenantID, id int64) (FactoryRFQWorkbook, error) {
	head, err := s.q.GetFactoryRFQDocument(ctx, store.GetFactoryRFQDocumentParams{TenantID: tenantID, ID: id})
	if err != nil {
		return FactoryRFQWorkbook{}, apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
	}
	lines, err := s.q.FactoryRFQLines(ctx, store.FactoryRFQLinesParams{TenantID: tenantID, FactoryRfqID: id})
	if err != nil {
		return FactoryRFQWorkbook{}, err
	}
	rows := make([][]string, 1, len(lines)+1)
	rows[0] = supplierQuoteHeaders
	for _, line := range lines {
		rows = append(rows, []string{head.RfqNo, strconv.FormatInt(line.SourcingLineID, 10), line.SpecSnapshot, line.Qty, line.UomCode, "", "", "", ""})
	}
	data, err := xlsx.Build("Supplier Quote", rows)
	if err != nil {
		return FactoryRFQWorkbook{}, err
	}
	name := fmt.Sprintf("%s-%s-quote.xlsx", safeFilePart(head.RfqNo), safeFilePart(head.SupplierName))
	return FactoryRFQWorkbook{FileName: name, Data: data, RFQNo: head.RfqNo, SupplierName: head.SupplierName,
		ContactEmail: head.ContactEmail, Currency: head.Currency, ResponseDueAt: head.ResponseDueAt}, nil
}

func safeFilePart(value string) string {
	value = strings.TrimSpace(value)
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == 0 {
			return '-'
		}
		return r
	}, value)
}

func (s *Service) ImportSupplierQuoteWorkbook(ctx context.Context, tenantID int64, data []byte, in NewSupplierQuote, op Operator) (store.CreateSupplierQuoteRow, error) {
	if in.FactoryRFQID == 0 {
		return store.CreateSupplierQuoteRow{}, apierr.Invalid("SC_RFQ_REQUIRED", "请选择工厂询价")
	}
	head, err := s.q.GetFactoryRFQDocument(ctx, store.GetFactoryRFQDocumentParams{TenantID: tenantID, ID: in.FactoryRFQID})
	if err != nil {
		return store.CreateSupplierQuoteRow{}, apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
	}
	expected, err := s.q.FactoryRFQLines(ctx, store.FactoryRFQLinesParams{TenantID: tenantID, FactoryRfqID: in.FactoryRFQID})
	if err != nil {
		return store.CreateSupplierQuoteRow{}, err
	}
	lines, err := parseSupplierQuoteWorkbook(data, head.RfqNo, expected)
	if err != nil {
		return store.CreateSupplierQuoteRow{}, err
	}
	in.Lines = lines
	in.Source = "EXCEL_IMPORT"
	if strings.TrimSpace(in.Currency) == "" {
		in.Currency = head.Currency
	}
	return s.CreateSupplierQuote(ctx, tenantID, in, op)
}

func parseSupplierQuoteWorkbook(data []byte, rfqNo string, expected []store.FactoryRFQLinesRow) ([]SupplierQuoteLineInput, error) {
	rows, err := xlsx.Parse(data)
	if err != nil {
		return nil, apierr.Invalid("SC_QUOTE_FILE_INVALID", "报价文件不是有效的 XLSX")
	}
	if len(rows) < 2 || len(rows[0]) < len(supplierQuoteHeaders) {
		return nil, apierr.Invalid("SC_QUOTE_TEMPLATE_INVALID", "请使用系统下载的供应商报价模板")
	}
	for i, header := range supplierQuoteHeaders {
		if !strings.EqualFold(strings.TrimSpace(rows[0][i]), header) {
			return nil, apierr.Invalid("SC_QUOTE_TEMPLATE_INVALID", "报价模板列名或顺序已被修改")
		}
	}
	wanted := make(map[int64]store.FactoryRFQLinesRow, len(expected))
	for _, line := range expected {
		wanted[line.SourcingLineID] = line
	}
	result := make([]SupplierQuoteLineInput, 0, len(expected))
	seen := make(map[int64]bool, len(expected))
	for _, row := range rows[1:] {
		for len(row) < len(supplierQuoteHeaders) {
			row = append(row, "")
		}
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		lineID, convErr := strconv.ParseInt(strings.TrimSpace(row[1]), 10, 64)
		expect, ok := wanted[lineID]
		if row[0] != rfqNo || convErr != nil || !ok || seen[lineID] {
			return nil, apierr.Invalid("SC_QUOTE_LINE_INVALID", "报价明细与工厂询价不一致")
		}
		if row[3] != expect.Qty || row[4] != expect.UomCode || strings.TrimSpace(row[5]) == "" {
			return nil, apierr.Invalid("SC_QUOTE_TEMPLATE_VALUES_INVALID", "请勿修改数量和单位，并填写全部单价")
		}
		seen[lineID] = true
		result = append(result, SupplierQuoteLineInput{SourcingLineID: lineID, Qty: expect.Qty, UnitPrice: row[5], MOQ: row[6], LeadTime: row[7], Remark: row[8]})
	}
	if len(result) != len(expected) {
		return nil, apierr.Invalid("SC_QUOTE_INCOMPLETE", "请填写全部询价明细的报价")
	}
	return result, nil
}

type NewFactoryRFQ struct {
	CaseID, SupplierID, FactoryID         int64
	FactoryCode, FactoryName              string
	ContactEmail, Currency, ResponseDueAt string
	SourcingLineIDs                       []int64
}

type SupplierQuoteLineInput struct {
	SourcingLineID                        int64
	Qty, UnitPrice, MOQ, LeadTime, Remark string
}

type NewSupplierQuote struct {
	FactoryRFQID                                                           int64
	QuotedAt, ValidUntil, Currency, PaymentTerms, Delivery, Remark, Source string
	Lines                                                                  []SupplierQuoteLineInput
}

func (s *Service) CreateFactoryRFQ(ctx context.Context, tenantID int64, in NewFactoryRFQ, op Operator) (store.ListFactoryRFQsRow, error) {
	if in.CaseID == 0 || in.SupplierID == 0 || in.FactoryID == 0 {
		return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_REQUIRED", "请选择询价案件、供应商和具体合作工厂")
	}
	if _, err := s.GetSourcingCase(ctx, tenantID, in.CaseID); err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	supplier, err := s.supplierForOrder(ctx, in.SupplierID)
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	if in.Currency == "" {
		in.Currency = supplier.Currency
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	caseLines, err := s.q.ListSourcingLines(ctx, store.ListSourcingLinesParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	allowed := make(map[int64]bool, len(caseLines))
	for _, line := range caseLines {
		allowed[line.ID] = line.Decision == "CONFIRMED"
	}
	if len(in.SourcingLineIDs) == 0 {
		for _, line := range caseLines {
			in.SourcingLineIDs = append(in.SourcingLineIDs, line.ID)
		}
	}
	for _, id := range in.SourcingLineIDs {
		if !allowed[id] {
			return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_LINE_UNCONFIRMED", "请先人工确认全部询价明细")
		}
	}
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.CreateFactoryRFQ(ctx, store.CreateFactoryRFQParams{TenantID: tenantID, CaseID: in.CaseID,
			SupplierID: supplier.ID, SupplierCode: supplier.Code, SupplierName: supplier.Name,
			FactoryID: in.FactoryID, FactoryCode: strings.TrimSpace(in.FactoryCode), FactoryName: strings.TrimSpace(in.FactoryName),
			ContactEmail: strings.TrimSpace(in.ContactEmail), Currency: strings.ToUpper(in.Currency),
			ResponseDueAt: in.ResponseDueAt, CreatedBy: op.ID, CreatedByName: op.Name})
		if err != nil {
			return err
		}
		id = head.ID
		for _, lineID := range in.SourcingLineIDs {
			if err := q.CreateFactoryRFQLine(ctx, store.CreateFactoryRFQLineParams{TenantID: tenantID, FactoryRfqID: id, CaseID: in.CaseID, SourcingLineID: lineID}); err != nil {
				return err
			}
		}
		if err := q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID,
			Section: "RFQ", Action: "RFQ_CREATED", EntityID: id, Summary: "向工厂 " + in.FactoryName + " 创建询价",
			BeforeJson: []byte("{}"), AfterJson: []byte(`{"status":"DRAFT"}`), OperatorID: op.ID, OperatorName: op.Name}); err != nil {
			return err
		}
		return q.MarkSourcingCaseSourcing(ctx, store.MarkSourcingCaseSourcingParams{TenantID: tenantID, ID: in.CaseID})
	})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	rows, err := s.ListFactoryRFQs(ctx, tenantID, in.CaseID)
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return store.ListFactoryRFQsRow{}, apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
}

// UpdateFactoryRFQ 修改对外询价的联系人或截止日期，并强制留下业务原因。
func (s *Service) UpdateFactoryRFQ(ctx context.Context, tenantID, id int64, contactEmail, dueAt, reason string, op Operator) (store.ListFactoryRFQsRow, error) {
	if strings.TrimSpace(reason) == "" {
		return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_CHANGE_REASON_REQUIRED", "修改 RFQ 联系人或截止日期时必须填写原因")
	}
	caseID, err := s.q.FactoryRFQCase(ctx, store.FactoryRFQCaseParams{TenantID: tenantID, ID: id})
	if err != nil {
		return store.ListFactoryRFQsRow{}, apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
	}
	rows, err := s.q.ListFactoryRFQs(ctx, store.ListFactoryRFQsParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	var before store.ListFactoryRFQsRow
	for _, row := range rows {
		if row.ID == id {
			before = row
			break
		}
	}
	if before.ID == 0 {
		return store.ListFactoryRFQsRow{}, apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(map[string]string{"contactEmail": contactEmail, "responseDueAt": dueAt})
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		changed, updateErr := q.UpdateFactoryRFQ(ctx, store.UpdateFactoryRFQParams{TenantID: tenantID, ID: id, ContactEmail: strings.TrimSpace(contactEmail), ResponseDueAt: dueAt})
		if updateErr != nil {
			return updateErr
		}
		if changed == 0 {
			return apierr.Conflict("SC_RFQ_NOT_EDITABLE", "当前 RFQ 状态不能修改")
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
			Section: "RFQ", Action: "RFQ_UPDATED", EntityID: id, Summary: "修改工厂询价联系人或截止日期",
			BeforeJson: beforeJSON, AfterJson: afterJSON, Reason: strings.TrimSpace(reason), OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	rows, err = s.q.ListFactoryRFQs(ctx, store.ListFactoryRFQsParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return store.ListFactoryRFQsRow{}, apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
}

func (s *Service) ListFactoryRFQs(ctx context.Context, tenantID, caseID int64) ([]store.ListFactoryRFQsRow, error) {
	if _, err := s.GetSourcingCase(ctx, tenantID, caseID); err != nil {
		return nil, err
	}
	return s.q.ListFactoryRFQs(ctx, store.ListFactoryRFQsParams{TenantID: tenantID, CaseID: caseID})
}

func (s *Service) CreateSupplierQuote(ctx context.Context, tenantID int64, in NewSupplierQuote, op Operator) (store.CreateSupplierQuoteRow, error) {
	if in.FactoryRFQID == 0 || len(in.Lines) == 0 {
		return store.CreateSupplierQuoteRow{}, apierr.Invalid("SC_QUOTE_LINES_REQUIRED", "供应商报价明细不能为空")
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	if in.Source == "" {
		in.Source = "MANUAL"
	}
	var result store.CreateSupplierQuoteRow
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rfq, err := q.FactoryRFQForQuote(ctx, store.FactoryRFQForQuoteParams{TenantID: tenantID, ID: in.FactoryRFQID})
		if err != nil {
			return apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
		}
		rfqLines, err := q.FactoryRFQLines(ctx, store.FactoryRFQLinesParams{TenantID: tenantID, FactoryRfqID: in.FactoryRFQID})
		if err != nil {
			return err
		}
		allowed := make(map[int64]bool, len(rfqLines))
		for _, line := range rfqLines {
			allowed[line.SourcingLineID] = true
		}
		if len(in.Lines) != len(rfqLines) {
			return apierr.Invalid("SC_QUOTE_INCOMPLETE", "请填写全部询价明细的报价")
		}
		seen := map[int64]bool{}
		for _, line := range in.Lines {
			qty, qerr := decimal.NewFromString(line.Qty)
			price, perr := decimal.NewFromString(line.UnitPrice)
			if !allowed[line.SourcingLineID] || seen[line.SourcingLineID] {
				return apierr.Invalid("SC_QUOTE_LINE_INVALID", "报价明细与工厂询价不一致")
			}
			if qerr != nil || qty.LessThanOrEqual(decimal.Zero) || perr != nil || price.IsNegative() {
				return apierr.Invalid("SC_QUOTE_PRICE_INVALID", "数量必须大于 0，单价不能为负数")
			}
			seen[line.SourcingLineID] = true
		}
		result, err = q.CreateSupplierQuote(ctx, store.CreateSupplierQuoteParams{TenantID: tenantID, FactoryRfqID: in.FactoryRFQID,
			QuotedAt: in.QuotedAt, ValidUntil: in.ValidUntil, Currency: strings.ToUpper(in.Currency), PaymentTerms: in.PaymentTerms,
			Delivery: in.Delivery, Remark: in.Remark, Source: in.Source, CreatedBy: op.ID})
		if err != nil {
			return err
		}
		for _, line := range in.Lines {
			if err := q.CreateSupplierQuoteLine(ctx, store.CreateSupplierQuoteLineParams{TenantID: tenantID,
				SupplierQuoteID: result.ID, SourcingLineID: line.SourcingLineID, Qty: line.Qty, UnitPrice: line.UnitPrice,
				Moq: line.MOQ, LeadTime: line.LeadTime, Remark: line.Remark}); err != nil {
				return err
			}
		}
		if err := q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: rfq.CaseID,
			Section: "QUOTE", Action: "QUOTE_RECEIVED", EntityID: result.ID, Summary: "录入工厂报价 " + result.SupplierQuoteNo,
			BeforeJson: []byte("{}"), AfterJson: []byte(`{"source":"` + in.Source + `"}`), OperatorID: op.ID, OperatorName: op.Name}); err != nil {
			return err
		}
		if err := q.MarkFactoryRFQQuoted(ctx, store.MarkFactoryRFQQuotedParams{TenantID: tenantID, ID: in.FactoryRFQID}); err != nil {
			return err
		}
		return q.MarkSourcingCaseQuotesReceived(ctx, store.MarkSourcingCaseQuotesReceivedParams{TenantID: tenantID, ID: rfq.CaseID})
	})
	return result, err
}

func (s *Service) ListSupplierQuoteComparison(ctx context.Context, tenantID, caseID int64) ([]store.ListSupplierQuoteComparisonRow, error) {
	if _, err := s.GetSourcingCase(ctx, tenantID, caseID); err != nil {
		return nil, err
	}
	return s.q.ListSupplierQuoteComparison(ctx, store.ListSupplierQuoteComparisonParams{TenantID: tenantID, CaseID: caseID})
}

func (s *Service) MarkFactoryRFQSent(ctx context.Context, tenantID, id int64, op Operator) error {
	caseID, err := s.q.FactoryRFQCase(ctx, store.FactoryRFQCaseParams{TenantID: tenantID, ID: id})
	if err != nil {
		return apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
	}
	rows, err := s.q.ListFactoryRFQs(ctx, store.ListFactoryRFQsParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return err
	}
	beforeStatus := ""
	for _, row := range rows {
		if row.ID == id {
			beforeStatus = row.Status
			break
		}
	}
	if beforeStatus == "" {
		return apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
	}
	if beforeStatus == "CANCELLED" || beforeStatus == "CLOSED" {
		return apierr.Conflict("SC_RFQ_NOT_SENDABLE", "已关闭或已取消的 RFQ 不能重新发送")
	}
	affected, err := s.q.MarkFactoryRFQSent(ctx, store.MarkFactoryRFQSentParams{TenantID: tenantID, ID: id})
	if err != nil {
		return err
	}
	action, summary, afterStatus := "RFQ_RESENT", "重新发送工厂询价", beforeStatus
	if affected > 0 {
		action, summary, afterStatus = "RFQ_SENT", "发送工厂询价", "SENT"
	}
	beforeJSON, _ := json.Marshal(map[string]string{"status": beforeStatus})
	afterJSON, _ := json.Marshal(map[string]string{"status": afterStatus})
	return s.q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
		Section: "RFQ", Action: action, EntityID: id, Summary: summary,
		BeforeJson: beforeJSON, AfterJson: afterJSON, OperatorID: op.ID, OperatorName: op.Name})
}
