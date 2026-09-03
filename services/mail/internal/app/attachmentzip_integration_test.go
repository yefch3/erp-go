package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestAttachmentBundleHoldsEveryListedFileExactlyOnce(t *testing.T) {
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
	const me, colleague = int64(7001), int64(7002)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()

	files := &previewStore{objects: map[string][]byte{
		"att/a":    []byte("first quotation"),
		"att/b":    []byte("second quotation, same name"),
		"att/logo": []byte("signature logo bytes"),
		"att/big":  bytes.Repeat([]byte("x"), 64),
	}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	newMail := func(owner int64, subject, bodyHTML string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, to_email, subject, body_html, received_at)
			VALUES ($1, 1, $2, $3, $4, 'INBOX', $5, 'client@buyer.com', 'me@co.com', $6, $7, now())
			RETURNING id`, tenantID, owner, subject+"@mid", subject+"-thr",
			time.Now().UnixNano()%1000000, subject, bodyHTML).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	attach := func(inboundID int64, name, key, cid, ct string, size int64) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
			(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id`, tenantID, inboundID, name, ct, size, key, cid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// 正文里引用了签名档那张图，所以它不算附件。
	mine := newMail(me, "Steel Request", `<p>hi</p><img src="cid:logo@x">`)
	const bin = "application/octet-stream"
	attach(mine, "报价单.xlsx", "att/a", "", bin, 15)
	attach(mine, "报价单.xlsx", "att/b", "", bin, 27) // 同名，必须两个都在
	// 真的是图片，所以正文换得掉它——页面上不列，包里也不该有。类型写对很
	// 重要：写成 octet-stream 的话正文根本换不掉，页面上仍然列着它，那它
	// 就该进包（见 TestBundleContainsExactlyWhatTheDetailPageLists）。
	attach(mine, "logo.png", "att/logo", "logo@x", "image/png", 20)
	attach(mine, "丢了原件.pdf", "att/gone", "", bin, 10) // 对象不在，跳过但不连累别人
	attach(mine, "没存过.doc", "", "", bin, 10)          // 从没存过原件

	bundle, err := svc.ZipInboundAttachments(ctx, tenantID, me, mine)
	if err != nil {
		t.Fatalf("打包失败：%v", err)
	}
	if bundle.FileName != "Steel Request-附件.zip" {
		t.Errorf("包名：%q", bundle.FileName)
	}
	zr, err := zip.NewReader(bytes.NewReader(bundle.Content), int64(len(bundle.Content)))
	if err != nil {
		t.Fatalf("打出来的不是合法压缩包：%v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(rc)
		rc.Close()
		got[f.Name] = b.String()
	}
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)

	// 两个同名的都在，内容各是各的。
	if got["报价单.xlsx"] != "first quotation" {
		t.Errorf("第一个报价单的内容：%q", got["报价单.xlsx"])
	}
	if got["报价单 (2).xlsx"] != "second quotation, same name" {
		t.Errorf("第二个同名附件丢了或串了：%q（包里有 %v）", got["报价单 (2).xlsx"], names)
	}
	// 正文里显示过的内嵌图不进包。
	if _, ok := got["logo.png"]; ok {
		t.Errorf("签名档的图不该进包：%v", names)
	}
	// 读不到的和没存过的都不进包，但也没让整次打包失败。
	if len(got) != 2 {
		t.Errorf("包里应该正好两个文件，实际 %v", names)
	}

	// 别人的信：一律「不存在」。
	theirs := newMail(colleague, "别人的", "")
	attach(theirs, "机密.xlsx", "att/a", "", bin, 15)
	if _, err := svc.ZipInboundAttachments(ctx, tenantID, me, theirs); err == nil {
		t.Error("不该打包别人的信")
	}

	// 一个附件都没有的信：说清楚，而不是给一个空压缩包。
	empty := newMail(me, "没有附件", "")
	if _, err := svc.ZipInboundAttachments(ctx, tenantID, me, empty); err == nil {
		t.Error("没有附件时不该返回成功")
	} else if !strings.Contains(err.Error(), "MAIL_ZIP_NO_FILES") {
		t.Errorf("应该报没有可下载的附件：%v", err)
	}

	// 全部读不到：不能给一个空包，那会让人以为附件本来就是空的。
	broken := newMail(me, "原件都没了", "")
	attach(broken, "x.xlsx", "att/nowhere", "", bin, 10)
	if _, err := svc.ZipInboundAttachments(ctx, tenantID, me, broken); err == nil {
		t.Error("一个都读不到时不该返回一个空包")
	} else if !strings.Contains(err.Error(), "MAIL_ZIP_UNREADABLE") {
		t.Errorf("应该报读不到：%v", err)
	}

	// 超过上限：先按库里记的大小拦下，不要读了几十兆才发现。
	huge := newMail(me, "太大了", "")
	attach(huge, "big.xlsx", "att/big", "", bin, MaxZipBytes+1)
	if _, err := svc.ZipInboundAttachments(ctx, tenantID, me, huge); err == nil {
		t.Error("超过上限不该打包")
	} else if !strings.Contains(err.Error(), "MAIL_ZIP_TOO_LARGE") {
		t.Errorf("应该报太大：%v", err)
	}
}

// 压缩包里的文件必须**正好等于**详情页列出来的文件。
//
// 这条是从一次真实的偏差里来的：内嵌图的隐藏条件有两半——正文引用了它，
// **而且**它真的被换成了一张能显示的图。只写前一半的话，一个「正文引用了
// 它、但它换不掉」的附件会从包里消失，而页面上还好端端列着它。少一个文件
// 的压缩包不报错，谁也不会发现。
//
// 换不掉最常见的原因不是出错，是**它根本不是图片**：带 Content-ID 的 PDF
// 一样会被正文引用，而签名那一步只认图片类型。
func TestBundleContainsExactlyWhatTheDetailPageLists(t *testing.T) {
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
	const me = int64(7101)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()

	files := &previewStore{objects: map[string][]byte{
		"z/quote": []byte("quotation"),
		"z/logo":  []byte("a real png"),
		"z/spec":  []byte("a pdf the body points at"),
	}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 正文引用两个部件：一张 png（换得掉，于是不算附件），
	// 一个 pdf（签名那一步只认图片，换不掉，于是页面上仍然列着它）。
	var mailID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, body_html, received_at)
		VALUES ($1, 1, $2, 'exact@mid', 'exact-thr', 'INBOX', 991,
		        'client@buyer.com', 'me@co.com', '一致性',
		        '<p>x</p><img src="cid:logo@x"><img src="cid:spec@x">', now())
		RETURNING id`, tenantID, me).Scan(&mailID); err != nil {
		t.Fatal(err)
	}
	add := func(name, key, cid, ct string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound_attachments
			(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
			VALUES ($1, $2, $3, $4, 9, $5, $6)`,
			tenantID, mailID, name, ct, key, cid); err != nil {
			t.Fatal(err)
		}
	}
	add("报价.pdf", "z/quote", "", "application/pdf")
	add("signature.png", "z/logo", "logo@x", "image/png")
	add("规格书.pdf", "z/spec", "spec@x", "application/pdf")

	view, err := svc.GetInbound(ctx, tenantID, me, mailID)
	if err != nil {
		t.Fatal(err)
	}
	onPage := map[string]bool{}
	for _, a := range view.Attachments {
		onPage[a.FileName] = true
	}

	bundle, err := svc.ZipInboundAttachments(ctx, tenantID, me, mailID)
	if err != nil {
		t.Fatalf("打包失败：%v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(bundle.Content), int64(len(bundle.Content)))
	if err != nil {
		t.Fatal(err)
	}
	inZip := map[string]bool{}
	for _, f := range zr.File {
		inZip[f.Name] = true
	}
	for name := range onPage {
		if !inZip[name] {
			t.Errorf("页面上列着 %q，包里却没有（页面 %v，包 %v）", name, keysOf(onPage), keysOf(inZip))
		}
	}
	for name := range inZip {
		if !onPage[name] {
			t.Errorf("包里多了 %q，页面上并没有列它（页面 %v，包 %v）", name, keysOf(onPage), keysOf(inZip))
		}
	}
	// 具体到这三个：换得掉的图两边都没有，换不掉的 pdf 两边都有。
	if onPage["signature.png"] || inZip["signature.png"] {
		t.Errorf("正文已经显示的图不该出现：页面 %v，包 %v", keysOf(onPage), keysOf(inZip))
	}
	if !inZip["规格书.pdf"] {
		t.Errorf("正文引用但换不掉的部件页面上还列着，包里也该有：包 %v", keysOf(inZip))
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
