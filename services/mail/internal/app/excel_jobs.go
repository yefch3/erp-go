package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/blobstore"
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
// 两个地方之一：对象存储（2026-09-15 起唯一的写入处），或者库里那一列（改动
// 前完成的旧任务的文件本身还在那里，留存期一到清空）。两处都空，就是已经被
// 清理器收走了。
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
			s.log.Warn("could not read an Excel result",
				"event", excelEventResultUnreachable, "key", row.FileKey, "err", err)
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
// 收两处：对象存储里的那两份（文件本身和 metadata），以及改动前完成的旧任务
// 留在库里的文件本身（file_data，2026-09-15 起不再写）。只收一处的话，另一处
// 就成了新的只增不减。
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
		// 先删对象、再清行。反过来的话，删对象失败就再也没有人回来收它了
		// ——那一行已经标成"收过了"，下一趟不会再列出来。
		//
		// **不看这一行的列里写着什么，一律按租户+任务号算出两个键去删。**
		// 这样这一段完全不依赖行的状态——成功的、失败的（文件传上去了但
		// metadata 没传成的那种也在内）、旧的，收的动作都一样。S3 的 DELETE
		// 对不存在的键是幂等的，没有对象的那些行，这一下什么都不会发生。
		if err := s.removeExcelObjects(ctx, row.TenantID, row.ID); err != nil {
			s.log.Warn("could not remove an expired excel result",
				"event", excelEventSweepRemoveFailed, "job", row.ID, "err", err)
			continue
		}
		if _, err := s.q.ClearExcelJobPayload(ctx, row.ID); err != nil {
			s.log.Error("could not clear an expired excel payload", "job", row.ID, "err", err)
			continue
		}
		done++
	}
	return done
}

// RunExcelWorkers 起 n 个 worker 一起从队列里领任务，等它们都停下才返回。
//
// 原来只起一个，全公司的转换排一条队：一次平均 15 秒、慢的 45 秒，十个人同时
// 点，最后一个要等几分钟。领任务那条查询是 SKIP LOCKED，几个 worker 同时领
// 不会领到同一条，所以这里只是多开几个，不用任何协调。
//
// 固定 n 个而不是来一个开一个：每个在转的任务都把附件放在内存里、占着一个
// 发给模型厂的请求，高峰时要有个上限（MAIL_EXCEL_WORKERS，默认 4）。
func (s *Service) RunExcelWorkers(ctx context.Context, n int) {
	if n < 1 {
		n = 1
	}
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.RunExcelWorker(ctx)
		}()
	}
	wg.Wait()
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

