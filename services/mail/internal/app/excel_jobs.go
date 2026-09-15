package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/livefeed"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// ExcelJob is the durable state returned to the browser. Result is populated
// only after the worker has atomically persisted both the preview and XLSX.
type ExcelJob struct {
	ID                     int64
	Status                 string
	Result                 ExcelResult
	ErrorCode              string
	ErrorMessage           string
	CreatedAt              time.Time
	CompletedAt            time.Time
	InquiryTemplateID      int64
	InquiryTemplateCode    string
	InquiryTemplateVersion int32
}

type inquiryTemplateSnapshot struct {
	ID      int64           `json:"id"`
	Code    string          `json:"code"`
	Version int32           `json:"version"`
	Columns []InquiryColumn `json:"columns"`
}

// StartExcelJob validates ownership and the selected source before enqueueing
// work. The expensive blob read and model call happen only in the worker.
// columns 是发起时默认询盘模板的列快照，随任务持久化：之后模板改版不影响
// 已排队任务的产出。
func (s *Service) StartExcelJob(
	ctx context.Context, tenantID, ownerID, inboundID int64,
	attachmentID *int64, selectedText *string, locale string,
	columns []InquiryColumn, templateID int64, templateCode string, templateVersion int32,
) (ExcelJob, error) {
	if s.tables == nil {
		return ExcelJob{}, apierr.Invalid("MAIL_EXCEL_NOT_CONFIGURED", "Excel 智能转换尚未配置")
	}
	if err := s.validateExcelJobSource(ctx, tenantID, ownerID, inboundID, attachmentID, selectedText); err != nil {
		return ExcelJob{}, err
	}
	// 额度在这里拦：点了按钮就该当场知道能不能做，而不是排队十几秒之后
	// 才被告知本来就不该让你点。见 excel_quota.go。
	//
	// 按 ownerID 算，不按公司算——额度的单位是每人每月。
	if err := s.ensureExcelQuota(ctx, tenantID, ownerID); err != nil {
		return ExcelJob{}, err
	}
	if selectedText != nil {
		trimmed := strings.TrimSpace(*selectedText)
		selectedText = &trimmed
	}
	columnSnapshot, err := json.Marshal(inquiryTemplateSnapshot{
		ID: templateID, Code: strings.TrimSpace(templateCode), Version: templateVersion, Columns: columns,
	})
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
		if !selectionBelongsToMail(row.BodyText, row.BodyHtml, text) {
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
	snapshot, err := decodeInquiryTemplateSnapshot(row.TemplateColumns)
	if err != nil {
		return ExcelJob{}, fmt.Errorf("decode excel job template snapshot: %w", err)
	}
	job.InquiryTemplateID = snapshot.ID
	job.InquiryTemplateCode = snapshot.Code
	job.InquiryTemplateVersion = snapshot.Version
	if row.CompletedAt.Valid {
		job.CompletedAt = row.CompletedAt.Time
	}
	if row.Status == "COMPLETED" {
		// 结果已经被清掉了（见 SweepExcelJobPayloads）。**必须在这里明说**：
		// 再往下走，Data 是空的、workbook_json 是 nil，人拿到的是一个 0 字节
		// 的 .xlsx——一个打不开的文件，比一句「过期了」难查得多。
		//
		// 走到这儿的只可能是一个标签页开了两周还没关、又点了一次下载的人。
		if len(row.FileData) == 0 {
			return ExcelJob{}, apierr.Invalid("MAIL_EXCEL_RESULT_EXPIRED",
				"这次转换的结果已经清理，请重新转换一次")
		}
		job.Result = ExcelResult{FileName: row.FileName, Data: row.FileData, Model: row.Model}
		if err := json.Unmarshal(row.WorkbookJson, &job.Result.Workbook); err != nil {
			return ExcelJob{}, fmt.Errorf("decode excel job workbook: %w", err)
		}
	}
	return job, nil
}

// decodeInquiryTemplateSnapshot 同时接受新版带身份的对象和历史任务保存的
// 纯列数组，确保部署升级后队列中已有任务仍能继续执行。
func decodeInquiryTemplateSnapshot(data []byte) (inquiryTemplateSnapshot, error) {
	var snapshot inquiryTemplateSnapshot
	if err := json.Unmarshal(data, &snapshot); err == nil && snapshot.Columns != nil {
		return snapshot, nil
	}
	var legacy []InquiryColumn
	if err := json.Unmarshal(data, &legacy); err != nil {
		return inquiryTemplateSnapshot{}, err
	}
	snapshot.Columns = legacy
	return snapshot, nil
}

// 转换结果在库里留多久。
//
// 十四天：任务号只活在浏览器的 sessionStorage 里（标签页一关就没），而且
// 没有任何界面列得出历史任务——所以真正够得着这份结果的窗口，短得多。
// 十四天是给「一个标签页开了两周」留的余量，不是给「以后可能想回看」留的：
// 那件事这套东西本来就不支持。
const (
	excelPayloadRetention = 14 * 24 * time.Hour
	excelSweepEvery       = 6 * time.Hour
	excelSweepPerPass     = 500
)

// RunExcelPayloadSweeper 把交付完的转换结果从库里清掉，只留账。
//
// 为什么需要它：file_data 是这个服务唯一一处真的把文件字节写进 PostgreSQL 的
// 地方，而且只有失败的任务会被清（FailExcelJob）。成功的从来没人清——一份
// 只增不减的二进制，躺在一张本来只该是账本的表里。
//
// **不按租户拆。** 别处的查询都带 tenant_id，因为那些是在回答某个人的问题；
// 这一条不回答任何人的问题，它是维护。按租户拆只会让它跑 N 遍、锁 N 次，
// 而条件（早于某个时刻、还带着字节）本来就和租户无关。
func (s *Service) RunExcelPayloadSweeper(ctx context.Context) {
	s.log.Info("excel payload sweeper started",
		"keep", excelPayloadRetention, "every", excelSweepEvery)
	t := time.NewTicker(excelSweepEvery)
	defer t.Stop()
	for {
		n, err := s.q.SweepExcelJobPayloads(ctx, store.SweepExcelJobPayloadsParams{
			Cutoff:   pgtype.Timestamptz{Time: time.Now().Add(-excelPayloadRetention), Valid: true},
			RowLimit: excelSweepPerPass,
		})
		switch {
		case err != nil:
			s.log.Error("could not sweep excel job payloads", "err", err)
		case n > 0:
			s.log.Info("swept excel job payloads", "rows", n)
		}
		select {
		case <-ctx.Done():
			s.log.Info("excel payload sweeper stopped")
			return
		case <-t.C:
		}
	}
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
	snapshot, decodeErr := decodeInquiryTemplateSnapshot(row.TemplateColumns)
	columns := snapshot.Columns
	if decodeErr != nil {
		s.log.Error("decode excel job template columns", "job", row.ID, "err", decodeErr)
		columns = nil
	}
	result, err := s.ConvertInboundToExcel(
		ctx, row.TenantID, row.OwnerID, row.InboundID,
		row.AttachmentID, row.SelectedText, row.Locale, columns,
	)
	// 先记账，再管状态。成败都要记——模型答了钱就花了，重试几遍就花几遍。
	// 记账失败不该拖垮任务本身：账少记一笔比活干不成轻。
	if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
		if _, usageErr := s.q.RecordExcelJobUsage(ctx, store.RecordExcelJobUsageParams{
			ID: row.ID, InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		}); usageErr != nil {
			s.log.Error("persist Excel job usage", "job", row.ID, "err", usageErr)
		}
	}
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
