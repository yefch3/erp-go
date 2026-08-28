package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type SourcingLineInput struct {
	RawText, Product, MaterialStandard, Grade, Thickness, Width, LengthOrForm string
	SurfaceRequirement, Coating, Tolerance, CoilWeight, CoilID, Packaging     string
	Delivery, PaymentTerms, Incoterm, Port, QuantityUnit, Remarks, Quantity   string
	// 模板里的 custom.* 自定义列，键即字段标识。
	CustomFields map[string]string
}

func marshalCustomFields(fields map[string]string) []byte {
	if len(fields) == 0 {
		return []byte("{}")
	}
	for key, value := range fields {
		if strings.TrimSpace(value) == "" {
			delete(fields, key)
		}
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return []byte("{}")
	}
	return out
}

// 复核编辑只覆盖标准字段；自定义列未被提交时保留读取时的值。
func reviewCustomFields(previous []byte, next map[string]string) []byte {
	if next == nil {
		if len(previous) == 0 {
			return []byte("{}")
		}
		return previous
	}
	return marshalCustomFields(next)
}

type NewSourcingCase struct {
	Title, CustomerName, ContactName, ContactEmail string
	SourceFileName, SourceContentType              string
	CustomerID, SourceMailID, SourceAttachmentID   int64
	SourceFileData                                 []byte
	Lines                                          []SourcingLineInput
	InquiryTemplateID                              int64
	InquiryTemplateCode                            string
	InquiryTemplateVersion                         int32
}

type SourcingCaseView struct {
	Head  store.GetSourcingCaseRow
	Lines []store.ListSourcingLinesRow
	// Fresh presigned download for the original inquiry file; empty when the
	// case predates object storage or carries no file. Never persisted.
	SourceFileURL string
}

type SourcingFilter struct{ Status, Keyword string }

func (s *Service) CreateSourcingCase(ctx context.Context, tenantID int64, in NewSourcingCase, op Operator) (SourcingCaseView, error) {
	if len(in.Lines) == 0 {
		return SourcingCaseView{}, apierr.Invalid("SC_LINES_REQUIRED", "询价案件至少需要一条产品明细")
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = "客户询盘"
	}
	for _, line := range in.Lines {
		if line.Quantity == "" {
			continue
		}
		qty, err := decimal.NewFromString(line.Quantity)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return SourcingCaseView{}, apierr.Invalid("SC_QUANTITY_INVALID", "询价数量必须是大于 0 的数字")
		}
	}

	// 询盘必须绑定“生成或解析它的那一版模板”。邮件转换和手工上传会明确
	// 传入模板 ID；未传仅用于兼容旧调用。模板之后改版不会影响历史询盘。
	template, err := s.resolveSourcingInquiryTemplate(ctx, tenantID, in)
	if err != nil {
		return SourcingCaseView{}, err
	}

	// 原始 Excel 进对象存储，库里只留 key。上传失败则退回旧路——整份字节
	// 存 BYTEA：录入是事实，不能因为存储打盹而被拦（和汇率快照同一条原则）。
	// 先传后写库：传成功库却没写只留一个可清扫的孤儿对象，反过来则是指着
	// 空气的 key。
	sourceFileKey := ""
	if s.files != nil && len(in.SourceFileData) > 0 {
		key := sourcingObjectKey(tenantID, in.SourceFileName)
		if err := s.files.Put(ctx, key, in.SourceFileData, in.SourceContentType); err == nil {
			sourceFileKey = key
			in.SourceFileData = nil
		}
	}

	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		head, err := q.CreateSourcingCase(ctx, store.CreateSourcingCaseParams{
			TenantID: tenantID, Title: strings.TrimSpace(in.Title), CustomerID: in.CustomerID,
			CustomerName: strings.TrimSpace(in.CustomerName), ContactName: strings.TrimSpace(in.ContactName),
			ContactEmail: strings.TrimSpace(in.ContactEmail), SourceMailID: in.SourceMailID,
			SourceAttachmentID: in.SourceAttachmentID, OwnerID: op.ID, OwnerName: op.Name,
			SourceFileName: strings.TrimSpace(in.SourceFileName), SourceContentType: in.SourceContentType,
			SourceFileData: in.SourceFileData, SourceFileKey: sourceFileKey,
			InquiryTemplateID:   template.Template.ID,
			InquiryTemplateCode: template.Template.TemplateCode, InquiryTemplateVersion: template.Template.Version,
		})
		if err != nil {
			return err
		}
		id = head.ID
		for i, line := range in.Lines {
			if err := q.CreateSourcingLine(ctx, sourcingLineParams(tenantID, id, int32(i+1), line)); err != nil {
				return err
			}
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: id,
			Section: "CASE", Action: "CREATED", EntityID: id, Summary: "创建采购询价项目",
			BeforeJson: []byte("{}"), AfterJson: []byte(`{"status":"INTAKE_PENDING"}`),
			OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.ConstraintName == "sourcing_cases_mail_attachment_idx" || pgErr.ConstraintName == "sourcing_cases_manual_file_idx") {
			return SourcingCaseView{}, apierr.Conflict("SC_INTAKE_DUPLICATE", "相同来源的标准询盘已经进入待确认队列")
		}
		return SourcingCaseView{}, err
	}
	return s.GetSourcingCase(ctx, tenantID, id)
}

