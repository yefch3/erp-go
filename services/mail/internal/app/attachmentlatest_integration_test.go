package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 在线改过的附件，页面上的「下载」、「下载全部」打的包、会话视图里的那一份，
// 给的都是改过的最新版——和「预览」打开的是同一个文件。
//
// 2026-09-15 之前只有预览给改过的，下载给原件，页面上靠一个「已改 · 第 N 版」
// 标记提醒。标记去掉之后，四条路要是还不一致，一个人改完再下载拿到的是没改
// 过的，而且没有任何提示。
func TestEditedAttachmentIsWhatEveryButtonHandsOut(t *testing.T) {
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
	const me = int64(7301)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_attachment_revisions WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	})

	const original, edited = "original quote", "edited quote, longer"
	files := &previewStore{objects: map[string][]byte{
		"att/orig": []byte(original),
		"att/v1":   []byte("first edit"),
		"att/v2":   []byte(edited),
	}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var mailID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, body_html, received_at)
		VALUES ($1, 1, $2, 'latest@mid', 'latest-thr', 'INBOX', 4301,
		        'client@buyer.com', 'me@co.com', 'Steel Request', '<p>see attached</p>', now())
		RETURNING id`, tenantID, me).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	var attID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
		VALUES ($1, $2, 'Steel Request.xlsx',
		        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
		        $3, 'att/orig', '')
		RETURNING id`, tenantID, mailID, len(original)).Scan(&attID); err != nil {
		t.Fatal(err)
	}
	// 改过两回。最新的是第 2 版。
	if _, err := pool.Exec(ctx, `INSERT INTO mail_attachment_revisions
		(tenant_id, attachment_id, inbound_id, version, file_key, file_size, edited_by)
		VALUES ($1, $2, $3, 1, 'att/v1', 10, $4), ($1, $2, $3, 2, 'att/v2', $5, $4)`,
		tenantID, attID, mailID, me, len(edited)); err != nil {
		t.Fatal(err)
	}

	// ---- 单封视图：下载按钮 ----
	view, err := svc.GetInbound(ctx, tenantID, me, mailID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Attachments) != 1 {
		t.Fatalf("该列出 1 个附件，实际 %d", len(view.Attachments))
	}
	a := view.Attachments[0]
	if a.FileKey != "att/v2" || a.FileSize != int64(len(edited)) || a.Revision != 2 {
		t.Fatalf("单封视图该给第 2 版：key=%q size=%d rev=%d", a.FileKey, a.FileSize, a.Revision)
	}
	if !strings.Contains(a.DownloadURL, "att/v2") {
		t.Fatalf("下载地址该指向第 2 版：%q", a.DownloadURL)
	}

	// ---- 「下载全部」的包 ----
	bundle, err := svc.ZipInboundAttachments(ctx, tenantID, me, mailID)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(bundle.Content), int64(len(bundle.Content)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("包里该有 1 个文件，实际 %d", len(zr.File))
	}
	r, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if string(got) != edited {
		t.Fatalf("包里装的该是改过的那一版，实际 %q", got)
	}

	// ---- 会话视图 ----
	items, err := svc.GetMailThread(ctx, tenantID, me, mailID, "latest-thr")
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, item := range items {
		if item.Direction != "IN" || item.ID != mailID {
			continue
		}
		seen = true
		if len(item.Attachments) != 1 || item.Attachments[0].FileKey != "att/v2" ||
			!strings.Contains(item.Attachments[0].DownloadURL, "att/v2") {
			t.Fatalf("会话视图该给第 2 版：%+v", item.Attachments)
		}
	}
	if !seen {
		t.Fatal("会话里没找到这封信")
	}
}
