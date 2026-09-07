package app

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// What only a database can prove about thread de-duplication: that the same
// physical mail speaks once per conversation, and that nothing which is not
// provably a duplicate is ever silenced.
//
// The anatomy: Gmail files a copy of every send into the SENT folder, and the
// host mirror syncs that copy into email_inbound. A conversation therefore
// used to show an ERP-sent reply twice — its email_messages row and its
// mirror — and a self-addressed mail twice, as its INBOX and SENT copies.
//
// Run with: MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail

func seedInbound(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	tenantID, ownerID int64, folder, messageID, threadKey, from string, uid int64,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_inbound
		  (tenant_id, account_id, owner_id, folder, imap_uid, message_id,
		   thread_key, from_email, from_name, subject, body_html, body_text, sent_at)
		VALUES ($1, 1, $2, $3, $4, $5, $6, $7, 'x',
		        's', '', 'body', '2026-03-02 10:30:00+00')`,
		tenantID, ownerID, folder, uid, messageID, threadKey, from); err != nil {
		t.Fatalf("seed inbound %s/%s: %v", folder, messageID, err)
	}
}

func TestSentMirrorCopiesSpeakOncePerThread(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const owner = int64(8002)

	// Conversation one: an ERP-sent reply and its Gmail SENT mirror, plus the
	// customer mail that started it. The mirror's Message-ID carries the
	// outbound row's message_key — that is how the two are provably one mail.
	const key1 = "customer-thread-1"
	const sentUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0001"
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_messages
		  (tenant_id, message_key, sender_id, sender_name, to_email, subject,
		   body, body_format, status, thread_key, sent_at)
		VALUES ($1, $2::uuid, $3, '李娜', 'buyer@example.com', 'Re: inquiry',
		        'our reply', 'TEXT', 'ACCEPTED', $4, '2026-03-02 11:00:00+00')`,
		tenantID, sentUUID, owner, key1); err != nil {
		t.Fatal(err)
	}
	seedInbound(t, ctx, pool, tenantID, owner, "INBOX", "cust-msg-1@example.com", key1, "buyer@example.com", 9101)
	seedInbound(t, ctx, pool, tenantID, owner, "SENT", sentUUID+"@gmail.com", key1, "us@corp.example", 9102)

	items, err := svc.GetMailThread(ctx, tenantID, owner, 0, key1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("reply thread: want 2 entries (customer mail + our OUT row), got %d", len(items))
	}
	dirs := map[string]int{}
	for _, it := range items {
		dirs[it.Direction]++
	}
	if dirs["OUT"] != 1 || dirs["IN"] != 1 {
		t.Errorf("reply thread: the SENT mirror was not the copy silenced: %v", dirs)
	}

	// Conversation two: a mail sent to yourself. Gmail holds one message under
	// two labels; we sync two rows. The INBOX copy is the one that stays.
	const key2 = "self-send-thread"
	seedInbound(t, ctx, pool, tenantID, owner, "INBOX", "self-1@gmail.com", key2, "me@corp.example", 9103)
	seedInbound(t, ctx, pool, tenantID, owner, "SENT", "self-1@gmail.com", key2, "me@corp.example", 9104)

	items, err = svc.GetMailThread(ctx, tenantID, owner, 0, key2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("self-send thread: want the INBOX copy alone, got %d entries", len(items))
	}

	// Conversation three: SENT rows that cannot be proven duplicates stay. One
	// has no Message-ID at all; one is a mail sent from the Gmail web UI (a
	// mirror with no OUT row and no twin). Silencing either would hide real
	// correspondence.
	const key3 = "unprovable-thread"
	seedInbound(t, ctx, pool, tenantID, owner, "SENT", "", key3, "me@corp.example", 9105)
	seedInbound(t, ctx, pool, tenantID, owner, "SENT", "webmail-sent-1@gmail.com", key3, "me@corp.example", 9106)

	items, err = svc.GetMailThread(ctx, tenantID, owner, 0, key3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("unprovable duplicates: want both SENT rows kept, got %d entries", len(items))
	}
}

// 发给自己名下另一个信箱：那封信落进那个箱的**收件箱**，不是已发送。
//
// 原来的去重只挡已发送，因为当时想到的只有「Gmail 把每封发出的信也塞进已
// 发送」。于是会话里一条写着收件人、一条写着发件人，看着像是同一封信发了
// 两遍——测试时几乎必然撞上，因为测试就是发给自己。
func TestOurOwnSendIsNotShownTwiceWhenItLandsInOurOtherMailbox(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const owner = int64(8003)

	const key = "self-across-mailboxes"
	const sentUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0002"
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_messages
		  (tenant_id, message_key, sender_id, sender_name, to_email, subject,
		   body, body_format, status, thread_key, sent_at)
		VALUES ($1, $2::uuid, $3, 'CEO', 'erptest@263.net', '测试邮件',
		        'body', 'TEXT', 'ACCEPTED', $4, '2026-03-02 11:00:00+00')`,
		tenantID, sentUUID, owner, key); err != nil {
		t.Fatal(err)
	}
	// 从 Gmail 发出，落进 263 那个箱的收件箱：同一封信，Message-ID 一样。
	seedInbound(t, ctx, pool, tenantID, owner, "INBOX", sentUUID+"@gmail.com", key, "fangchen1101@gmail.com", 9201)

	items, err := svc.GetMailThread(ctx, tenantID, owner, 0, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("同一封信应该只说一次，实际 %d 条：%+v", len(items), items)
	}
	if items[0].Direction != "OUT" {
		t.Errorf("留下的该是「我发出」那一条，实际 %s", items[0].Direction)
	}

	// 别人发来的信，Message-ID 和我们发出去的任何一封都对不上——不能被挡掉。
	const key2 = "customer-reply"
	seedInbound(t, ctx, pool, tenantID, owner, "INBOX", "buyer-reply-1@example.com", key2, "buyer@example.com", 9202)
	items, err = svc.GetMailThread(ctx, tenantID, owner, 0, key2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("客户来信不该被挡掉，实际 %d 条", len(items))
	}
	if items[0].Direction != "IN" {
		t.Errorf("客户来信该是「收到」，实际 %s", items[0].Direction)
	}
}