// resolveSourcingInquiryTemplate 解析并校验跨模块传入的模板身份，防止只传
// ID 却误配编码或版本。SUPERSEDED 版本仍可用于已由它生成的历史询盘。
func (s *Service) resolveSourcingInquiryTemplate(ctx context.Context, tenantID int64, in NewSourcingCase) (InquiryTemplateView, error) {
	if in.InquiryTemplateID <= 0 {
		return s.GetDefaultInquiryTemplate(ctx, tenantID)
	}
	template, err := s.GetInquiryTemplate(ctx, tenantID, in.InquiryTemplateID)
	if err != nil {
		return InquiryTemplateView{}, err
	}
	if code := strings.TrimSpace(in.InquiryTemplateCode); code != "" && code != template.Template.TemplateCode {
		return InquiryTemplateView{}, apierr.Conflict("SC_INQUIRY_TEMPLATE_MISMATCH", "询盘模板编码与模板版本不一致，请重新生成标准询盘")
	}
	if in.InquiryTemplateVersion > 0 && in.InquiryTemplateVersion != template.Template.Version {
		return InquiryTemplateView{}, apierr.Conflict("SC_INQUIRY_TEMPLATE_MISMATCH", "询盘模板版本与模板记录不一致，请重新生成标准询盘")
	}
	return template, nil
}

func sourcingLineParams(tenantID, caseID int64, lineNo int32, in SourcingLineInput) store.CreateSourcingLineParams {
	return store.CreateSourcingLineParams{
		TenantID: tenantID, CaseID: caseID, LineNo: lineNo, RawText: in.RawText,
		Product: in.Product, MaterialStandard: in.MaterialStandard, Grade: in.Grade,
		Thickness: in.Thickness, Width: in.Width, LengthOrForm: in.LengthOrForm,
		SurfaceRequirement: in.SurfaceRequirement, Coating: in.Coating,
		Tolerance: in.Tolerance, CoilWeight: in.CoilWeight, CoilID: in.CoilID,
		Packaging: in.Packaging, Delivery: in.Delivery, PaymentTerms: in.PaymentTerms,
		Incoterm: in.Incoterm, Port: in.Port, QuantityUnit: in.QuantityUnit,
		Remarks: in.Remarks, Quantity: in.Quantity, CustomFields: marshalCustomFields(in.CustomFields),
	}
}

