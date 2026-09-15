package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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

// excelResultBytes 取这次转换出来的那份文件。
//
// 两个地方之一：对象存储（正常路径），或者库里那一列（对象存储当时写不进去
// 的退路，见 storeExcelResult）。两处都空，就是已经被清理器收走了。
//
// **收走之后必须明说。** 不拦的话，Data 是空的、workbook_json 是 nil，人拿到
// 一个 0 字节的 .xlsx——一个打不开的文件，比一句"过期了"难查得多。走到这儿
// 的只可能是一个标签页开了两周还没关、又点了一次下载的人。
func (s *Service) excelResultBytes(ctx context.Context, row store.MailExcelJob) ([]byte, error) {
	if row.FileKey != "" {
		if s.files == nil {
			return nil, apierr.Invalid("MAIL_EXCEL_RESULT_UNREACHABLE", "文件存储未配置，取不到这次转换的结果")
		}
		data, err := s.readCapped(ctx, row.FileKey, MaxExcelResultBytes)
		if err != nil {
			// 对象不见了（被谁清了、桶被换了）和存储暂时不通，这里分不出来，
			// 也不该在这里猜。说一句"取不到"，比给一个空文件强。
			s.log.Warn("could not read an Excel result", "key", row.FileKey, "err", err)
			return nil, apierr.Invalid("MAIL_EXCEL_RESULT_UNREACHABLE", "这次转换的结果暂时取不到，请稍后重试或重新转换")
		}
		return data, nil
	}
	if len(row.FileData) > 0 {
		return row.FileData, nil
	}
	return nil, apierr.Invalid("MAIL_EXCEL_RESULT_EXPIRED", "这次转换的结果已经清理，请重新转换一次")
}

// MaxExcelResultBytes 是从对象存储读回一份结果的上限。和送去转换的上限
// 同一个数量级：模型吐出来的表格比原件大得有限。
const MaxExcelResultBytes = 25 << 20

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
	job, err := excelJobFromRow(row)
	if err != nil {
		return ExcelJob{}, err
	}
	if job.Status == "COMPLETED" {
		// 文件本体不在这一行里：它在对象存储，要 ctx 才取得到。
		if job.Result.Data, err = s.excelResultBytes(ctx, row); err != nil {
			return ExcelJob{}, err
		}
	}
	return job, nil
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
		// 结果被清理器收走之后 workbook_json 也是 nil。**先拦在这里**，
		// 不然下面那句 Unmarshal 报的是"解不开"，而真相是"已经没有了"。
		if len(row.WorkbookJson) == 0 {
			return ExcelJob{}, apierr.Invalid("MAIL_EXCEL_RESULT_EXPIRED",
				"这次转换的结果已经清理，请重新转换一次")
		}
		// Data 不在这里填：它可能在对象存储里，要 ctx 才取得到。
		// 由调用方 GetExcelJob 补上（excelResultBytes）。
		job.Result = ExcelResult{FileName: row.FileName, Model: row.Model}
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

