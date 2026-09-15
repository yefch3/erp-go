package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 会话里「我发出」的那一条，附件走本地「已发送」里留着的那一份。
//
// 为什么非这样不可：那一条来自投递记录（email_messages），它的附件在
// email_attachments 里，**和收到的附件是两套各自独立的编号**。预览、在线
// 编辑、转 Excel 三条路按定义只认收件那张表——于是发出去的 .xlsx 点预览，
// 页面拿一个发件编号去收件那边找，找不到，报的还是「文件可能损坏」
// （2026-09-15 生产上实测：/mail/38240/sheet/66，而 38240 的附件是 22045）。
//
// 同一封信本地是有一份的：服务商把我们发出去的信存进「已发送」，同步回来就是
// email_inbound 的一行，附件也在收件那张表里。这个用例钉住会话把那一份的编号
// 交出来，并且附件列表换成它的。
func TestSentTurnHandsOutTheLocalCopysAttachments(t *testing.T) {
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
	const me = int64(7401)
	threadKey := fmt.Sprintf("sent-copy-%d", tenantID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_campaigns WHERE tenant_id=$1", tenantID)
	})

	files := &previewStore{objects: map[string][]byte{
		"mail/out/quote.xlsx": []byte("发件那张表里的那一份"),
		"mail/in/quote.xlsx":  []byte("已发送里留着的那一份"),
		"mail/in/logo.png":    []byte("签名档那张图"),
	}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// ---- 我们发出去的那一封：投递记录 + 它自己的发件附件 ----
	var campaignID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_campaigns
		(tenant_id, campaign_no, subject_tpl, body_tpl, sender_id)
		VALUES ($1, $2, '报价', '正文', $3) RETURNING id`,
		tenantID, fmt.Sprintf("C-SENTCOPY-%d", tenantID), me).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}
	messageKey := "11111111-2222-3333-4444-555555555555"
	var sentID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_messages
		(tenant_id, campaign_id, message_key, sender_id, sender_name, to_email, subject,
		 body, body_format, from_email, status, thread_key, sent_at)
		VALUES ($1, $2, $3::uuid, $4, 'CEO', 'buyer@overseas.com', '报价',
		        '<p>附上报价</p>', 'HTML', 'me@co.com', 'ACCEPTED', $5, now())
		RETURNING id`, tenantID, campaignID, messageKey, me, threadKey).Scan(&sentID); err != nil {
		t.Fatal(err)
	}
	var outAttID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_attachments
		(tenant_id, campaign_id, file_name, file_key, file_size, content_type)
		VALUES ($1, $2, '报价.xlsx', 'mail/out/quote.xlsx', 11,
		        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')
		RETURNING id`, tenantID, campaignID).Scan(&outAttID); err != nil {
		t.Fatal(err)
	}

	// ---- 服务商存在「已发送」里的那一份，同步回来了 ----
	//
	// message_id 由 message_key 生成，所以 ListThread 会把它挡掉（同一封信不能
	// 在会话里出现两遍）。它仍然在库里，附件也在收件那张表里——这个用例要的
	// 就是「挡掉了，但用得上」。
	var copyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, body_html, sent_message_id, received_at)
		VALUES ($1, 1, $2, $3, $4, 'SENT', 7401, 'me@co.com', 'buyer@overseas.com',
		        '报价', '<p>附上报价</p><img src="cid:logo@x">', $5, now())
		RETURNING id`, tenantID, me, messageKey+"@co.com", threadKey, sentID).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	var inAttID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
		VALUES ($1, $2, '报价.xlsx',
		        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
		        22, 'mail/in/quote.xlsx', '')
		RETURNING id`, tenantID, copyID).Scan(&inAttID); err != nil {
		t.Fatal(err)
	}
	// 正文引用的签名档图：它是正文的一部分，**不该**列成附件。换过去之后仍然
	// 不该列——不然会话里我们发出的每一条都会凭空多挂一堆 image001.png。
	if _, err := pool.Exec(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
		VALUES ($1, $2, 'logo.png', 'image/png', 9, 'mail/in/logo.png', 'logo@x')`,
		tenantID, copyID); err != nil {
		t.Fatal(err)
	}

	items, err := svc.GetMailThread(ctx, tenantID, me, copyID, threadKey)
	if err != nil {
		t.Fatal(err)
	}
	// 同一封信只说一次：投递记录那条在，已发送那份被挡掉。
	if len(items) != 1 || items[0].Direction != "OUT" || items[0].ID != sentID {
		t.Fatalf("会话该只有「我发出」那一条：%+v", items)
	}
	it := items[0]
	if it.LocalMailID != copyID {
		t.Fatalf("该交出本地那一份的编号 %d，得到 %d", copyID, it.LocalMailID)
	}
	if len(it.Attachments) != 1 {
		t.Fatalf("内嵌的签名档图不该列成附件，该只剩一个：%+v", it.Attachments)
	}
	a := it.Attachments[0]
	if a.ID != inAttID {
		t.Fatalf("附件编号该是收件那套的 %d（不是发件那套的 %d），得到 %d", inAttID, outAttID, a.ID)
	}
	if a.FileKey != "mail/in/quote.xlsx" || a.FileSize != 22 {
		t.Fatalf("该给本地那一份的文件：%+v", a)
	}
	if !strings.Contains(a.DownloadURL, "mail/in/quote.xlsx") {
		t.Fatalf("下载地址也该指向它：%q", a.DownloadURL)
	}
	// 拿这个编号真能开预览、真能转——两条路都只认收件那张表，这里验的是
	// 「交出去的编号在那张表里查得到」，也就是界面点下去不会再落空。
	atts, err := svc.q.ListInboundAttachments(ctx, store.ListInboundAttachmentsParams{
		TenantID: tenantID, InboundID: it.LocalMailID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range atts {
		if row.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("交出去的编号在收件那张表里查不到，预览和转 Excel 还是会落空：%+v", atts)
	}
}

// 一个人绑了两个信箱，从 B 箱发出去的信出现在 A 箱的会话里：留底在 B 箱，
// 照样要认。
//
// 会话里「我发出」那一腿列的是**全部**（email_messages 上还没有 account_id），
// 而收到的那一腿按信箱限定。留底的附件当初是跟着收到的那一腿一起取的，于是
// 一按信箱限定就漏掉了另一个箱里的留底——2026-09-15 生产上一条会话三封
// 「我发出」，留底分别在 23 号和 80 号两个箱里，结果只有一封有预览按钮。
func TestSentTurnFindsItsLocalCopyInTheOtherMailbox(t *testing.T) {
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
	const me = int64(7403)
	// 两个信箱：读会话用的那个，和发信用的那个。
	const readBox, sendBox = int64(23), int64(80)
	threadKey := fmt.Sprintf("two-boxes-%d", tenantID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_campaigns WHERE tenant_id=$1", tenantID)
	})

	files := &previewStore{objects: map[string][]byte{"mail/in/quote2.xlsx": []byte("另一个箱里的留底")}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 我们在 A 箱里读的那封（对方发来的）。它决定会话按哪个信箱限定。
	var opened int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, body_html, received_at)
		VALUES ($1, $2, $3, 'theirs@mid', $4, 'INBOX', 7403,
		        'buyer@overseas.com', 'a@co.com', '询价', '<p>请报价</p>', now())
		RETURNING id`, tenantID, readBox, me, threadKey).Scan(&opened); err != nil {
		t.Fatal(err)
	}

	// 从 B 箱发出去的那一封：投递记录 + 留在 B 箱「已发送」里的那一份。
	var campaignID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_campaigns
		(tenant_id, campaign_no, subject_tpl, body_tpl, sender_id)
		VALUES ($1, $2, '报价', '正文', $3) RETURNING id`,
		tenantID, fmt.Sprintf("C-TWOBOX-%d", tenantID), me).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}
	messageKey := "99999999-8888-7777-6666-555555555555"
	var sentID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_messages
		(tenant_id, campaign_id, message_key, sender_id, sender_name, to_email, subject,
		 body, body_format, from_email, status, thread_key, sent_at)
		VALUES ($1, $2, $3::uuid, $4, 'CEO', 'buyer@overseas.com', 'Re: 询价',
		        '<p>报价见附件</p>', 'HTML', 'b@co.com', 'ACCEPTED', $5, now())
		RETURNING id`, tenantID, campaignID, messageKey, me, threadKey).Scan(&sentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_attachments
		(tenant_id, campaign_id, file_name, file_key, file_size, content_type)
		VALUES ($1, $2, '报价.xlsx', 'mail/out/quote2.xlsx', 7,
		        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')`,
		tenantID, campaignID); err != nil {
		t.Fatal(err)
	}
	var copyID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, body_html, sent_message_id, received_at)
		VALUES ($1, $2, $3, $4, $5, 'SENT', 7404, 'b@co.com', 'buyer@overseas.com',
		        'Re: 询价', '<p>报价见附件</p>', $6, now())
		RETURNING id`, tenantID, sendBox, me, messageKey+"@co.com", threadKey, sentID).Scan(&copyID); err != nil {
		t.Fatal(err)
	}
	var inAttID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
		VALUES ($1, $2, '报价.xlsx',
		        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
		        33, 'mail/in/quote2.xlsx', '')
		RETURNING id`, tenantID, copyID).Scan(&inAttID); err != nil {
		t.Fatal(err)
	}

	// 从 A 箱那封点进会话——会话因此按 A 箱限定，而留底在 B 箱。
	items, err := svc.GetMailThread(ctx, tenantID, me, opened, threadKey)
	if err != nil {
		t.Fatal(err)
	}
	var out *ThreadItem
	for i := range items {
		if items[i].Direction == "OUT" {
			out = &items[i]
		}
	}
	if out == nil {
		t.Fatalf("会话里该有「我发出」那一条：%+v", items)
	}
	if out.LocalMailID != copyID {
		t.Fatalf("留底在另一个箱里也该认出来：想要 %d，得到 %d", copyID, out.LocalMailID)
	}
	if len(out.Attachments) != 1 || out.Attachments[0].ID != inAttID {
		t.Fatalf("附件该用留底那一份的编号 %d：%+v", inAttID, out.Attachments)
	}
	if out.Attachments[0].FileKey != "mail/in/quote2.xlsx" {
		t.Fatalf("该给留底那一份的文件：%+v", out.Attachments[0])
	}
}

