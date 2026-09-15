package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 转换结果不能在库里躺一辈子。
//
// file_data 是这个服务唯一一处真的把文件字节写进 PostgreSQL 的地方，而且
// 只有失败的任务会被清——成功的从来没人清。这条钉住三件事：
//
//	一、过了窗口期的结果被清掉，**行还在**（用量账数的就是这些行）；
//	二、还在窗口期内的一个字节都不许动；
//	三、清掉之后再去取那个任务，得到的是一句明话，**不是一个 0 字节的文件**。
//
// 第三条是这里最要紧的：一个打不开的 .xlsx 比一句「过期了」难查得多。
func TestSweptExcelResultKeepsTheLedgerAndRefusesToServeAnEmptyFile(t *testing.T) {
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

	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 两个已经交付的任务：一个是上个月的，一个是刚才的。
	add := func(doneAgo time.Duration) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO mail_excel_jobs
			(tenant_id, owner_id, inbound_id, selected_text, status, file_name,
			 file_data, workbook_json, input_tokens, output_tokens, completed_at)
			VALUES ($1,$2,7,'一段表格','COMPLETED','报价.xlsx',$3,'{"sheets":[]}',1200,800,$4)
			RETURNING id`,
			tenantID, me, []byte("PK\x03\x04 假装是个 xlsx"),
			time.Now().Add(-doneAgo)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	old := add(30 * 24 * time.Hour)
	fresh := add(time.Hour)

	sweep := func() int64 {
		t.Helper()
		n, err := svc.q.SweepExcelJobPayloads(ctx, store.SweepExcelJobPayloadsParams{
			Cutoff:   pgtype.Timestamptz{Time: time.Now().Add(-excelPayloadRetention), Valid: true},
			RowLimit: excelSweepPerPass,
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	// 返回的条数不断言：这个清理器**有意不按租户拆**（它是维护，不回答谁的
	// 问题），所以同一个库里别人的行也会被一起扫到。要看的是这一租户里
	// 该清的清了、不该清的没动。
	sweep()
	stillHasBytes := func(id int64) bool {
		t.Helper()
		var has bool
		if err := pool.QueryRow(ctx,
			"SELECT file_data IS NOT NULL FROM mail_excel_jobs WHERE id=$1", id).Scan(&has); err != nil {
			t.Fatal(err)
		}
		return has
	}
	if stillHasBytes(old) {
		t.Fatal("过期的那个该被清掉")
	}
	if !stillHasBytes(fresh) {
		t.Fatal("还在窗口期的那个不该被动")
	}

	// 一、行还在，账还在。
	var rows, tokens int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*), coalesce(sum(input_tokens),0) FROM mail_excel_jobs WHERE tenant_id=$1`,
		tenantID).Scan(&rows, &tokens); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || tokens != 2400 {
		t.Fatalf("清的是字节不是账：应该还有 2 行、2400 个 token，实际 %d 行 %d", rows, tokens)
	}

	// 二、还在窗口期的那个一个字节都没动。
	got, err := svc.GetExcelJob(ctx, tenantID, me, fresh)
	if err != nil {
		t.Fatalf("窗口期内的结果该照常给：%v", err)
	}
	if len(got.Result.Data) == 0 {
		t.Fatal("窗口期内的结果被清掉了")
	}

	// 三、清掉的那个明说，而不是回一个空文件。
	_, err = svc.GetExcelJob(ctx, tenantID, me, old)
	if code := apierr.CodeFromError(err); code != "MAIL_EXCEL_RESULT_EXPIRED" {
		t.Fatalf("该明说已清理，得到 %q（%v）", code, err)
	}

	// 再扫一遍，这一租户里不该再有变化——清过的行 file_data 已经是 NULL，
	// 而条件里带着 IS NOT NULL。漏了那一句，这个清理器会永远在重扫同一批。
	sweep()
	if stillHasBytes(old) || !stillHasBytes(fresh) {
		t.Fatal("第二遍把不该动的动了")
	}
}
