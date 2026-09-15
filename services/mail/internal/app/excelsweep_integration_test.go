package app

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

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

// 转换结果：正常路径进对象存储，库里只留一个 key；写不进去才退回库里。
//
// 这条钉住的是整条链：存 → 取 → 过期收走 → 收走之后再取。每一段坏起来都
// 不报错，只给出一个空文件或者一份取不到的东西。
func TestExcelResultLivesInObjectStorageAndIsSweptFromBothPlaces(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	const me = int64(5301)
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM mail_excel_jobs WHERE tenant_id=$1", tenantID) }()

	files := &moodyStore{previewStore: previewStore{objects: map[string][]byte{}}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	const content = "PK\x03\x04 假装是一份 xlsx"
	// 直接造一个 PROCESSING 的任务，然后走真正的完成路径——这样测到的是
	// storeExcelResult，而不是一个我自己摆好的库状态。
	newJob := func() int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status, input_tokens, output_tokens, started_at)
			VALUES ($1,$2,7,'一段表格','PROCESSING',1200,800,now()) RETURNING id`,
			tenantID, me).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	finish := func(id int64) {
		t.Helper()
		key, inline := svc.storeExcelResult(ctx,
			store.MailExcelJob{ID: id, TenantID: tenantID},
			ExcelResult{FileName: "报价.xlsx", Data: []byte(content)})
		if _, err := pool.Exec(ctx, `UPDATE mail_excel_jobs
			SET status='COMPLETED', file_name='报价.xlsx', file_key=$2, file_data=$3,
			    workbook_json='{"sheets":[]}', completed_at=now()
			WHERE id=$1`, id, key, inline); err != nil {
			t.Fatal(err)
		}
	}

	// ---- 正常：进对象存储，库里不留字节 ----
	good := newJob()
	finish(good)
	var key string
	var hasInline bool
	row := func(id int64) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			"SELECT file_key, file_data IS NOT NULL FROM mail_excel_jobs WHERE id=$1", id).
			Scan(&key, &hasInline); err != nil {
			t.Fatal(err)
		}
	}
	row(good)
	if key == "" || hasInline {
		t.Fatalf("正常情况该只留一个 key：key=%q 库里还有字节=%v", key, hasInline)
	}
	if string(files.objects[key]) != content {
		t.Fatalf("对象存储里的内容不对：%q", files.objects[key])
	}
	job, err := svc.GetExcelJob(ctx, tenantID, me, good)
	if err != nil {
		t.Fatal(err)
	}
	if string(job.Result.Data) != content {
		t.Fatalf("取回来的不是存进去的：%q", job.Result.Data)
	}

	// ---- 对象存储坏了：重试几次，然后退回库里，**结果不能丢** ----
	files.refusePut = true
	files.puts = 0
	bad := newJob()
	finish(bad)
	// 写死 3，不写 excelUploadAttempts。拿常量和常量比是一句废话：把常量
	// 改成 1，断言跟着变成 1，测试照样绿——重试没了也没人知道。
	//
	// 重试必须是**上传那一下**的重试。任务级的重试（ClaimExcelJob 十五分钟
	// 后重新领取）会把模型那一次调用一起重跑，也就是再花一次钱。
	if files.puts != 3 {
		t.Fatalf("上传该重试到 3 次，实际 %d 次", files.puts)
	}
	row(bad)
	if key != "" || !hasInline {
		t.Fatalf("写不进去时该退回库里：key=%q 库里有字节=%v", key, hasInline)
	}
	files.refusePut = false
	job, err = svc.GetExcelJob(ctx, tenantID, me, bad)
	if err != nil || string(job.Result.Data) != content {
		t.Fatalf("退路上的结果也该取得到：%v %q", err, job.Result.Data)
	}

	// ---- 过期：两处一起收，行留着 ----
	if _, err := pool.Exec(ctx,
		"UPDATE mail_excel_jobs SET completed_at=now() - interval '30 days' WHERE tenant_id=$1",
		tenantID); err != nil {
		t.Fatal(err)
	}
	svc.sweepExcelPayloadsOnce(ctx)

	for _, id := range []int64{good, bad} {
		row(id)
		if key != "" || hasInline {
			t.Fatalf("任务 %d 该被收干净：key=%q 库里有字节=%v", id, key, hasInline)
		}
	}
	if len(files.objects) != 0 {
		t.Fatalf("对象存储里那一份也该删掉，还剩 %d 个", len(files.objects))
	}
	// 行和账都在。
	var rows, tokens int64
	if err := pool.QueryRow(ctx,
		"SELECT count(*), coalesce(sum(input_tokens),0) FROM mail_excel_jobs WHERE tenant_id=$1",
		tenantID).Scan(&rows, &tokens); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || tokens != 2400 {
		t.Fatalf("收的是字节不是账：应该还有 2 行、2400 token，实际 %d 行 %d", rows, tokens)
	}

	// ---- 收走之后再取：明说，不给 0 字节的文件 ----
	if _, err := svc.GetExcelJob(ctx, tenantID, me, good); apierr.CodeFromError(err) != "MAIL_EXCEL_RESULT_EXPIRED" {
		t.Fatalf("该明说已清理，得到 %v", err)
	}

	// ---- 再扫一遍不该再动任何东西 ----
	if n := svc.sweepExcelPayloadsOnce(ctx); n != 0 {
		t.Fatalf("第二遍不该再收到东西：%d", n)
	}
}
