package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
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
	refusePut bool
	puts      int
}

func (f *moodyStore) Put(ctx context.Context, key string, r io.Reader, n int64, ct string) error {
	f.puts++
	if f.refusePut {
		return errors.New("object storage is down")
	}
	return f.previewStore.Put(ctx, key, r, n, ct)
}

type excelFixture struct {
	svc   *Service
	pool  *pgxpool.Pool
	files *moodyStore
	tid   int64
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
		pool.Close()
	})
	files := &moodyStore{previewStore: previewStore{objects: map[string][]byte{}}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	return &excelFixture{svc: svc, pool: pool, files: files, tid: tid}
}

// 走真正的完成路径：造一个 PROCESSING 的任务，再用 CompleteExcelJob 收尾。
func (f *excelFixture) complete(t *testing.T, content string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO mail_excel_jobs
		(tenant_id, owner_id, inbound_id, selected_text, status, input_tokens, output_tokens, started_at)
		VALUES ($1,$2,7,'一段表格','PROCESSING',1200,800,now()) RETURNING id`,
		f.tid, excelTestOwner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	workbook, _ := json.Marshal(Workbook{})
	if _, err := f.svc.q.CompleteExcelJob(ctx, store.CompleteExcelJobParams{
		ID: id, FileName: "报价.xlsx", FileData: []byte(content),
		WorkbookJson: workbook, Model: "test",
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *excelFixture) row(t *testing.T, id int64) (key string, hasBytes bool) {
	t.Helper()
	if err := f.pool.QueryRow(context.Background(),
		"SELECT file_key, file_data IS NOT NULL FROM mail_excel_jobs WHERE id=$1", id).
		Scan(&key, &hasBytes); err != nil {
		t.Fatal(err)
	}
	return key, hasBytes
}

// 落库和写对象存储是两次写，中间没有事务。这条钉住的是那个缺口**被顺序消掉了**：
//
//	完成任务时只写库（一条语句，要么全成要么全不成），对象存储交给会重试的
//	搬运工。于是任何时刻，file_data 和 file_key **至少有一处是全的**——
//	人永远取得到这份文件。
func TestExcelResultIsAlwaysReachableWhileItMovesToObjectStorage(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	const content = "PK\x03\x04 假装是一份 xlsx"

	// ---- 刚完成：字节在库里，还没有 key，但已经取得到 ----
	id := f.complete(t, content)
	key, hasBytes := f.row(t, id)
	if key != "" || !hasBytes {
		t.Fatalf("完成那一刻该只有字节：key=%q 有字节=%v", key, hasBytes)
	}
	job, err := f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, id)
	if err != nil || string(job.Result.Data) != content {
		t.Fatalf("还没搬走的时候也该取得到：%v %q", err, job.Result.Data)
	}

	// ---- 对象存储坏着：搬不走，但**人照样取得到** ----
	f.files.refusePut = true
	f.svc.uploadExcelResultsOnce(ctx)
	key, hasBytes = f.row(t, id)
	if key != "" || !hasBytes {
		t.Fatalf("搬不走时字节一个都不能动：key=%q 有字节=%v", key, hasBytes)
	}
	job, err = f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, id)
	if err != nil || string(job.Result.Data) != content {
		t.Fatalf("搬不走不等于取不到：%v %q", err, job.Result.Data)
	}
	var attempts int32
	var nextTry *time.Time
	if err := f.pool.QueryRow(ctx,
		"SELECT upload_attempts, upload_next_try_at FROM mail_excel_jobs WHERE id=$1", id).
		Scan(&attempts, &nextTry); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || nextTry == nil || !nextTry.After(time.Now()) {
		t.Fatalf("该记一次失败并退避：attempts=%d next=%v", attempts, nextTry)
	}

	// ---- 存储好了：搬走，同一条语句写 key、清字节 ----
	f.files.refusePut = false
	if _, err := f.pool.Exec(ctx,
		"UPDATE mail_excel_jobs SET upload_next_try_at=now() WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	f.svc.uploadExcelResultsOnce(ctx)
	key, hasBytes = f.row(t, id)
	if key == "" || hasBytes {
		t.Fatalf("搬走之后该只剩 key：key=%q 还有字节=%v", key, hasBytes)
	}
	if string(f.files.objects[key]) != content {
		t.Fatalf("对象存储里的内容不对：%q", f.files.objects[key])
	}
	job, err = f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, id)
	if err != nil || string(job.Result.Data) != content {
		t.Fatalf("搬走之后照样取得到：%v %q", err, job.Result.Data)
	}

	// ---- 再跑一趟不该重复搬 ----
	f.files.puts = 0
	f.svc.uploadExcelResultsOnce(ctx)
	if f.files.puts != 0 {
		t.Fatalf("已经搬完的不该再传：%d 次", f.files.puts)
	}
}

// 「传成功了、但那一条清理字节的语句没写成」——两次写之间最后一个缺口。
//
// 这时对象在、字节也在。**读的仍然是字节**（file_key 还是空的），所以人不受
// 影响；下一趟重传同一个键（覆盖，不是堆积）再清一次。这里钉的是"重来一次
// 就能自愈"，而不是"留下一个要人来打扫的状态"。
func TestExcelUploadHealsWhenTheRowUpdateDidNotLand(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()
	const content = "PK\x03\x04 传上去了但行没写成"
	id := f.complete(t, content)

	// 手工制造那个中间态：对象已经在桶里，而行还没记上。
	key := excelResultKey(f.tid, id, "报价.xlsx")
	f.files.objects[key] = []byte(content)

	// 人在这一刻取文件：读的是库里的字节，照常拿得到。
	job, err := f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, id)
	if err != nil || string(job.Result.Data) != content {
		t.Fatalf("中间态也该取得到：%v %q", err, job.Result.Data)
	}

	// 下一趟：重传同一个键（覆盖），然后把行写上。
	f.svc.uploadExcelResultsOnce(ctx)
	gotKey, hasBytes := f.row(t, id)
	if gotKey != key || hasBytes {
		t.Fatalf("重来一趟该自愈：key=%q 还有字节=%v", gotKey, hasBytes)
	}
	if len(f.files.objects) != 1 {
		t.Fatalf("重传是覆盖不是堆积，桶里该只有一份：%d", len(f.files.objects))
	}
}

// 过了留存期，两处一起收走，**行留着**（用量账数的就是这些行）。
// 收走之后再取，要明说——不能给一个 0 字节的 .xlsx。
func TestExpiredExcelResultIsSweptFromBothPlacesButTheLedgerStays(t *testing.T) {
	f := newExcelFixture(t)
	ctx := context.Background()

	moved := f.complete(t, "搬走了的")
	f.svc.uploadExcelResultsOnce(ctx)
	stuck := f.complete(t, "还没搬走的")

	if _, err := f.pool.Exec(ctx,
		"UPDATE mail_excel_jobs SET completed_at=now() - interval '30 days' WHERE tenant_id=$1",
		f.tid); err != nil {
		t.Fatal(err)
	}
	f.svc.sweepExcelPayloadsOnce(ctx)

	for _, id := range []int64{moved, stuck} {
		key, hasBytes := f.row(t, id)
		if key != "" || hasBytes {
			t.Fatalf("任务 %d 该被收干净：key=%q 有字节=%v", id, key, hasBytes)
		}
	}
	if len(f.files.objects) != 0 {
		t.Fatalf("对象存储里那一份也该删掉，还剩 %d 个", len(f.files.objects))
	}

	var rows, tokens int64
	if err := f.pool.QueryRow(ctx,
		"SELECT count(*), coalesce(sum(input_tokens),0) FROM mail_excel_jobs WHERE tenant_id=$1",
		f.tid).Scan(&rows, &tokens); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || tokens != 2400 {
		t.Fatalf("收的是字节不是账：应该还有 2 行、2400 token，实际 %d 行 %d", rows, tokens)
	}

	if _, err := f.svc.GetExcelJob(ctx, f.tid, excelTestOwner, moved); apierr.CodeFromError(err) != "MAIL_EXCEL_RESULT_EXPIRED" {
		t.Fatalf("该明说已清理，得到 %v", err)
	}
	if n := f.svc.sweepExcelPayloadsOnce(ctx); n != 0 {
		t.Fatalf("第二遍不该再收到东西：%d", n)
	}
}
