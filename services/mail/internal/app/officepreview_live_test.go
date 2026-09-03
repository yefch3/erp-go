package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/adapter/gotenberg"
)

// 走完整条路：服务方法 → 真的 gotenberg 适配器 → 真的 LibreOffice → 真的 PDF。
//
// 上面那条集成测试用的是假转换器，验的是「转过一次就不再转」这类自家逻辑；
// 这一条验的是另一件事——**我们对 LibreOffice 的那些假设是不是真的**：老的
// .doc 读不读得了、中文会不会变方块、转出来的到底是不是 PDF。假转换器永远
// 答不了这个。
//
// 要一个跑着的转换器：
//
//	docker run -d --rm -p 3099:3000 --name gt gotenberg/gotenberg:8
//	MAIL_TEST_GOTENBERG=http://localhost:3099 MAIL_TEST_DSN=... go test ./services/mail/internal/app/ -run Live
func TestLiveConversionTurnsARealOfficeFileIntoARealPDF(t *testing.T) {
	base := os.Getenv("MAIL_TEST_GOTENBERG")
	dsn := os.Getenv("MAIL_TEST_DSN")
	if base == "" || dsn == "" {
		t.Skip("MAIL_TEST_GOTENBERG / MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	const me = int64(5001)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()

	docx, err := os.ReadFile("testdata/中文报价单.docx")
	if err != nil {
		t.Fatal(err)
	}
	files := &previewStore{objects: map[string][]byte{"att/real": docx}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{
		Files:     files,
		Converter: gotenberg.New(base, 60*time.Second),
		Secrets:   box, Numbering: &seqNumbers{},
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var mailID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at)
		VALUES ($1, 1, $2, 'live@mid', 'live-thr', 'INBOX', 1,
		        'client@buyer.com', 'me@co.com', '真的报价', now())
		RETURNING id`, tenantID, me).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	var attID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key)
		VALUES ($1, $2, '中文报价单.docx', 'application/octet-stream', $3, 'att/real')
		RETURNING id`, tenantID, mailID, len(docx)).Scan(&attID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.PreviewInboundAttachment(ctx, tenantID, me, mailID, attID); err != nil {
		t.Fatalf("真文件转换失败：%v", err)
	}
	pdf, ok := files.objects[previewKeyFor("att/real")]
	if !ok {
		t.Fatal("转好的 PDF 没有存下来")
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("存下来的不是 PDF，头 8 个字节是 %q", pdf[:min(8, len(pdf))])
	}
	if len(pdf) < 1000 {
		t.Errorf("PDF 只有 %d 字节，可能是个空白页", len(pdf))
	}
	// 嵌进去的字体里得有一个 CJK 的，否则中文就是一页方块——而方块在字节
	// 层面看着一切正常，只有渲染出来才看得见。
	//
	// 不能写死某一个名字：Word 文档转出来嵌的是 NotoSerifCJKsc，表格嵌的是
	// NotoSansCJKsc，同一个容器里两种都会出现。要的是「有 CJK 字体」这件事。
	fonts := regexp.MustCompile(`/BaseFont\s*/([A-Za-z0-9+\-]+)`).FindAllSubmatch(pdf, -1)
	hasCJK := false
	var names []string
	for _, f := range fonts {
		names = append(names, string(f[1]))
		if bytes.Contains(f[1], []byte("CJK")) {
			hasCJK = true
		}
	}
	if !hasCJK {
		t.Errorf("PDF 里没有嵌入中文字体，中文会渲染成方块。嵌进去的是：%v", names)
	}
	t.Logf("转出来 %d 字节的 PDF", len(pdf))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