func (s *Service) ListSourcingCases(ctx context.Context, tenantID int64, f SourcingFilter, page, size int32, op Operator) ([]store.ListSourcingCasesRow, int64, error) {
	page, size = normalizePage(page, size)
	visible, err := s.visibleSourcingTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListSourcingCases(ctx, store.ListSourcingCasesParams{
		TenantID: tenantID, Status: f.Status, Keyword: f.Keyword,
		VisibleAll: visible.All, VisibleIds: visible.EmployeeIDs,
		RowOffset: (page - 1) * size, RowLimit: size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

func (s *Service) GetSourcingCase(ctx context.Context, tenantID, id int64) (SourcingCaseView, error) {
	head, err := s.q.GetSourcingCase(ctx, store.GetSourcingCaseParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return SourcingCaseView{}, apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	if err != nil {
		return SourcingCaseView{}, err
	}
	lines, err := s.q.ListSourcingLines(ctx, store.ListSourcingLinesParams{TenantID: tenantID, CaseID: id})
	if err != nil {
		return SourcingCaseView{}, err
	}
	view := SourcingCaseView{Head: head, Lines: lines}
	if head.SourceFileKey != "" && s.files != nil {
		// A failed presign degrades to "named but not downloadable", which
		// beats failing the whole detail.
		if url, err := s.files.PresignGet(ctx, head.SourceFileKey); err == nil {
			view.SourceFileURL = url
		}
	}
	return view, nil
}

// sourcingObjectKey namespaces the original inquiry file by tenant. The case
// number does not exist yet when the upload happens (the INSERT mints it),
// so a random prefix carries the uniqueness instead.
func sourcingObjectKey(tenantID int64, fileName string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	if base == "" || base == "." {
		base = "inquiry.xlsx"
	}
	return fmt.Sprintf("sourcing-cases/%d/%s-%s", tenantID, hex.EncodeToString(buf), base)
}

// AddSourcingLine 在人工复核阶段补录一条产品需求，并将操作写入询盘变更记录。
// 只有尚未进入询价的待复核询盘可以增加明细，避免改变已经用于报价的依据。
func (s *Service) AddSourcingLine(ctx context.Context, tenantID, caseID int64, in SourcingLineInput, op Operator) (SourcingCaseView, error) {
	view, err := s.GetSourcingCase(ctx, tenantID, caseID)
	if err != nil {
		return SourcingCaseView{}, err
	}
	if view.Head.Status != "INTAKE_PENDING" {
		return SourcingCaseView{}, apierr.Conflict("SC_INTAKE_ALREADY_CONFIRMED", "询盘已进入询价，不能继续新增产品明细")
	}
	if strings.TrimSpace(in.Product) == "" {
		return SourcingCaseView{}, apierr.Invalid("SC_PRODUCT_REQUIRED", "请填写产品名称")
	}
	if strings.TrimSpace(in.Quantity) != "" {
		qty, parseErr := decimal.NewFromString(in.Quantity)
		if parseErr != nil || qty.LessThanOrEqual(decimal.Zero) {
			return SourcingCaseView{}, apierr.Invalid("SC_QUANTITY_INVALID", "询价数量必须是大于 0 的数字")
		}
	}

	afterJSON, _ := json.Marshal(in)
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var status string
		if lockErr := tx.QueryRow(ctx, "SELECT status FROM sourcing_cases WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenantID, caseID).Scan(&status); lockErr != nil {
			return lockErr
		}
		if status != "INTAKE_PENDING" {
			return apierr.Conflict("SC_INTAKE_ALREADY_CONFIRMED", "询盘已进入询价，不能继续新增产品明细")
		}
		var nextLineNo int32
		if numberErr := tx.QueryRow(ctx, "SELECT coalesce(max(line_no), 0) + 1 FROM sourcing_lines WHERE tenant_id=$1 AND case_id=$2", tenantID, caseID).Scan(&nextLineNo); numberErr != nil {
			return numberErr
		}
		if createErr := q.CreateSourcingLine(ctx, sourcingLineParams(tenantID, caseID, nextLineNo, in)); createErr != nil {
			return createErr
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
			Section: "PRODUCT", Action: "LINE_ADDED", Summary: "人工新增产品明细第 " + fmt.Sprint(nextLineNo) + " 行",
			BeforeJson: []byte("{}"), AfterJson: afterJSON, OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return SourcingCaseView{}, err
	}
	return s.GetSourcingCase(ctx, tenantID, caseID)
}

func (s *Service) ConfirmSourcingLines(ctx context.Context, tenantID, caseID int64, ids []int64, reason string, op Operator) (SourcingCaseView, error) {
	view, err := s.GetSourcingCase(ctx, tenantID, caseID)
	if err != nil {
		return SourcingCaseView{}, err
	}
	if len(ids) == 0 {
		return SourcingCaseView{}, apierr.Invalid("SC_CONFIRM_LINES_REQUIRED", "请选择需要确认的询价明细")
	}
	reason = strings.TrimSpace(reason)
	if view.Head.HandoffStatus == "RETURNED_FOR_SUPPLEMENT" && reason == "" {
		return SourcingCaseView{}, apierr.Invalid("SC_RESUBMIT_REASON_REQUIRED", "采购退回后再次提交必须填写本次补充说明")
	}
	// 待复核询盘已经由员工完成字段审核。转入询价时，保留行直接确认，
	// 其余行记录为暂不采购，询价项目不再重复要求匹配内部产品。
	if view.Head.Status == "INTAKE_PENDING" {
		requiredFields, fieldsErr := s.sourcingRequiredFields(ctx, tenantID, view.Head.InquiryTemplateID)
		if fieldsErr != nil {
			return SourcingCaseView{}, fieldsErr
		}
		allowed := make(map[int64]store.ListSourcingLinesRow, len(view.Lines))
		for _, line := range view.Lines {
			allowed[line.ID] = line
		}
		seen := map[int64]bool{}
		incompleteLines := make([]string, 0)
		for _, id := range ids {
			line, exists := allowed[id]
			if !exists || seen[id] {
				return SourcingCaseView{}, apierr.Invalid("SC_INTAKE_LINES_INVALID", "待复核询盘明细已变化，请刷新后重试")
			}
			seen[id] = true
			if missing := missingIntakeReviewFields(line, requiredFields); len(missing) > 0 {
				incompleteLines = append(incompleteLines, fmt.Sprintf("第 %d 行：%s", line.LineNo, strings.Join(missing, "、")))
			}
		}
		if len(seen) == 0 {
			return SourcingCaseView{}, apierr.Invalid("SC_CONFIRM_LINES_REQUIRED", "至少保留一条有效询盘明细")
		}
		if len(incompleteLines) > 0 {
			return SourcingCaseView{}, apierr.Invalid("SC_INTAKE_FIELDS_REQUIRED", "询盘资料不完整；"+strings.Join(incompleteLines, "；")+"需要填写")
		}
		err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			result, execErr := tx.Exec(ctx,
				`UPDATE sourcing_cases SET status='REVIEWING', handoff_status='WAITING_ACCEPTANCE',
 requirement_version_no=CASE WHEN handoff_status='RETURNED_FOR_SUPPLEMENT' THEN requirement_version_no+1 ELSE greatest(requirement_version_no,1) END,
 return_reason='', return_fields='{}',
 updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='INTAKE_PENDING'`,
				tenantID, caseID)
			if execErr != nil {
				return execErr
			}
			if result.RowsAffected() != 1 {
				return apierr.Conflict("SC_INTAKE_CHANGED", "待复核询盘已被处理，请刷新后重试")
			}
			q := s.q.WithTx(tx)
			if _, execErr = tx.Exec(ctx, `UPDATE sourcing_lines
SET decision=CASE WHEN id=ANY($3::bigint[]) THEN 'CONFIRMED' ELSE 'SKIPPED' END,
    decided_by=$4, decided_by_name=$5, updated_at=now()
WHERE tenant_id=$1 AND case_id=$2`, tenantID, caseID, ids, op.ID, op.Name); execErr != nil {
				return execErr
			}
			return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
				Section: "HANDOFF", Action: "SUBMITTED_TO_PROCUREMENT", Summary: "销售提交采购寻源",
				BeforeJson: []byte(`{"status":"INTAKE_PENDING"}`), AfterJson: []byte(`{"status":"REVIEWING","handoffStatus":"WAITING_ACCEPTANCE"}`),
				Reason: reason, OperatorID: op.ID, OperatorName: op.Name})
		})
		if err != nil {
			return SourcingCaseView{}, err
		}
		return s.GetSourcingCase(ctx, tenantID, caseID)
	}
	requiredFields, err := s.sourcingRequiredFields(ctx, tenantID, view.Head.InquiryTemplateID)
	if err != nil {
		return SourcingCaseView{}, err
	}
	allowed := make(map[int64]store.ListSourcingLinesRow, len(view.Lines))
	for _, line := range view.Lines {
		allowed[line.ID] = line
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		line, exists := allowed[id]
		if !exists || line.ProductID == 0 || line.UomID == 0 || seen[id] {
			return SourcingCaseView{}, apierr.Invalid("SC_CONFIRM_PRODUCT_REQUIRED", "请先逐行匹配内部产品")
		}
		if missing := missingIntakeReviewFields(line, requiredFields); len(missing) > 0 {
			return SourcingCaseView{}, apierr.Invalid("SC_PRODUCT_FIELDS_REQUIRED", fmt.Sprintf("第 %d 行资料不完整：%s", line.LineNo, strings.Join(missing, "、")))
		}
		seen[id] = true
	}
	idsJSON, _ := json.Marshal(ids)
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		changed, updateErr := q.ConfirmSourcingLines(ctx, store.ConfirmSourcingLinesParams{TenantID: tenantID, CaseID: caseID, Ids: ids})
		if updateErr != nil {
			return updateErr
		}
		if changed != int64(len(ids)) {
			return apierr.Conflict("SC_CONFIRM_CHANGED", "询价明细已变化，请刷新后重试")
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
			Section: "PRODUCT", Action: "LINES_CONFIRMED", Summary: fmt.Sprintf("批量确认 %d 条产品规范", len(ids)),
			BeforeJson: []byte("{}"), AfterJson: idsJSON, OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return SourcingCaseView{}, err
	}
	return s.GetSourcingCase(ctx, tenantID, caseID)
}