// 结果怎么落地——顺序是关键（2026-09-15 定的；之前两版都把文件本身写进
// 过库，一版还会在写库失败时重跑模型，两条都是产品负责人明确不要的）：
//
//  1. **文件本身 → 对象存储**（mail-excel/<租户>/<任务号>.xlsx）
//  2. **一小份 metadata → 对象存储**（同名 .json：文件名、模型、表的说明和
//     字段标识——就是第 3 步要写进库的那几样）
//  3. **状态 + file_key + metadata → 库**，一条 UPDATE
//
// 文件本身**从不进库**。库里只有 metadata，行数据在文件里（见 withoutRows）。
//
// 每一步失败了怎么办：
//
//   - 第 1、2 步写不进去：进程里退避重试几次（excelWriteAttempts）；还不行
//     就把任务标成 FAILED（MAIL_EXCEL_STORAGE_UNAVAILABLE），说清原因，由人
//     决定什么时候再转。文件此刻只在内存里，而它不许进库——所以没有第二个
//     地方可放；对象存储连着半分钟都写不进去，本来就是该有人看一眼的事故。
//   - 第 3 步写不进去：进程里退避重试几次；还不行就让任务留在「处理中」，
//     15 分钟后被重新领走（ClaimExcelJob）。领走时**先看对象存储里有没有第 2
//     步那份 metadata**（recoverExcelResult）：有，说明文件早就在了，直接做
//     第 3 步——**模型一次都不多跑**。进程半路被杀（部署）也走这条路。
//   - 领走时对象存储不通：什么都不做，等下一次领。这时跑模型只会白花钱——
//     跑完也存不进去。
//
// 第 2 步排在第 1 步之后，所以 metadata 在 ⇒ 文件在；恢复时只用看 metadata。
// 反过来（文件在、metadata 不在）是第 2 步失败留下的，恢复时当作没有，重跑
// 之后同一个键覆盖掉；就算任务最后失败了，清理器也按算出来的键把两份都删。
// 这几条日志各带一个固定的 event 字段——CloudWatch 上的指标过滤器按它匹配
// （deploy/aws/05-alerts.sh），不按那句话的文字。文字随便改，event 不能改：
// 改了告警就静默失效。TestAlertScriptKnowsEveryExcelEvent 钉着两边一致。
const (
	excelEventStorageUnreachable = "excel_storage_unreachable" // 领任务时对象存储不通，什么都没做
	excelEventStorageUnavailable = "excel_storage_unavailable" // 结果传不上对象存储，任务标失败
	excelEventRowWriteFailed     = "excel_row_write_failed"    // 传上去了，库那一行没写成，等恢复
	excelEventResultRecovered    = "excel_result_recovered"    // 恢复路径走了一次，模型没跑
	excelEventResultUnreachable  = "excel_result_unreachable"  // 预览/下载时从对象存储取不到
	excelEventSweepRemoveFailed  = "excel_sweep_remove_failed" // 清理器删不掉对象
	excelEventModelAccount       = "excel_model_account"       // 模型厂拒了这把 key：失效或余额用完，全公司都转不成
	excelEventModelBusy          = "excel_model_busy"          // 限流或对方出错，重试几次仍不行
)

func (s *Service) processExcelJob(ctx context.Context, row store.MailExcelJob) {
	result, recovered, err := s.recoverExcelResult(ctx, row)
	if err != nil {
		s.log.Warn("object storage unreachable; leaving the Excel job for the next claim",
			"event", excelEventStorageUnreachable, "job", row.ID, "err", err)
		return
	}
	if !recovered {
		var ok bool
		if result, ok = s.convertExcelJob(ctx, row); !ok {
			return
		}
		if err := s.putExcelResult(ctx, row, result); err != nil {
			s.log.Error("could not store an Excel result; failing the job",
				"event", excelEventStorageUnavailable, "job", row.ID, "err", err)
			s.failExcelJob(ctx, row, "MAIL_EXCEL_STORAGE_UNAVAILABLE",
				"文件存储暂时不可用，这次转换的结果没能保存，请稍后重新转换")
			return
		}
	}
	if err := s.completeExcelJob(ctx, row, result); err != nil {
		// 文件和 metadata 都已经在对象存储里了。任务留在「处理中」，下一次
		// 领走时从那里恢复，不再跑模型。
		s.log.Error("could not record a completed Excel job; it will be recovered from object storage",
			"event", excelEventRowWriteFailed, "job", row.ID, "err", err)
		return
	}
	s.publishExcelJob(ctx, row)
}

// excelModelBudget 是一个任务问模型最多花多久，包括适配器里的重试。
//
// 它必须明显短于 ClaimExcelJob 里那条「处理中超过 15 分钟就当它死了、重新领」
// 的线（db/queries/excel.sql）：超过那条线，另一个 worker 会把还在跑的任务
// 再领一遍，模型再问一次、钱再花一次。按默认配置一次最多约 7 分钟（3 次 ×
// 2 分钟超时 + 等待），本来碰不到；但 OPENAI_TIMEOUT 能调大，调到 5 分钟以上
// 三次就可能越线——所以不靠「碰巧够不着」，在这里卡死。剩下的几分钟留给
// 存对象、写库。TestTheModelBudgetStaysInsideTheReclaimWindow 钉着两边。
const excelModelBudget = 10 * time.Minute

