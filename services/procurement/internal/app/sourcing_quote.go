package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

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
	// Problems are collected per row and reported together with the Excel
	// row numbers the clerk actually sees — "somewhere in your file" is not
	// an error message anyone can act on (A2 §1). The import stays
	// all-or-nothing on purpose: a quote missing lines is not a smaller
	// quote, it is no quote.
	result := make([]SupplierQuoteLineInput, 0, len(expected))
	seen := make(map[int64]bool, len(expected))
	mentioned := make(map[int64]bool, len(expected))
	var problems []string
	for i, row := range rows[1:] {
		excelRow := strconv.Itoa(i + 2) // header is Excel row 1
		for len(row) < len(supplierQuoteHeaders) {
			row = append(row, "")
		}
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		if row[0] != rfqNo {
			problems = append(problems, "第 "+excelRow+" 行：询价单号被修改（应为 "+rfqNo+"）")
			continue
		}
		lineID, convErr := strconv.ParseInt(strings.TrimSpace(row[1]), 10, 64)
		if convErr != nil {
			problems = append(problems, "第 "+excelRow+" 行：行号不是数字，请勿修改模板的行号列")
			continue
		}
		expect, ok := wanted[lineID]
		if !ok {
			problems = append(problems, "第 "+excelRow+" 行：行号 "+strconv.FormatInt(lineID, 10)+" 不属于这份询价")
			continue
		}
		// A row that showed up with a value problem is complained about
		// above; only lines that never appeared get the "missing" line.
		mentioned[lineID] = true
		if seen[lineID] {
			problems = append(problems, "第 "+excelRow+" 行：行号 "+strconv.FormatInt(lineID, 10)+" 重复出现")
			continue
		}
		if row[3] != expect.Qty || row[4] != expect.UomCode {
			problems = append(problems, "第 "+excelRow+" 行：数量或单位被修改（应为 "+expect.Qty+" "+expect.UomCode+"）")
			continue
		}
		if strings.TrimSpace(row[5]) == "" {
			problems = append(problems, "第 "+excelRow+" 行：单价未填写")
			continue
		}
		seen[lineID] = true
		result = append(result, SupplierQuoteLineInput{SourcingLineID: lineID, Qty: expect.Qty, UnitPrice: row[5], MOQ: row[6], LeadTime: row[7], Remark: row[8]})
	}
	for _, line := range expected {
		if !mentioned[line.SourcingLineID] {
			problems = append(problems, "行号 "+strconv.FormatInt(line.SourcingLineID, 10)+"（"+line.SpecSnapshot+"）缺少报价")
		}
	}
	if len(problems) > 0 {
		return nil, apierr.Invalid("SC_QUOTE_ROWS_INVALID", strings.Join(problems, "；"))
	}
	return result, nil
}

type NewFactoryRFQ struct {
	CaseID, SupplierID, FactoryID                                       int64
	FactoryCode, FactoryName                                            string
	ContactEmail, Currency, ResponseDueAt                               string
	InquiryChannel, ContactName, ContactValue, ContactedAt, InquiryNote string
	RoundNo                                                             int32
	SourcingLineIDs                                                     []int64
}

type SupplierQuoteLineInput struct {
	SourcingLineID                        int64
	Qty, UnitPrice, MOQ, LeadTime, Remark string
}

type NewSupplierQuote struct {
	FactoryRFQID                                                           int64
	QuotedAt, ValidUntil, Currency, PaymentTerms, Delivery, Incoterm, Remark, Source string
	ConfirmationStatus, EvidenceNote                                       string
	Lines                                                                  []SupplierQuoteLineInput
}

// rfqEligibleSourcingLineIDs 返回人工复核后保留的询盘明细。
// 兼容旧数据的 PENDING 状态，但明确忽略或不匹配的行不会进入 RFQ。
func rfqEligibleSourcingLineIDs(lines []store.ListSourcingLinesRow) []int64 {
	ids := make([]int64, 0, len(lines))
	for _, line := range lines {
		if line.Decision != "SKIPPED" && line.Decision != "NO_MATCH" {
			ids = append(ids, line.ID)
		}
	}
	return ids
}

