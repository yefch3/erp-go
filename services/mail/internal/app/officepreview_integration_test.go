package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 会读会写的假对象存储，够跑通「转一次、存下来、第二次不再转」。
type previewStore struct {
	Files
	objects map[string][]byte
}

func (f *previewStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := f.objects[key]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *previewStore) Stat(_ context.Context, key string) (int64, string, error) {
	b, ok := f.objects[key]
	if !ok {
		return 0, "", io.ErrUnexpectedEOF
	}
	return int64(len(b)), "application/pdf", nil
}

func (f *previewStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = b
	return nil
}

func (f *previewStore) PresignGetInline(_ context.Context, key, ct string) (string, error) {
	return "https://files.example/" + key + "?ct=" + ct, nil
}

// 记账的假转换器：调了几次、每次拿到什么名字。
type countingConverter struct {
	calls atomic.Int32
	names []string
	fail  bool
}

func (c *countingConverter) ToPDF(_ context.Context, fileName string, data []byte) ([]byte, error) {
	c.calls.Add(1)
	c.names = append(c.names, fileName)
	if c.fail {
		return nil, errors.New("libreoffice said no")
	}
	return append([]byte("%PDF-1.4 converted from "), data...), nil
}

func TestOfficeAttachmentIsConvertedOnceAndServedFromCacheAfterwards(t *testing.T) {
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
	const me, colleague = int64(4001), int64(4002)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()

	files := &previewStore{objects: map[string][]byte{
		"att/quote": []byte("PK\x03\x04 pretend this is a xlsx"),
		"att/photo": []byte("\xff\xd8\xff pretend jpeg"),
		"att/huge":  bytes.Repeat([]byte("x"), MaxConvertBytes+10),
	}}
	conv := &countingConverter{}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Converter: conv, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	newMail := func(owner int64, subject string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, to_email, subject, received_at)
			VALUES ($1, 1, $2, $3, $4, 'INBOX', $5, 'client@buyer.com', 'me@co.com', $6, now())
			RETURNING id`, tenantID, owner, subject+"@mid", subject+"-thr",
			time.Now().UnixNano()%1000000, subject).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	attach := func(inboundID int64, name, key string, size int64) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
			(tenant_id, inbound_id, file_name, content_type, file_size, file_key)
			VALUES ($1, $2, $3, 'application/octet-stream', $4, $5)
			RETURNING id`, tenantID, inboundID, name, size, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	mine := newMail(me, "报价")
	xlsx := attach(mine, "2026 报价单.xlsx", "att/quote", 30)
	jpeg := attach(mine, "现场照片.jpg", "att/photo", 20)
	huge := attach(mine, "全年账.xlsx", "att/huge", 30) // 库里写着 30，实际超上限
	gone := attach(mine, "没存过.docx", "", 10)

	// 第一次：真转一次，结果存进对象存储。
	url1, err := svc.PreviewInboundAttachment(ctx, tenantID, me, mine, xlsx)
	if err != nil {
		t.Fatalf("第一次预览应该成功：%v", err)
	}
	if !strings.HasPrefix(url1, "https://files.example/") || !strings.Contains(url1, "application/pdf") {
		t.Errorf("应该给一个 PDF 的内联地址：%q", url1)
	}
	if got := conv.calls.Load(); got != 1 {
		t.Fatalf("第一次应该正好转一次，实际 %d", got)
	}
	if _, ok := files.objects[previewKeyFor("att/quote")]; !ok {
		t.Error("转好的 PDF 没有存回对象存储，下次还得重转")
	}
	// 名字要原样传给转换器：LibreOffice 靠它挑过滤器。
	if conv.names[0] != "2026 报价单.xlsx" {
		t.Errorf("转换器拿到的文件名：%q", conv.names[0])
	}

	// 第二次：不该再转。这是这段代码存在的理由。
	url2, err := svc.PreviewInboundAttachment(ctx, tenantID, me, mine, xlsx)
	if err != nil {
		t.Fatalf("第二次预览应该成功：%v", err)
	}
	if url2 != url1 {
		t.Errorf("同一个附件两次应该指向同一个对象：%q vs %q", url1, url2)
	}
	if got := conv.calls.Load(); got != 1 {
		t.Errorf("第二次不该再转，转换次数变成了 %d", got)
	}

	// 图片不走这条路：它在列表那一步就有预览地址了。
	if _, err := svc.PreviewInboundAttachment(ctx, tenantID, me, mine, jpeg); err == nil {
		t.Error("图片不该走转换")
	} else if !strings.Contains(err.Error(), "MAIL_PREVIEW_FILE_TYPE") {
		t.Errorf("图片应该报类型不支持：%v", err)
	}

	// 上限由读这一侧执行，不由库里那个 file_size 说了算。
	if _, err := svc.PreviewInboundAttachment(ctx, tenantID, me, mine, huge); err == nil {
		t.Error("超过上限的文件不该被转")
	} else if !strings.Contains(err.Error(), "MAIL_PREVIEW_TOO_LARGE") {
		t.Errorf("应该报文件过大：%v", err)
	}

	// 从没存过原件的附件：没得转，如实说。
	if _, err := svc.PreviewInboundAttachment(ctx, tenantID, me, mine, gone); err == nil {
		t.Error("没有原件的附件不该给出预览")
	}

	// 别人的信：一律「不存在」，连附件 id 猜对了也一样。
	theirs := newMail(colleague, "别人的报价")
	theirAtt := attach(theirs, "机密.docx", "att/quote", 30)
	for _, c := range []struct {
		name             string
		mailID, attachID int64
	}{
		{"整封是别人的", theirs, theirAtt},
		// 拿自己的邮件号配别人的附件号：附件必须属于这封信。
		{"附件号是别人的", mine, theirAtt},
	} {
		if _, err := svc.PreviewInboundAttachment(ctx, tenantID, me, c.mailID, c.attachID); err == nil {
			t.Errorf("%s：不该给出预览", c.name)
		}
	}

	// 没配转换器：其余一切照常，只有这个动作说没配。转换器为空时，已经转好的
	// 仍然看得到——缓存不依赖转换器。
	noConv := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := noConv.PreviewInboundAttachment(ctx, tenantID, me, mine, xlsx); err != nil {
		t.Errorf("已经转好的应该还能看：%v", err)
	}
	fresh := attach(mine, "还没转过.docx", "att/quote", 30)
	// 换一个还没转过的原件键，避开上面那份缓存。
	files.objects["att/fresh"] = []byte("PK\x03\x04 another")
	_, _ = pool.Exec(ctx, `UPDATE email_inbound_attachments SET file_key='att/fresh' WHERE id=$1`, fresh)
	if _, err := noConv.PreviewInboundAttachment(ctx, tenantID, me, mine, fresh); err == nil {
		t.Error("没配转换器时，没转过的应该说没配")
	} else if !strings.Contains(err.Error(), "MAIL_PREVIEW_NOT_CONFIGURED") {
		t.Errorf("应该报未配置：%v", err)
	}

	// 转换器自己失败：说清楚是这个文件转不了，不要把 500 抛给用户。
	conv.fail = true
	if _, err := svc.PreviewInboundAttachment(ctx, tenantID, me, mine, fresh); err == nil {
		t.Error("转换失败时不该返回成功")
	} else if !strings.Contains(err.Error(), "MAIL_PREVIEW_CONVERT_FAILED") {
		t.Errorf("应该报转换失败：%v", err)
	}
}