// ---- 对不上本地那一份时：退回老样子，只能下载 ----
//
// 信箱不留已发送，或者还没同步回来。那时不能给编号——给了界面就会显示一颗
// 点下去必然落空的预览按钮，而那正是 2026-09-15 那次的样子。
func TestSentTurnWithoutALocalCopyOffersNoPreview(t *testing.T) {
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
	const me = int64(7402)
	threadKey := fmt.Sprintf("no-copy-%d", tenantID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_campaigns WHERE tenant_id=$1", tenantID)
	})

	files := &previewStore{objects: map[string][]byte{"mail/out/spec.docx": []byte("x")}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 配上在线 Office，不然 .docx 本来就不会被标成 office 那一档，
	// 下面那句断言就成了永远成立的空话。
	office := NewOffice("https://erp.example/docs", "s3cr3t-office-key", "http://mail:9011")
	if office == nil {
		t.Fatal("office viewer should be configured for this test")
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}, Office: office},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var campaignID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_campaigns
		(tenant_id, campaign_no, subject_tpl, body_tpl, sender_id)
		VALUES ($1, $2, '主题', '正文', $3) RETURNING id`,
		tenantID, fmt.Sprintf("C-NOCOPY-%d", tenantID), me).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}
	var sentID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_messages
		(tenant_id, campaign_id, message_key, sender_id, sender_name, to_email, subject,
		 body, body_format, from_email, status, thread_key, sent_at)
		VALUES ($1, $2, gen_random_uuid(), $3, 'CEO', 'buyer@overseas.com', '规格',
		        '<p>见附件</p>', 'HTML', 'me@co.com', 'ACCEPTED', $4, now())
		RETURNING id`, tenantID, campaignID, me, threadKey).Scan(&sentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_attachments
		(tenant_id, campaign_id, file_name, file_key, file_size, content_type)
		VALUES ($1, $2, '规格.docx', 'mail/out/spec.docx', 5,
		        'application/vnd.openxmlformats-officedocument.wordprocessingml.document')`,
		tenantID, campaignID); err != nil {
		t.Fatal(err)
	}

	items, err := svc.GetMailThread(ctx, tenantID, me, sentID, threadKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].Attachments) != 1 {
		t.Fatalf("%+v", items)
	}
	if items[0].LocalMailID != 0 {
		t.Fatalf("没有本地那一份就不能给编号，得到 %d", items[0].LocalMailID)
	}
	if kind := items[0].Attachments[0].PreviewKind; kind != "" {
		t.Fatalf("在线 Office 那一档该摘掉，不然按钮点了必然落空：%q", kind)
	}
	if items[0].Attachments[0].DownloadURL == "" {
		t.Fatal("下载不该受影响")
	}
}