// validateFactoryRFQContact 校验对外询价必须具备可投递联系人和明确回复期限。
func validateFactoryRFQCommunication(channel, contactEmail, contactValue, currency, responseDueAt string, today time.Time) error {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return apierr.Invalid("SC_RFQ_CURRENCY_INVALID", "币种必须使用 3 位代码，例如 USD 或 CNY")
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return apierr.Invalid("SC_RFQ_CURRENCY_INVALID", "币种必须使用 3 位代码，例如 USD 或 CNY")
		}
	}
	// 采购已在系统外完成人工沟通时，可以直接从产品组录入供应商报价。
	// 这条技术上的 RFQ 链只用于保存供应商、产品和报价版本，不伪造沟通记录。
	if channel == "OTHER" && strings.TrimSpace(contactEmail) == "" && strings.TrimSpace(contactValue) == "" && strings.TrimSpace(responseDueAt) == "" {
		return nil
	}
	if channel == "SYSTEM_EMAIL" {
		contactEmail = strings.TrimSpace(contactEmail)
		address, err := mail.ParseAddress(contactEmail)
		if err != nil || !strings.EqualFold(address.Address, contactEmail) {
			return apierr.Invalid("SC_RFQ_CONTACT_INVALID", "系统邮件询价必须填写有效的联系人邮箱")
		}
	} else if strings.TrimSpace(contactValue) == "" {
		return apierr.Invalid("SC_RFQ_CONTACT_REQUIRED", "人工询价必须填写电话、账号或联系说明")
	}
	due, err := time.Parse("2006-01-02", strings.TrimSpace(responseDueAt))
	if err != nil {
		return apierr.Invalid("SC_RFQ_DUE_DATE_INVALID", "请选择有效的工厂回复期限")
	}
	startOfToday := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if due.Before(startOfToday) {
		return apierr.Invalid("SC_RFQ_DUE_DATE_PAST", "工厂回复期限不能早于今天")
	}
	return nil
}

// validateFactoryRFQContact 保留邮件询价校验入口，兼容既有测试和调用方。
func validateFactoryRFQContact(contactEmail, currency, responseDueAt string, today time.Time) error {
	return validateFactoryRFQCommunication("SYSTEM_EMAIL", contactEmail, "", currency, responseDueAt, today)
}

// validateFactoryRFQTarget 只要求询价案件和供应商存在。
// 生产工厂是可选快照：向贸易商或供应商总部询价时允许不指定工厂。
func validateFactoryRFQTarget(caseID, supplierID int64) error {
	if caseID == 0 || supplierID == 0 {
		return apierr.Invalid("SC_RFQ_REQUIRED", "请选择询价案件和供应商")
	}
	return nil
}