func missingIntakeReviewFields(line store.ListSourcingLinesRow, templateFields []store.ListInquiryTemplateFieldsRow) []string {
	customFields := map[string]string{}
	_ = json.Unmarshal(line.CustomFields, &customFields)
	missing := make([]string, 0)
	for _, field := range templateFields {
		if !field.IsRequired || field.FieldKey == "unit_price" || field.FieldKey == "total_price" {
			continue
		}
		if strings.TrimSpace(intakeReviewFieldValue(line, customFields, field.FieldKey)) == "" {
			name := strings.TrimSpace(field.DisplayName)
			if name == "" {
				name = field.FieldKey
			}
			missing = append(missing, name)
		}
	}
	return missing
}

func defaultSourcingRequiredFields() []store.ListInquiryTemplateFieldsRow {
	return []store.ListInquiryTemplateFieldsRow{
		{FieldKey: "product", DisplayName: "产品", IsRequired: true},
		{FieldKey: "quantity", DisplayName: "数量", IsRequired: true},
		{FieldKey: "quantity_unit", DisplayName: "单位", IsRequired: true},
	}
}

// sourcingRequiredFields 返回询盘锁定模板中的字段规则；旧数据没有模板时使用兼容必填项。
func (s *Service) sourcingRequiredFields(ctx context.Context, tenantID, templateID int64) ([]store.ListInquiryTemplateFieldsRow, error) {
	if templateID == 0 {
		return defaultSourcingRequiredFields(), nil
	}
	template, err := s.GetInquiryTemplate(ctx, tenantID, templateID)
	if err != nil {
		return nil, err
	}
	return template.Fields, nil
}