// convertExcelJob 跑模型。返回 false 表示任务已经标成失败（或者连失败都标
// 不上，那时日志里有）。
func (s *Service) convertExcelJob(ctx context.Context, row store.MailExcelJob) (ExcelResult, bool) {
	snapshot, decodeErr := decodeInquiryTemplateSnapshot(row.TemplateColumns)
	columns := snapshot.Columns
	if decodeErr != nil {
		s.log.Error("decode excel job template columns", "job", row.ID, "err", decodeErr)
		columns = nil
	}
	// 问模型最多花这么久，包括重试。见 excelModelBudget。
	modelCtx, cancel := context.WithTimeout(ctx, excelModelBudget)
	defer cancel()
	result, err := s.ConvertInboundToExcel(
		modelCtx, row.TenantID, row.OwnerID, row.InboundID,
		row.AttachmentID, row.SelectedText, row.Locale, columns,
	)
	// 先记账，再管状态。成败都要记——模型答了钱就花了。记账失败不该拖垮
	// 任务本身：账少记一笔比活干不成轻。
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
		s.failExcelJob(ctx, row, code, message)
		return ExcelResult{}, false
	}
	return result, true
}

func (s *Service) failExcelJob(ctx context.Context, row store.MailExcelJob, code, message string) {
	if _, err := s.q.FailExcelJob(ctx, store.FailExcelJobParams{
		ID: row.ID, ErrorCode: code, ErrorMessage: message,
	}); err != nil {
		s.log.Error("persist failed Excel job", "job", row.ID, "err", err)
		return
	}
	s.publishExcelJob(ctx, row)
}

// excelResultSidecar 是和文件本身放在一起的那份 metadata——第 3 步要写进
// 库的东西，一个不多一个不少。恢复时靠它把任务收尾，不用再问模型。
type excelResultSidecar struct {
	FileName string   `json:"file_name"`
	Model    string   `json:"model"`
	Workbook Workbook `json:"workbook"`
}

// 一次写（对象存储或库）连着失败时，在进程里等多久再放弃：2、4、8、16 秒，
// 一共半分钟出头。抖一下够它缓过来；半分钟都不行的，等下去只是占着 worker。
// 是变量不是常量：测试里不等。
var (
	excelWriteAttempts  = 5
	excelWriteRetryBase = 2 * time.Second
)

// retryBriefly 把一个会失败的写操作重试几次，退避翻倍。
func retryBriefly(ctx context.Context, op func() error) error {
	var err error
	for attempt := 0; attempt < excelWriteAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(excelWriteRetryBase << (attempt - 1)):
			}
		}
		if err = op(); err == nil {
			return nil
		}
	}
	return err
}

// putExcelResult 是第 1、2 步：文件本身，然后 metadata，都进对象存储。
func (s *Service) putExcelResult(ctx context.Context, row store.MailExcelJob, result ExcelResult) error {
	if s.files == nil {
		return errors.New("object storage not configured")
	}
	sidecar, err := json.Marshal(excelResultSidecar{
		FileName: result.FileName, Model: result.Model, Workbook: result.Workbook.withoutRows(),
	})
	if err != nil {
		return fmt.Errorf("encode excel result metadata: %w", err)
	}
	fileKey := excelResultKey(row.TenantID, row.ID)
	if err := retryBriefly(ctx, func() error {
		return s.files.Put(ctx, fileKey, bytes.NewReader(result.Data), int64(len(result.Data)), excelContentType)
	}); err != nil {
		return fmt.Errorf("store excel result file: %w", err)
	}
	metaKey := excelResultMetaKey(row.TenantID, row.ID)
	if err := retryBriefly(ctx, func() error {
		return s.files.Put(ctx, metaKey, bytes.NewReader(sidecar), int64(len(sidecar)), "application/json")
	}); err != nil {
		return fmt.Errorf("store excel result metadata: %w", err)
	}
	return nil
}

// metadata 那份对象的上限。几百字节的东西，给 1 MB 已经是天花板。
const maxExcelSidecarBytes = 1 << 20