func (s *Service) CreateFactoryRFQ(ctx context.Context, tenantID int64, in NewFactoryRFQ, op Operator) (store.ListFactoryRFQsRow, error) {
	if err := validateFactoryRFQTarget(in.CaseID, in.SupplierID); err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	caseView, err := s.GetSourcingCase(ctx, tenantID, in.CaseID)
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	if caseView.Head.HandoffStatus != "WAITING_ACCEPTANCE" && caseView.Head.HandoffStatus != "IN_PROGRESS" {
		return store.ListFactoryRFQsRow{}, apierr.Conflict("SC_PARTICIPATION_NOT_OPEN", "当前案件尚未开放采购询价")
	}
	if err := s.requireSourcingParticipant(ctx, tenantID, in.CaseID, op.ID); err != nil {
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
	if in.InquiryChannel == "" {
		in.InquiryChannel = "SYSTEM_EMAIL"
	}
	if in.RoundNo <= 0 {
		in.RoundNo = 1
	}
	if err := validateFactoryRFQCommunication(in.InquiryChannel, in.ContactEmail, in.ContactValue, in.Currency, in.ResponseDueAt, time.Now().UTC()); err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	caseLines, err := s.q.ListSourcingLines(ctx, store.ListSourcingLinesParams{TenantID: tenantID, CaseID: in.CaseID})
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	allowed := make(map[int64]bool, len(caseLines))
	for _, line := range caseLines {
		allowed[line.ID] = line.Decision != "SKIPPED" && line.Decision != "NO_MATCH"
	}
	if len(in.SourcingLineIDs) == 0 {
		in.SourcingLineIDs = rfqEligibleSourcingLineIDs(caseLines)
	}
	if len(in.SourcingLineIDs) == 0 {
		return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_LINES_REQUIRED", "询盘没有可用于工厂询价的产品明细")
	}
	requiredFields, err := s.sourcingRequiredFields(ctx, tenantID, caseView.Head.InquiryTemplateID)
	if err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	for _, id := range in.SourcingLineIDs {
		if !allowed[id] {
			return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_LINE_UNAVAILABLE", "所选产品明细已被忽略，不能进入工厂询价")
		}
		for _, line := range caseLines {
			if line.ID != id {
				continue
			}
			if missing := missingIntakeReviewFields(line, requiredFields); len(missing) > 0 {
				return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_FIELDS_REQUIRED", fmt.Sprintf("第 %d 行询盘资料不完整：%s", line.LineNo, strings.Join(missing, "、")))
			}
			break
		}
	}
	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.CreateFactoryRFQ(ctx, store.CreateFactoryRFQParams{TenantID: tenantID, CaseID: in.CaseID,
			SupplierID: supplier.ID, SupplierCode: supplier.Code, SupplierName: supplier.Name,
			FactoryID: in.FactoryID, FactoryCode: strings.TrimSpace(in.FactoryCode), FactoryName: strings.TrimSpace(in.FactoryName),
			ContactEmail: strings.TrimSpace(in.ContactEmail), Currency: strings.ToUpper(in.Currency),
			ResponseDueAt: in.ResponseDueAt, CreatedBy: op.ID, CreatedByName: op.Name,
			InquiryChannel: in.InquiryChannel, ContactName: strings.TrimSpace(in.ContactName), ContactValue: strings.TrimSpace(in.ContactValue),
			ContactedAt: in.ContactedAt, InquiryNote: strings.TrimSpace(in.InquiryNote), RoundNo: in.RoundNo})
		if err != nil {
			return err
		}
		id = head.ID
		for _, lineID := range in.SourcingLineIDs {
			if err := q.CreateFactoryRFQLine(ctx, store.CreateFactoryRFQLineParams{TenantID: tenantID, FactoryRfqID: id, CaseID: in.CaseID, SourcingLineID: lineID}); err != nil {
				return err
			}
		}
		targetName := strings.TrimSpace(in.FactoryName)
		if targetName == "" {
			targetName = supplier.Name
		}
		if err := q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID,
			Section: "RFQ", Action: "RFQ_CREATED", EntityID: id, Summary: "向 " + targetName + " 创建询价",
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

// UpdateFactoryRFQ 修改对外询价的渠道、联系人和截止日期，并强制留下业务原因。
func (s *Service) UpdateFactoryRFQ(ctx context.Context, tenantID, id int64, inquiryChannel, contactName, contactEmail, contactValue, contactedAt, inquiryNote, dueAt, reason string, op Operator) (store.ListFactoryRFQsRow, error) {
	if strings.TrimSpace(reason) == "" {
		return store.ListFactoryRFQsRow{}, apierr.Invalid("SC_RFQ_CHANGE_REASON_REQUIRED", "修改询价记录时必须填写原因")
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
	if before.CreatedBy != op.ID {
		return store.ListFactoryRFQsRow{}, apierr.Permission("SC_RFQ_OWNER_REQUIRED", "只能修改自己上报的工厂询价")
	}
	if strings.TrimSpace(inquiryChannel) == "" {
		inquiryChannel = before.InquiryChannel
	}
	if err := validateFactoryRFQCommunication(inquiryChannel, contactEmail, contactValue, before.Currency, dueAt, time.Now().UTC()); err != nil {
		return store.ListFactoryRFQsRow{}, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(map[string]string{"inquiryChannel": inquiryChannel, "contactName": contactName, "contactEmail": contactEmail, "contactValue": contactValue, "contactedAt": contactedAt, "inquiryNote": inquiryNote, "responseDueAt": dueAt})
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		changed, updateErr := q.UpdateFactoryRFQ(ctx, store.UpdateFactoryRFQParams{TenantID: tenantID, ID: id,
			InquiryChannel: strings.TrimSpace(inquiryChannel), ContactName: strings.TrimSpace(contactName),
			ContactEmail: strings.TrimSpace(contactEmail), ContactValue: strings.TrimSpace(contactValue),
			ContactedAt: strings.TrimSpace(contactedAt), InquiryNote: strings.TrimSpace(inquiryNote), ResponseDueAt: dueAt})
		if updateErr != nil {
			return updateErr
		}
		if changed == 0 {
			return apierr.Conflict("SC_RFQ_NOT_EDITABLE", "当前 RFQ 状态不能修改")
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
			Section: "RFQ", Action: "RFQ_UPDATED", EntityID: id, Summary: "修改工厂询价记录",
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

// ListOverdueFactoryRFQs 是工作台的催办清单：过了回复期限还没报价的询价，
// 最逾期的排最前。围栏沿用询价案件的属主可见性——工作台只是另一个入口，
// 不是另一套权限。
func (s *Service) ListOverdueFactoryRFQs(ctx context.Context, tenantID int64, limit int32, op Operator) ([]store.ListOverdueFactoryRFQsRow, error) {
	visible, err := s.visibleSourcingTo(ctx, op)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 50 {
		limit = 10
	}
	return s.q.ListOverdueFactoryRFQs(ctx, store.ListOverdueFactoryRFQsParams{
		TenantID: tenantID, VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
		RowLimit: limit,
	})
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
	// 进入报价比较的数据均已由员工核实，统一作为正式报价。
	in.ConfirmationStatus = "WRITTEN_CONFIRMED"
	allowedSources := map[string]bool{"MANUAL": true, "EXCEL_IMPORT": true, "EMAIL_ATTACHMENT": true, "PHONE": true, "WECHAT": true, "WHATSAPP": true, "IN_PERSON": true, "OTHER": true}
	if !allowedSources[in.Source] {
		return store.CreateSupplierQuoteRow{}, apierr.Invalid("SC_QUOTE_SOURCE_INVALID", "报价来源无效")
	}
	var result store.CreateSupplierQuoteRow
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rfq, err := q.FactoryRFQForQuote(ctx, store.FactoryRFQForQuoteParams{TenantID: tenantID, ID: in.FactoryRFQID})
		if err != nil {
			return apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价不存在")
		}
		if rfq.CreatedBy != op.ID {
			return apierr.Permission("SC_RFQ_OWNER_REQUIRED", "只能为自己上报的工厂询价录入报价")
		}
		rfqLines, err := q.FactoryRFQLines(ctx, store.FactoryRFQLinesParams{TenantID: tenantID, FactoryRfqID: in.FactoryRFQID})
		if err != nil {
			return err
		}
		allowed := make(map[int64]bool, len(rfqLines))
		for _, line := range rfqLines {
			allowed[line.SourcingLineID] = true
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
			Delivery: in.Delivery, Incoterm: strings.ToUpper(strings.TrimSpace(in.Incoterm)), Remark: in.Remark, Source: in.Source, CreatedBy: op.ID,
			ConfirmationStatus: in.ConfirmationStatus, EvidenceNote: strings.TrimSpace(in.EvidenceNote)})
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

// SupplierQuoteComparisonLine is one quote line plus its price through the
// comparison lens: the same unit price expressed in the book currency, so a
// CNY quote and a USD quote for the same coil rank on one axis (A2 §4).
// Display-only and computed per read — the original currency is the record,
// the conversion is the lens, and a stored conversion would just be a
// number that stops being true when the rate moves.
type SupplierQuoteComparisonLine struct {
	Row             store.ListSupplierQuoteComparisonRow
	ComparePrice    string
	CompareCurrency string
}

func (s *Service) ListSupplierQuoteComparison(ctx context.Context, tenantID, caseID int64) ([]SupplierQuoteComparisonLine, error) {
	if _, err := s.GetSourcingCase(ctx, tenantID, caseID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListSupplierQuoteComparison(ctx, store.ListSupplierQuoteComparisonParams{TenantID: tenantID, CaseID: caseID})
	if err != nil {
		return nil, err
	}
	base := s.bookCurrency()
	// One rate per distinct currency, not per row: the fx service is a
	// network away.
	rateCache := map[string]decimal.Decimal{}
	out := make([]SupplierQuoteComparisonLine, 0, len(rows))
	for _, row := range rows {
		v := SupplierQuoteComparisonLine{Row: row}
		rate, ok := rateCache[row.Currency]
		if !ok {
			rate = s.crossRate(ctx, row.Currency)
			rateCache[row.Currency] = rate
		}
		// Rate unavailable → no lens for this row; the frontend falls back
		// to same-currency ranking rather than pretending.
		if !rate.IsZero() {
			if price, err := decimal.NewFromString(row.LUnitPrice); err == nil {
				v.ComparePrice = price.Mul(rate).Round(4).String()
				v.CompareCurrency = base
			}
		}
		out = append(out, v)
	}
	return out, nil
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
			if row.CreatedBy != op.ID {
				return apierr.Permission("SC_RFQ_OWNER_REQUIRED", "只能发送自己上报的工厂询价")
			}
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
