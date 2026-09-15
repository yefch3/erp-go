package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 会赌气的对象存储：想让它坏就坏。
type moodyStore struct {
	previewStore
	refusePut  bool
	refuseStat bool
	puts       int
}

func (f *moodyStore) Put(ctx context.Context, key string, r io.Reader, n int64, ct string) error {
	f.puts++
	if f.refusePut {
		return errors.New("object storage is down")
	}
	return f.previewStore.Put(ctx, key, r, n, ct)
}

func (f *moodyStore) Stat(ctx context.Context, key string) (int64, string, error) {
	if f.refuseStat {
		return 0, "", errors.New("object storage is down")
	}
	return f.previewStore.Stat(ctx, key)
}

// 数着被问了几次的模型。答案固定：三行询盘，第二行带单价。
type countingExtractor struct {
	calls  int
	forbid *testing.T // 非 nil 时，一被问到测试就失败
}

func (e *countingExtractor) Extract(_ context.Context, in TableExtractionInput) (Extraction, error) {
	e.calls++
	if e.forbid != nil {
		e.forbid.Fatal("模型被调用了——这条路上不该问模型")
	}
	return Extraction{
		Workbook: NewTemplateWorkbook(ExtractedInquiry{
			Title: "询盘", Summary: "三条询盘，一条带单价",
			Items: []map[string]string{
				{"product": "热轧钢卷", "quantity": "100", "quantity_unit": "TON"},
				{"product": "冷轧钢卷", "quantity": "50", "quantity_unit": "TON", "unit_price": "620.5"},
				{"product": "镀锌卷", "quantity_unit": "TON"},
			},
		}, in.Columns),
		Model: "test", Usage: ModelUsage{InputTokens: 1200, OutputTokens: 800},
	}, nil
}

type excelFixture struct {
	svc   *Service
	pool  *pgxpool.Pool
	files *moodyStore
	model *countingExtractor
	logs  *bytes.Buffer // 服务写的 JSON 日志：告警靠里面的 event 字段
	tid   int64
	mail  int64
}

// loggedEvent 是「CloudWatch 上那条告警会不会响」：指标过滤器匹配的就是
// 这个字段（deploy/aws/05-alerts.sh）。
func (f *excelFixture) loggedEvent(event string) bool {
	return strings.Contains(f.logs.String(), `"event":"`+event+`"`)
}

const excelTestOwner = int64(5301)