// RunExcelPayloadSweeper 把交付完的转换结果收走，**行留着**。
//
// 收两处：对象存储里的那一份，以及退路上留在库里的那些字节（对象存储当时
// 写不进去，见 storeExcelResult）。只收一处的话，另一处就成了新的只增不减。
//
// **谁都够不着了才收。** 任务号只活在浏览器的 sessionStorage 里（标签页一关
// 就没），而且没有任何界面列得出历史任务。过了窗口期，那份文件是任何人都
// 取不回来的。
//
// 逐行处理而不是一条 UPDATE：对象存储里的那一份得由 Go 去删，而**一个删不掉
// 不该连累一整批**——删不掉的那一行不清，下一趟再来。
//
// 不按租户拆：别处的查询都带 tenant_id，因为那些在回答某个人的问题；这一条
// 不回答任何人的问题，它是维护。
func (s *Service) RunExcelPayloadSweeper(ctx context.Context) {
	s.log.Info("excel payload sweeper started",
		"keep", excelPayloadRetention, "every", excelSweepEvery)
	t := time.NewTicker(excelSweepEvery)
	defer t.Stop()
	for {
		if n := s.sweepExcelPayloadsOnce(ctx); n > 0 {
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

// sweepExcelPayloadsOnce 收一批，返回真的收掉了几行。
func (s *Service) sweepExcelPayloadsOnce(ctx context.Context) int {
	rows, err := s.q.ListExpiredExcelPayloads(ctx, store.ListExpiredExcelPayloadsParams{
		Cutoff:   pgtype.Timestamptz{Time: time.Now().Add(-excelPayloadRetention), Valid: true},
		RowLimit: excelSweepPerPass,
	})
	if err != nil {
		s.log.Error("could not list expired excel payloads", "err", err)
		return 0
	}
	done := 0
	for _, row := range rows {
		// 先删对象、再清行。反过来的话，删对象失败就再也没有人知道那个 key
		// 是什么了——一个谁都找不着、也谁都删不掉的对象。
		if row.FileKey != "" && s.files != nil {
			if err := s.files.Remove(ctx, row.FileKey); err != nil {
				s.log.Warn("could not remove an expired excel result", "key", row.FileKey, "err", err)
				continue
			}
		}
		if _, err := s.q.ClearExcelJobPayload(ctx, row.ID); err != nil {
			s.log.Error("could not clear an expired excel payload", "job", row.ID, "err", err)
			continue
		}
		done++
	}
	return done
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
	key, inline := s.storeExcelResult(ctx, row, result)
	if _, err := s.q.CompleteExcelJob(ctx, store.CompleteExcelJobParams{
		ID: row.ID, FileName: result.FileName, FileKey: key, FileData: inline,
		WorkbookJson: workbook, Model: result.Model,
	}); err != nil {
		s.log.Error("persist completed Excel job", "job", row.ID, "err", err)
		return
	}
	s.publishExcelJob(ctx, row)
}

// 上传重试：只重试**上传那一下**，不是重试整个任务。
//
// 这条区别是这一段的全部要点。任务级的重试（ClaimExcelJob 十五分钟后重新
// 领取）会把模型那一次调用一起重跑——几十秒，而且**再花一次钱**。而此刻
// 字节已经在手里了，要解决的只是"存哪去"。
const (
	excelUploadAttempts = 3
	excelUploadBackoff  = 400 * time.Millisecond
)

// storeExcelResult 把结果放进对象存储，返回 (对象键, 退路字节)。
//
// 两个返回值永远只有一个有值：
//
//   - 传上去了 → (key, nil)
//   - 传不上去 → ("", 字节)，**存进库里当退路**
//
// 为什么不是"传不上去就让任务失败"：模型那一次调用已经花了钱、等了几十秒，
// 而手里的字节是好的。因为对象存储抖了一下就把它扔掉、让人重跑一遍，是拿
// 用户的时间和我们的钱去换一个"干净"的失败。
//
// 为什么不是"排进死信队列等人来看"：队列里能放的是消息，而这里必须活下来的
// 是**文件本身**。不带文件的死信条目救不了任何人——要拿到那份 Excel 还得重跑
// 模型；带文件的死信条目就是又把字节写回了 Postgres（failed_events.payload 是
// JSONB），正是这次要解决的事。而且那套死信是给 Kafka 事件用的：事件小、可
// 重放、"等人来看"是可接受的时延。这里人正坐在屏幕前等这份表格。
//
// 退路上的字节和对象存储里的那一份由同一个清理器按同一个窗口收走，所以它
// 不会变成新的只增不减。
func (s *Service) storeExcelResult(ctx context.Context, row store.MailExcelJob, result ExcelResult) (string, []byte) {
	if s.files == nil {
		// 没配对象存储。不是错误，是这套部署本来就没有它——照旧存库里。
		return "", result.Data
	}
	key := excelResultKey(row.TenantID, row.ID, result.FileName)
	var last error
	for attempt := 1; attempt <= excelUploadAttempts; attempt++ {
		last = s.files.Put(ctx, key, bytes.NewReader(result.Data),
			int64(len(result.Data)), excelContentType)
		if last == nil {
			return key, nil
		}
		s.log.Warn("could not upload an Excel result, retrying",
			"job", row.ID, "attempt", attempt, "err", last)
		select {
		case <-ctx.Done():
			return "", result.Data
		case <-time.After(time.Duration(attempt) * excelUploadBackoff):
		}
	}
	// 三次都没成。**大声说**：这不是正常路径，而且它意味着对象存储此刻是坏的
	// ——别的功能（附件、原件）也正在受影响，只是那些地方没有退路。
	s.log.Error("Excel result stays in the database: object storage would not take it",
		"job", row.ID, "bytes", len(result.Data), "err", last)
	return "", result.Data
}

const excelContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// 对象键带任务号：同一个人对同一封信转两次是两份结果，不该互相覆盖。
func excelResultKey(tenantID, jobID int64, fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != ".xlsx" {
		ext = ".xlsx"
	}
	return fmt.Sprintf("mail-excel/%d/%d%s", tenantID, jobID, ext)
}

func (s *Service) publishExcelJob(ctx context.Context, row store.MailExcelJob) {
	if s.live == nil {
		return
	}
	s.live.ToEmployees(ctx, row.TenantID, []int64{row.OwnerID}, livefeed.Event{
		Type: livefeed.MailExcelJobChanged, Subject: fmt.Sprintf("EXCEL_JOB:%d", row.ID),
	})
}
