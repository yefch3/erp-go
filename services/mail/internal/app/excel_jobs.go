package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// ExcelJob is the durable state returned to the browser. Result is populated
// only after the worker has atomically persisted both the preview and XLSX.
type ExcelJob struct {
	ID           int64
	Status       string
	Result       ExcelResult
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
	CompletedAt  time.Time
}

// StartExcelJob validates ownership and the selected source before enqueueing
// work. The expensive blob read and model call happen only in the worker.
// columns 是发起时默认询盘模板的列快照，随任务持久化：之后模板改版不影响
// 已排队任务的产出。
func (s *Service) StartExcelJob(
	ctx context.Context, tenantID, ownerID, inboundID int64,
	attachmentID *int64, selectedText *string, locale string,
	columns []InquiryColumn,
) (ExcelJob, error) {
	if s.tables == nil {
		return ExcelJob{}, apierr.Invalid("MAIL_EXCEL_NOT_CONFIGURED", "Excel 智能转换尚未配置")
	}
	if err := s.validateExcelJobSource(ctx, tenantID, ownerID, inboundID, attachmentID, selectedText); err != nil {
		return ExcelJob{}, err
	}
	if selectedText != nil {
		trimmed := strings.TrimSpace(*selectedText)
		selectedText = &trimmed
	}
	columnSnapshot, err := json.Marshal(columns)
	if err != nil {
		return ExcelJob{}, err
	}
	row, err := s.q.CreateExcelJob(ctx, store.CreateExcelJobParams{
		TenantID: tenantID, OwnerID: ownerID, InboundID: inboundID,
		AttachmentID: attachmentID, SelectedText: selectedText,
		Locale: normalizeExcelLocale(locale), TemplateColumns: columnSnapshot,
	})
	if err != nil {
		return ExcelJob{}, err
	}
	return excelJobFromRow(row)
}

func (s *Service) validateExcelJobSource(
	ctx context.Context, tenantID, ownerID, inboundID int64,
	attachmentID *int64, selectedText *string,
) error {
	if inboundID <= 0 {
		return apierr.Invalid("MAIL_EXCEL_MAIL_REQUIRED", "缺少邮件标识")
	}
	if (attachmentID == nil) == (selectedText == nil) {
		return apierr.Invalid("MAIL_EXCEL_SOURCE_REQUIRED", "请选择一段正文或一个附件")
	}
	row, err := s.q.GetInbound(ctx, store.GetInboundParams{TenantID: tenantID, ID: inboundID})
	if err != nil || row.OwnerID != ownerID {
		return errNotFound()
	}
	if selectedText != nil {
		text := strings.TrimSpace(*selectedText)
		if text == "" {
			return apierr.Invalid("MAIL_EXCEL_TEXT_EMPTY", "选中的文字为空")
		}
		if len(text) > maxExcelSelectedText {
			return apierr.Invalid("MAIL_EXCEL_TEXT_TOO_LARGE", "选中的文字过长，请缩小选择范围")
		}
		body := row.BodyText
		if strings.TrimSpace(body) == "" {
			body = HTMLToText(row.BodyHtml)
		}
		if !containsNormalizedText(body, text) {
			return apierr.Invalid("MAIL_EXCEL_TEXT_NOT_IN_MAIL", "选中的文字不属于这封邮件")
		}
		return nil
	}
	atts, err := s.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: inboundID,
	})
	if err != nil {
		return err
	}
	for _, attachment := range atts {
		if attachment.ID != *attachmentID {
			continue
		}
		if attachment.FileKey == "" {
			return apierr.NotFound("MAIL_EXCEL_ATTACHMENT_NOT_FOUND", "附件不存在或未保存")
		}
		if !supportedTableSource(attachment.FileName, attachment.ContentType) {
			return apierr.Invalid("MAIL_EXCEL_FILE_TYPE", "这个附件类型暂不支持转换为 Excel")
		}
		return nil
	}
	return apierr.NotFound("MAIL_EXCEL_ATTACHMENT_NOT_FOUND", "附件不存在或未保存")
}