func newExcelFixture(t *testing.T) *excelFixture {
	t.Helper()
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tid := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tid)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tid)
		pool.Close()
	})
	// 重试的退避在测试里不等。
	base := excelWriteRetryBase
	excelWriteRetryBase = time.Millisecond
	t.Cleanup(func() { excelWriteRetryBase = base })

	files := &moodyStore{previewStore: previewStore{objects: map[string][]byte{}}}
	model := &countingExtractor{}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}, Tables: model},
		slog.New(slog.NewJSONHandler(io.MultiWriter(logs, os.Stderr), nil)))

	// 一封正文里有那段文字的邮件：转换会验证选中的文字属于这封信。
	var mail int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, body_text, body_html, received_at)
		VALUES ($1, 1, $2, 'excel@mid', 'excel-thr', 'INBOX', 4242,
		        'client@buyer.com', 'me@co.com', '询盘', '一段表格', '<p>一段表格</p>', now())
		RETURNING id`, tid, excelTestOwner).Scan(&mail); err != nil {
		t.Fatal(err)
	}
	return &excelFixture{svc: svc, pool: pool, files: files, model: model, logs: logs, tid: tid, mail: mail}
}

// 一个刚被领走、还没问模型的任务。返回从库里读回来的整行——worker 手里的
// 就是这个。
func (f *excelFixture) claimed(t *testing.T) store.MailExcelJob {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO mail_excel_jobs
		(tenant_id, owner_id, inbound_id, selected_text, status, started_at)
		VALUES ($1, $2, $3, '一段表格', 'PROCESSING', now()) RETURNING id`,
		f.tid, excelTestOwner, f.mail).Scan(&id); err != nil {
		t.Fatal(err)
	}
	row, err := f.svc.q.GetExcelJob(ctx, store.GetExcelJobParams{TenantID: f.tid, OwnerID: excelTestOwner, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

type jobState struct {
	status, code, key string
	hasBytes          bool
	workbook          string
}

func (f *excelFixture) state(t *testing.T, id int64) jobState {
	t.Helper()
	var st jobState
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status, error_code, file_key, file_data IS NOT NULL, coalesce(workbook_json::text, '')
		 FROM mail_excel_jobs WHERE id=$1`, id).
		Scan(&st.status, &st.code, &st.key, &st.hasBytes, &st.workbook); err != nil {
		t.Fatal(err)
	}
	return st
}

// 顺序：文件本身 → 对象存储，metadata → 对象存储，最后库里一行。
// **文件本身从不进库**，库里只有 key 和 metadata。
func TestExcelResultGoesToObjectStorageAndOnlyMetadataToTheRow(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	row := f.claimed(t)

	f.svc.processExcelJob(ctx, row)

	if f.model.calls != 1 {
		t.Fatalf("模型该问一次，问了 %d 次", f.model.calls)
	}
	st := f.state(t, row.ID)
	if st.status != "COMPLETED" || st.key != excelResultKey(f.tid, row.ID) {
		t.Fatalf("任务该完成并记着 key：%+v", st)
	}
	if st.hasBytes {
		t.Fatal("文件本身进了库")
	}
	if strings.Contains(st.workbook, `"rows"`) || !strings.Contains(st.workbook, `"column_keys"`) {
		t.Fatalf("库里该只有 metadata：%s", st.workbook)
	}

	file := f.files.objects[excelResultKey(f.tid, row.ID)]
	if !bytes.HasPrefix(file, []byte("PK")) {
		t.Fatalf("对象存储里该是一份 xlsx，开头是 %q", file[:min(8, len(file))])
	}
	var side excelResultSidecar
	if err := json.Unmarshal(f.files.objects[excelResultMetaKey(f.tid, row.ID)], &side); err != nil {
		t.Fatal(err)
	}
	if side.FileName == "" || side.Model != "test" ||
		side.Workbook.Sheets[0].Summary != "三条询盘，一条带单价" || len(side.Workbook.Sheets[0].Rows) != 0 {
		t.Fatalf("对象存储里那份 metadata 不对：%+v", side)
	}

	job, err := f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, row.ID)
	if err != nil || !bytes.Equal(job.Result.Data, file) {
		t.Fatalf("取回来的该是对象存储里那份：%v", err)
	}
	if job.Result.FileName != side.FileName || len(job.Result.Workbook.Sheets[0].ColumnKeys) == 0 {
		t.Fatalf("库里的 metadata 读丢了：%+v", job.Result)
	}
	var tokens int64
	if err := f.pool.QueryRow(ctx, "SELECT input_tokens FROM mail_excel_jobs WHERE id=$1", row.ID).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != 1200 {
		t.Fatalf("用量没记：%d", tokens)
	}
}

// 对象存储写不进去：重试几次，然后任务标失败、说清原因。文件本身**不退回
// 库里**，模型不自动重跑——再转要人来点。
func TestStorageOutageFailsTheJobAndKeepsTheFileOutOfTheDatabase(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	f.files.refusePut = true
	row := f.claimed(t)

	f.svc.processExcelJob(ctx, row)

	if f.model.calls != 1 {
		t.Fatalf("模型该问一次，问了 %d 次", f.model.calls)
	}
	// 写死 5，不拿 excelWriteAttempts 比：常量改了断言跟着变，等于什么都没验。
	if f.files.puts != 5 {
		t.Fatalf("该重试到第 5 次再放弃，实际传了 %d 次", f.files.puts)
	}
	st := f.state(t, row.ID)
	if st.status != "FAILED" || st.code != "MAIL_EXCEL_STORAGE_UNAVAILABLE" {
		t.Fatalf("该标失败并说清是存储的问题：%+v", st)
	}
	if st.hasBytes {
		t.Fatal("文件本身退回了库里——不许")
	}
	if len(f.files.objects) != 0 {
		t.Fatalf("桶里不该有东西：%d", len(f.files.objects))
	}
	if !f.loggedEvent(excelEventStorageUnavailable) {
		t.Fatalf("告警靠的那条日志没打：\n%s", f.logs.String())
	}
}

// 文件和 metadata 都传上去了、库那一步没写成（或者进程在那一刻被杀）：任务
// 留在「处理中」，下一次领走时从对象存储里恢复，**模型一次都不多跑**。
func TestRecoveryFinishesTheJobWithoutAskingTheModelAgain(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	row := f.claimed(t)
	// 上一次跑到第 2 步之后停了：两份都在桶里，行还是处理中。
	side, err := json.Marshal(excelResultSidecar{
		FileName: "上次的.xlsx", Model: "test", Workbook: excelJobFixtureWorkbook().withoutRows(),
	})
	if err != nil {
		t.Fatal(err)
	}
	const file = "PK\x03\x04 上次写好的文件"
	f.files.objects[excelResultKey(f.tid, row.ID)] = []byte(file)
	f.files.objects[excelResultMetaKey(f.tid, row.ID)] = side
	f.model.forbid = t

	f.svc.processExcelJob(ctx, row)

	st := f.state(t, row.ID)
	if st.status != "COMPLETED" || st.key != excelResultKey(f.tid, row.ID) || st.hasBytes {
		t.Fatalf("该直接收尾：%+v", st)
	}
	if f.files.puts != 0 {
		t.Fatalf("恢复不该再传一遍：%d 次", f.files.puts)
	}
	job, err := f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Result.FileName != "上次的.xlsx" || string(job.Result.Data) != file ||
		job.Result.Workbook.Sheets[0].Summary != "三条询盘，一条带单价" {
		t.Fatalf("恢复出来的不是上次那份：%+v", job.Result)
	}
	if !f.loggedEvent(excelEventResultRecovered) {
		t.Fatalf("恢复了一次该留下记号：\n%s", f.logs.String())
	}
}

// 领走时对象存储不通：分不清有没有上次的结果，什么都不做。这时跑模型只会
// 白花钱——跑完也存不进去。
func TestUnreachableStorageAtClaimSpendsNothing(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	f.files.refuseStat = true
	row := f.claimed(t)
	f.model.forbid = t

	f.svc.processExcelJob(ctx, row)

	if st := f.state(t, row.ID); st.status != "PROCESSING" {
		t.Fatalf("该留在处理中等下一次领：%+v", st)
	}
	if f.files.puts != 0 {
		t.Fatalf("不该传任何东西：%d 次", f.files.puts)
	}
	if !f.loggedEvent(excelEventStorageUnreachable) {
		t.Fatalf("告警靠的那条日志没打：\n%s", f.logs.String())
	}
}

// 只有文件、没有 metadata（上次第 2 步没写成）：当作没有，重跑，同一个键覆盖。
func TestFileWithoutMetadataIsNotTrusted(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	row := f.claimed(t)
	f.files.objects[excelResultKey(f.tid, row.ID)] = []byte("上次的残片")

	f.svc.processExcelJob(ctx, row)

	if f.model.calls != 1 {
		t.Fatalf("模型该问一次，问了 %d 次", f.model.calls)
	}
	if st := f.state(t, row.ID); st.status != "COMPLETED" {
		t.Fatalf("%+v", st)
	}
	if file := f.files.objects[excelResultKey(f.tid, row.ID)]; !bytes.HasPrefix(file, []byte("PK")) {
		t.Fatalf("残片该被这次的文件盖掉：%q", file)
	}
}

// 过了留存期，桶里两份一起收，改动前留在库里的旧文件本身也清掉，**行留着**
// （用量账数的就是这些行）。收走之后再取，要明说——不能给一个 0 字节的 .xlsx。
func TestExpiredExcelResultIsSweptFromEverywhereButTheLedgerStays(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	fresh := f.claimed(t)
	f.svc.processExcelJob(ctx, fresh)
	if len(f.files.objects) != 2 {
		t.Fatalf("桶里该有文件本身和 metadata 两份：%d", len(f.files.objects))
	}
	// 一个改动前完成的旧任务：文件本身还在 file_data 里。
	var legacy int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO mail_excel_jobs
		(tenant_id, owner_id, inbound_id, selected_text, status, file_name, file_data, workbook_json,
		 input_tokens, output_tokens, started_at, completed_at)
		VALUES ($1, $2, $3, '一段表格', 'COMPLETED', '旧.xlsx', '\x504b'::bytea, '{}'::jsonb,
		        1200, 800, now(), now()) RETURNING id`,
		f.tid, excelTestOwner, f.mail).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx,
		"UPDATE mail_excel_jobs SET completed_at=now() - interval '30 days' WHERE tenant_id=$1", f.tid); err != nil {
		t.Fatal(err)
	}

	f.svc.sweepExcelPayloadsOnce(ctx)

	for _, id := range []int64{fresh.ID, legacy} {
		if st := f.state(t, id); st.key != "" || st.hasBytes || st.workbook != "" {
			t.Fatalf("任务 %d 该被收干净：%+v", id, st)
		}
	}
	if len(f.files.objects) != 0 {
		t.Fatalf("桶里两份都该删掉，还剩 %d 个", len(f.files.objects))
	}
	var rows, tokens int64
	if err := f.pool.QueryRow(ctx,
		"SELECT count(*), coalesce(sum(input_tokens),0) FROM mail_excel_jobs WHERE tenant_id=$1",
		f.tid).Scan(&rows, &tokens); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || tokens != 2400 {
		t.Fatalf("收的是文件不是账：应该还有 2 行、2400 token，实际 %d 行 %d", rows, tokens)
	}
	if _, err := f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, fresh.ID); apierr.CodeFromError(err) != "MAIL_EXCEL_RESULT_EXPIRED" {
		t.Fatalf("该明说已清理，得到 %v", err)
	}
	if n := f.svc.sweepExcelPayloadsOnce(ctx); n != 0 {
		t.Fatalf("第二遍不该再收到东西：%d", n)
	}
}