// recoverExcelResult 看对象存储里有没有这个任务上一次跑完留下的 metadata。
//
//   - 有 ⇒ 文件也在（写的顺序保证的）。返回 true，不用再跑模型。
//   - 没有 ⇒ 返回 false，正常跑。
//   - 对象存储不通 ⇒ 返回错误。分不清有没有，这时什么都不该做。
func (s *Service) recoverExcelResult(ctx context.Context, row store.MailExcelJob) (ExcelResult, bool, error) {
	if s.files == nil {
		return ExcelResult{}, false, nil
	}
	metaKey := excelResultMetaKey(row.TenantID, row.ID)
	if _, _, err := s.files.Stat(ctx, metaKey); err != nil {
		if errors.Is(err, blobstore.ErrNotFound) {
			return ExcelResult{}, false, nil
		}
		return ExcelResult{}, false, err
	}
	raw, err := s.readCapped(ctx, metaKey, maxExcelSidecarBytes)
	if err != nil {
		return ExcelResult{}, false, err
	}
	var sidecar excelResultSidecar
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		// 不是我们写的东西。当作没有；重跑之后同一个键覆盖掉。
		s.log.Warn("unreadable excel result metadata in object storage; converting again",
			"job", row.ID, "err", err)
		return ExcelResult{}, false, nil
	}
	s.log.Info("recovered an Excel result from object storage; the model is not run again",
		"event", excelEventResultRecovered, "job", row.ID)
	return ExcelResult{FileName: sidecar.FileName, Model: sidecar.Model, Workbook: sidecar.Workbook}, true, nil
}

// completeExcelJob 是第 3 步：库里写一行。只有 key 和 metadata，没有文件本身。
func (s *Service) completeExcelJob(ctx context.Context, row store.MailExcelJob, result ExcelResult) error {
	workbook, err := json.Marshal(result.Workbook.withoutRows())
	if err != nil {
		return fmt.Errorf("encode excel job workbook: %w", err)
	}
	return retryBriefly(ctx, func() error {
		n, err := s.q.CompleteExcelJob(ctx, store.CompleteExcelJobParams{
			ID: row.ID, FileName: result.FileName, FileKey: excelResultKey(row.TenantID, row.ID),
			WorkbookJson: workbook, Model: result.Model,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			// 已经不是「处理中」了——别的副本抢先收了尾，或者标了失败。
			// 不算错，也没什么可重试的。
			s.log.Warn("Excel job was no longer processing when its result was recorded", "job", row.ID)
		}
		return nil
	})
}

// removeExcelObjects 删文件本身和 metadata 两份。S3 的 DELETE 对不存在的键
// 幂等，所以没传成、传了一半、早就删过的，都一样处理。
func (s *Service) removeExcelObjects(ctx context.Context, tenantID, jobID int64) error {
	if s.files == nil {
		return nil
	}
	for _, key := range []string{excelResultKey(tenantID, jobID), excelResultMetaKey(tenantID, jobID)} {
		if err := s.files.Remove(ctx, key); err != nil {
			return fmt.Errorf("remove %s: %w", key, err)
		}
	}
	return nil
}

const excelContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// 对象键：租户 + 任务号，**只由这两样算出来**。
//
// 带任务号，所以同一个人对同一封信转两次是两份结果，不会互相覆盖；而同一个
// 任务重传多少次都是同一个键，所以重传是覆盖不是堆积。
//
// **刻意不收文件名。** 文件名是人看的（下载时的另存为名），而它会变——同一
// 个任务重跑一次，模型可能给出不一样的名字。键要是掺了它，写进去的那个和
// 清理器算出来的那个就会对不上，而症状是桶里悄悄留下一份谁也删不掉的文件。
func excelResultKey(tenantID, jobID int64) string {
	return fmt.Sprintf("mail-excel/%d/%d.xlsx", tenantID, jobID)
}

// metadata 和文件本身并排放，同名不同后缀。清理器按同样的算法算出两个键。
func excelResultMetaKey(tenantID, jobID int64) string {
	return fmt.Sprintf("mail-excel/%d/%d.json", tenantID, jobID)
}

func (s *Service) publishExcelJob(ctx context.Context, row store.MailExcelJob) {
	if s.live == nil {
		return
	}
	s.live.ToEmployees(ctx, row.TenantID, []int64{row.OwnerID}, livefeed.Event{
		Type: livefeed.MailExcelJobChanged, Subject: fmt.Sprintf("EXCEL_JOB:%d", row.ID),
	})
}