func (s *Service) GetExcelJob(ctx context.Context, tenantID, ownerID, id int64) (ExcelJob, error) {
	row, err := s.q.GetExcelJob(ctx, store.GetExcelJobParams{TenantID: tenantID, OwnerID: ownerID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ExcelJob{}, errNotFound()
	}
	if err != nil {
		return ExcelJob{}, err
	}
	return excelJobFromRow(row)
}

func excelJobFromRow(row store.MailExcelJob) (ExcelJob, error) {
	job := ExcelJob{
		ID: row.ID, Status: row.Status, ErrorCode: row.ErrorCode,
		ErrorMessage: row.ErrorMessage, CreatedAt: row.CreatedAt.Time,
	}
	if row.CompletedAt.Valid {
		job.CompletedAt = row.CompletedAt.Time
	}
	if row.Status == "COMPLETED" {
		job.Result = ExcelResult{FileName: row.FileName, Data: row.FileData, Model: row.Model}
		if err := json.Unmarshal(row.WorkbookJson, &job.Result.Workbook); err != nil {
			return ExcelJob{}, fmt.Errorf("decode excel job workbook: %w", err)
		}
	}
	return job, nil
}

// RunExcelWorker drains the durable extraction queue. SKIP LOCKED in the
// claim query makes multiple mail replicas safe, and abandoned PROCESSING
// rows become claimable again after the decision window in that query.
func (s *Service) RunExcelWorker(ctx context.Context) {
	if s.tables == nil {
		return
	}
	s.log.Info("mail-to-Excel worker started")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.drainExcelJobs(ctx); err != nil {
			s.log.Error("mail-to-Excel worker pass failed", "err", err)
		}
		select {
		case <-ctx.Done():
			s.log.Info("mail-to-Excel worker stopped")
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) drainExcelJobs(ctx context.Context) error {
	for {
		row, err := s.q.ClaimExcelJob(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		s.processExcelJob(ctx, row)
	}
}

func (s *Service) processExcelJob(ctx context.Context, row store.MailExcelJob) {
	var columns []InquiryColumn
	if err := json.Unmarshal(row.TemplateColumns, &columns); err != nil {
		s.log.Error("decode excel job template columns", "job", row.ID, "err", err)
		columns = nil
	}
	result, err := s.ConvertInboundToExcel(
		ctx, row.TenantID, row.OwnerID, row.InboundID,
		row.AttachmentID, row.SelectedText, row.Locale, columns,
	)
	if err != nil {
		code, message := "MAIL_EXCEL_MODEL_FAILED", "智能转换失败，请稍后重试"
		var business *apierr.Error
		if errors.As(err, &business) {
			code, message = business.Code, business.Msg
		}
		if _, saveErr := s.q.FailExcelJob(ctx, store.FailExcelJobParams{
			ID: row.ID, ErrorCode: code, ErrorMessage: message,
		}); saveErr != nil {
			s.log.Error("persist failed Excel job", "job", row.ID, "err", saveErr)
			return
		}
		s.publishExcelJob(ctx, row)
		return
	}
	workbook, err := json.Marshal(result.Workbook)
	if err != nil {
		s.log.Error("encode Excel job workbook", "job", row.ID, "err", err)
		return
	}
	if _, err := s.q.CompleteExcelJob(ctx, store.CompleteExcelJobParams{
		ID: row.ID, FileName: result.FileName, FileData: result.Data,
		WorkbookJson: workbook, Model: result.Model,
	}); err != nil {
		s.log.Error("persist completed Excel job", "job", row.ID, "err", err)
		return
	}
	s.publishExcelJob(ctx, row)
}

func (s *Service) publishExcelJob(ctx context.Context, row store.MailExcelJob) {
	if s.live == nil {
		return
	}
	s.live.ToEmployees(ctx, row.TenantID, []int64{row.OwnerID}, livefeed.Event{
		Type: livefeed.MailExcelJobChanged, Subject: fmt.Sprintf("EXCEL_JOB:%d", row.ID),
	})
}