func sourcingInputAsRow(in SourcingLineInput) store.ListSourcingLinesRow {
	return store.ListSourcingLinesRow{
		Product: in.Product, MaterialStandard: in.MaterialStandard, Grade: in.Grade,
		Thickness: in.Thickness, Width: in.Width, LengthOrForm: in.LengthOrForm,
		SurfaceRequirement: in.SurfaceRequirement, Coating: in.Coating, Tolerance: in.Tolerance,
		CoilWeight: in.CoilWeight, CoilID: in.CoilID, Packaging: in.Packaging,
		Delivery: in.Delivery, PaymentTerms: in.PaymentTerms, Incoterm: in.Incoterm,
		Port: in.Port, QuantityUnit: in.QuantityUnit, Remarks: in.Remarks, Quantity: in.Quantity,
		CustomFields: marshalCustomFields(in.CustomFields),
	}
}

// intakeReviewFieldValue 将模板字段读取统一映射到询盘行，确保不同客户的
// 自定义模板和系统已知字段使用同一套必填校验。
func intakeReviewFieldValue(line store.ListSourcingLinesRow, customFields map[string]string, key string) string {
	switch key {
	case "product":
		return line.Product
	case "material_standard":
		return line.MaterialStandard
	case "grade":
		return line.Grade
	case "thickness":
		return line.Thickness
	case "width":
		return line.Width
	case "length_or_form":
		return line.LengthOrForm
	case "surface_requirement":
		return line.SurfaceRequirement
	case "coating":
		return line.Coating
	case "tolerance":
		return line.Tolerance
	case "coil_weight":
		return line.CoilWeight
	case "coil_id":
		return line.CoilID
	case "packaging":
		return line.Packaging
	case "delivery":
		return line.Delivery
	case "payment_terms":
		return line.PaymentTerms
	case "incoterm":
		return line.Incoterm
	case "port":
		return line.Port
	case "quantity_unit":
		return line.QuantityUnit
	case "remarks":
		return line.Remarks
	case "quantity":
		return line.Quantity
	default:
		return customFields[key]
	}
}

