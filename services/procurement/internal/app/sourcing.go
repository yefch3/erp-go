package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

type SourcingCaseView struct {
	Head  store.GetSourcingCaseRow
	Lines []store.ListSourcingLinesRow
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

	// 询盘记录读取时的列布局：默认模板的 id/编码/版本快照随案件保存，
	// 之后模板再改版也不影响这单已有的明细。
	template, err := s.GetDefaultInquiryTemplate(ctx, tenantID)
	if err != nil {
		return SourcingCaseView{}, err
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
			SourceFileData: in.SourceFileData, InquiryTemplateID: template.Template.ID,
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
	return SourcingCaseView{Head: head, Lines: lines}, nil
}

func (s *Service) ConfirmSourcingLines(ctx context.Context, tenantID, caseID int64, ids []int64, op Operator) (SourcingCaseView, error) {
	view, err := s.GetSourcingCase(ctx, tenantID, caseID)
	if err != nil {
		return SourcingCaseView{}, err
	}
	if len(ids) == 0 {
		return SourcingCaseView{}, apierr.Invalid("SC_CONFIRM_LINES_REQUIRED", "请选择需要确认的询价明细")
	}
	// 待复核询盘只检查明细是否属于当前询盘；此阶段不强制匹配内部产品。
	// 确认后进入原有 REVIEWING 阶段，再由采购人员完成产品匹配和逐行复核。
	if view.Head.Status == "INTAKE_PENDING" {
		allowed := make(map[int64]bool, len(view.Lines))
		for _, line := range view.Lines {
			allowed[line.ID] = true
		}
		seen := map[int64]bool{}
		for _, id := range ids {
			if !allowed[id] || seen[id] {
				return SourcingCaseView{}, apierr.Invalid("SC_INTAKE_LINES_INVALID", "待复核询盘明细已变化，请刷新后重试")
			}
			seen[id] = true
		}
		if len(seen) == 0 {
			return SourcingCaseView{}, apierr.Invalid("SC_CONFIRM_LINES_REQUIRED", "至少保留一条有效询盘明细")
		}
		err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			result, execErr := tx.Exec(ctx,
				"UPDATE sourcing_cases SET status='REVIEWING', updated_at=now() WHERE tenant_id=$1 AND id=$2 AND status='INTAKE_PENDING'",
				tenantID, caseID)
			if execErr != nil {
				return execErr
			}
			if result.RowsAffected() != 1 {
				return apierr.Conflict("SC_INTAKE_CHANGED", "待复核询盘已被处理，请刷新后重试")
			}
			return s.q.WithTx(tx).CreateSourcingChange(ctx, store.CreateSourcingChangeParams{TenantID: tenantID, CaseID: caseID,
				Section: "CASE", Action: "INTAKE_CONFIRMED", Summary: "确认标准询盘并进入产品规范复核",
				BeforeJson: []byte(`{"status":"INTAKE_PENDING"}`), AfterJson: []byte(`{"status":"REVIEWING"}`), OperatorID: op.ID, OperatorName: op.Name})
		})
		if err != nil {
			return SourcingCaseView{}, err
		}
		return s.GetSourcingCase(ctx, tenantID, caseID)
	}
	allowed := make(map[int64]bool, len(view.Lines))
	for _, line := range view.Lines {
		allowed[line.ID] = line.ProductID > 0
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !allowed[id] || seen[id] {
			return SourcingCaseView{}, apierr.Invalid("SC_CONFIRM_PRODUCT_REQUIRED", "请先逐行匹配内部产品")
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
			CustomFields:  reviewCustomFields(previous.CustomFields, in.Extracted.CustomFields),
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
