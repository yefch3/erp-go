package app

import (
	"log/slog"
	"os"
	"testing"
)

// 我们自己发出的那封，从邮箱的「已发送」同步回来时必须落回同一个会话。
//
// Gmail 把我们发出的信留一份在 SENT 文件夹，同步回来就是 email_inbound 里的一
// 行。它没有 In-Reply-To、也没有 References —— 它是对话的第一封。原先
// resolveThread 只看这两个头，于是走到最后的兜底：thread_key = 它自己的
// Message-ID 原文，也就是 <uuid@domain>。
//
// 而客户的回复带着 In-Reply-To: <uuid@domain>，resolveThread 从中取出 uuid，
// 在 email_messages 里查到我们的记录，继承的是那条记录的会话键 —— 裸 uuid。
//
// 两个字符串不同，于是同一段对话被劈成两个会话：收件箱里看得到三封往来，
// 已发送里那封孤零零一封，看不出客户已经回过信。
//
// 运行：MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail
func TestSentCopyJoinsTheThreadItStarted(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const owner = int64(8301)
	const sentUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0301"
	const ourThread = "thread-we-started"

	if _, err := pool.Exec(ctx, `
		INSERT INTO email_messages
		  (tenant_id, message_key, sender_id, sender_name, to_email, subject,
		   body, body_format, status, thread_key, sent_at)
		VALUES ($1, $2::uuid, $3, '李娜', 'buyer@example.com', '报价',
		        'our mail', 'TEXT', 'ACCEPTED', $4, now())`,
		tenantID, sentUUID, owner, ourThread); err != nil {
		t.Fatal(err)
	}

	// 邮箱把它还回来：自己的 Message-ID，没有任何引用头。
	thread, replyTo, _ := svc.resolveThread(ctx, tenantID, ParsedMail{
		MessageID: "<" + sentUUID + "@gmail.com>",
	})

	if thread != ourThread {
		t.Errorf("会话键 = %q，期望 %q —— 已发送的副本自成一个会话，"+
			"于是客户的回复看不见它", thread, ourThread)
	}
	// 它就是那封被发出去的信，不是对它的回复。指向自己会让链条成环。
	if replyTo != nil {
		t.Errorf("reply_to_id = %v，期望 nil：这封信不是对自己的回复", *replyTo)
	}
}

// 真正的回复仍然优先。
//
// 一封信可以既是对我们的回应、又是我们自己发的（同事用同一个邮箱回信）。这时
// 该跟着它所答复的那条链走，而不是跟着自己的 Message-ID —— 后者会把一封回复
// 变成它自己会话的开头。
func TestAGenuineReplyStillWinsOverTheSelfAnchor(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	const owner = int64(8302)
	const firstUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0302"
	const secondUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeffff0303"
	const firstThread = "the-original-conversation"

	for _, m := range []struct {
		key, thread string
	}{
		{firstUUID, firstThread},
		{secondUUID, "a-thread-of-its-own"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_messages
			  (tenant_id, message_key, sender_id, sender_name, to_email, subject,
			   body, body_format, status, thread_key, sent_at)
			VALUES ($1, $2::uuid, $3, '李娜', 'buyer@example.com', '报价',
			        'x', 'TEXT', 'ACCEPTED', $4, now())`,
			tenantID, m.key, owner, m.thread); err != nil {
			t.Fatal(err)
		}
	}

	// 第二封既回复了第一封，又是我们自己发的。
	thread, replyTo, _ := svc.resolveThread(ctx, tenantID, ParsedMail{
		MessageID: "<" + secondUUID + "@gmail.com>",
		InReplyTo: "<" + firstUUID + "@gmail.com>",
	})

	if thread != firstThread {
		t.Errorf("会话键 = %q，期望 %q —— 它答复的那条链才是对的会话", thread, firstThread)
	}
	if replyTo == nil {
		t.Error("reply_to_id 为空：它确实是对第一封的回复，链条要连上")
	}
}