type SourcingLineReview struct {
	CaseID, LineID, ProductID, SkuID, UomID int64
	Decision                                string
	Reason                                  string
	Extracted                               SourcingLineInput
}

func (s *Service) ReviewSourcingLine(ctx context.Context, tenantID int64, in SourcingLineReview, op Operator) (SourcingCaseView, error) {
	if in.Decision != "CONFIRMED" && in.Decision != "NO_MATCH" && in.Decision != "SKIPPED" && in.Decision != "PENDING" {
		return SourcingCaseView{}, apierr.Invalid("SC_REVIEW_DECISION_INVALID", "复核结果不合法")
	}
	if in.Decision == "CONFIRMED" && (in.ProductID == 0 || in.UomID == 0) {
		return SourcingCaseView{}, apierr.Invalid("SC_CONFIRM_PRODUCT_REQUIRED", "确认明细前必须匹配内部产品和单位")
	}
	if in.Extracted.Quantity != "" {
		qty, err := decimal.NewFromString(in.Extracted.Quantity)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return SourcingCaseView{}, apierr.Invalid("SC_QUANTITY_INVALID", "询价数量必须是大于 0 的数字")
		}
	}
	view, err := s.GetSourcingCase(ctx, tenantID, in.CaseID)
	if err != nil {
		return SourcingCaseView{}, err
	}
	if in.Decision == "CONFIRMED" {
		requiredFields, fieldsErr := s.sourcingRequiredFields(ctx, tenantID, view.Head.InquiryTemplateID)
		if fieldsErr != nil {
			return SourcingCaseView{}, fieldsErr
		}
		if missing := missingIntakeReviewFields(sourcingInputAsRow(in.Extracted), requiredFields); len(missing) > 0 {
			return SourcingCaseView{}, apierr.Invalid("SC_PRODUCT_FIELDS_REQUIRED", "产品规范资料不完整："+strings.Join(missing, "、"))
		}
	}
	var previous *store.ListSourcingLinesRow
	for i := range view.Lines {
		if view.Lines[i].ID == in.LineID {
			previous = &view.Lines[i]
			break
		}
	}
	if previous == nil {
		return SourcingCaseView{}, apierr.NotFound("SC_LINE_NOT_FOUND", "询价明细不存在")
	}
	if previous.Decision == "CONFIRMED" && strings.TrimSpace(in.Reason) == "" {
		return SourcingCaseView{}, apierr.Invalid("SC_REVISION_REASON_REQUIRED", "已确认的产品规范再次修改时必须填写原因")
	}
	if in.Decision != "CONFIRMED" {
		in.ProductID, in.SkuID, in.UomID = 0, 0, 0
	}
	beforeJSON, _ := json.Marshal(previous)
	afterJSON, _ := json.Marshal(in)
	action := "LINE_REVIEWED"
	if previous.Decision == "CONFIRMED" {
		action = "LINE_REVISED"
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		changed, updateErr := q.ReviewSourcingLine(ctx, store.ReviewSourcingLineParams{
			TenantID: tenantID, CaseID: in.CaseID, ID: in.LineID, Product: in.Extracted.Product,
			MaterialStandard: in.Extracted.MaterialStandard, Grade: in.Extracted.Grade, Thickness: in.Extracted.Thickness,
			Width: in.Extracted.Width, LengthOrForm: in.Extracted.LengthOrForm, SurfaceRequirement: in.Extracted.SurfaceRequirement,
			Coating: in.Extracted.Coating, Tolerance: in.Extracted.Tolerance, CoilWeight: in.Extracted.CoilWeight,
			CoilID: in.Extracted.CoilID, Packaging: in.Extracted.Packaging, Delivery: in.Extracted.Delivery,
			PaymentTerms: in.Extracted.PaymentTerms, Incoterm: in.Extracted.Incoterm, Port: in.Extracted.Port,
			QuantityUnit: in.Extracted.QuantityUnit, Remarks: in.Extracted.Remarks, Quantity: in.Extracted.Quantity,
			// 复核编辑的是标准字段；自定义列未被提交时保留读取时的值。
			CustomFields: reviewCustomFields(previous.CustomFields, in.Extracted.CustomFields),
			ProductID:    in.ProductID, SkuID: in.SkuID, UomID: in.UomID, Decision: in.Decision,
			DecidedBy: op.ID, DecidedByName: op.Name,
		})
		if updateErr != nil {
			return updateErr
		}
		if changed != 1 {
			return apierr.NotFound("SC_LINE_NOT_FOUND", "询价明细不存在")
		}
		return q.CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: in.CaseID,
			Section: "PRODUCT", Action: action, EntityID: in.LineID,
			Summary: "复核产品规范第 " + fmt.Sprint(previous.LineNo) + " 行", BeforeJson: beforeJSON, AfterJson: afterJSON,
			Reason: strings.TrimSpace(in.Reason), OperatorID: op.ID, OperatorName: op.Name})
	})
	if err != nil {
		return SourcingCaseView{}, err
	}
	return s.GetSourcingCase(ctx, tenantID, in.CaseID)
}

func (s *Service) ListSourcingChanges(ctx context.Context, tenantID, caseID int64) ([]store.ListSourcingChangesRow, error) {
	if _, err := s.GetSourcingCase(ctx, tenantID, caseID); err != nil {
		return nil, err
	}
	return s.q.ListSourcingChanges(ctx, store.ListSourcingChangesParams{TenantID: tenantID, CaseID: caseID})
}
