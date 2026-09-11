package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/mail/internal/store"
)

// 「已回复」标识认的是哪一封（issue #364）。
//
// 这条路上没有外键：发出去的那封回信带着 RFC 头里的 In-Reply-To，收到的信存着
// 自己的 Message-ID，两者靠字符串对上。所以尖括号、信箱归属、重复触发这三件事
// 都得钉住——错了不会报错，只会让别人信箱里的一行、或者一封根本没答过的信，
// 在列表上亮起「已回复」。
func TestAnsweredMatchesTheRightMail(t *testing.T) {
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
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	mine, theirs := tenantID%100000+960001, tenantID%100000+960002
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 四行，Message-ID 全一样。只有第一行该被标上。
	add := func(owner, account int64, uid int64, msgID, folder string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, owner_id, account_id, folder, imap_uid, message_id, subject)
			VALUES ($1,$2,$3,$4,$5,$6,'报价') RETURNING id`,
			tenantID, owner, account, folder, uid, msgID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	target := add(mine, 1, 101, "abc@buyer.com", "INBOX")
	otherAccount := add(mine, 2, 102, "abc@buyer.com", "INBOX")
	otherOwner := add(theirs, 1, 103, "abc@buyer.com", "INBOX")
	// message_id 是空串的行：Message-ID 头缺失的信真实存在（有些群发系统不写）。
	// 不挡住的话，一封同样没有 In-Reply-To 的回信会把它们**全部**标成已回复。
	noID := add(mine, 1, 104, "", "INBOX")

	answered := func(id int64) bool {
		t.Helper()
		var v bool
		if err := pool.QueryRow(ctx,
			"SELECT is_answered FROM email_inbound WHERE id=$1", id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	// 发出去的那封信带的是头里的原样：**有尖括号**。库里存的没有。
	rows, err := svc.q.MarkInboundAnsweredByMessageID(ctx, store.MarkInboundAnsweredByMessageIDParams{
		TenantID: tenantID, OwnerID: mine, AccountID: 1, InReplyTo: "<abc@buyer.com>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应该正好标中一封，实际 %d 封", len(rows))
	}
	if rows[0].ID != target {
		t.Errorf("标错了信：想要 %d，实际 %d", target, rows[0].ID)
	}
	// 回传的这两样是「到服务器上哪里去找这封信」，少了就推不上 \Answered。
	if rows[0].Folder != "INBOX" || rows[0].ImapUid != 101 {
		t.Errorf("回传的位置不对：%s/%d", rows[0].Folder, rows[0].ImapUid)
	}

	if !answered(target) {
		t.Error("该标的没标上")
	}
	if answered(otherAccount) {
		t.Error("标到另一个信箱的同名会话上了")
	}
	if answered(otherOwner) {
		t.Error("标到别人信箱里去了")
	}
	if answered(noID) {
		t.Error("Message-ID 是空串的信被误标了")
	}

	// 再发一封同样的回信（合并发送一封信对多个人、或者重试），不该再回一行——
	// 每回一行就多排一次推给服务器的 \Answered，而那件事已经做过了。
	again, err := svc.q.MarkInboundAnsweredByMessageID(ctx, store.MarkInboundAnsweredByMessageIDParams{
		TenantID: tenantID, OwnerID: mine, AccountID: 1, InReplyTo: "<abc@buyer.com>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("已经标过的信不该再回一行，实际回了 %d 行", len(again))
	}

	// 空的 In-Reply-To（不是回信）一行都不该动。
	none, err := svc.q.MarkInboundAnsweredByMessageID(ctx, store.MarkInboundAnsweredByMessageIDParams{
		TenantID: tenantID, OwnerID: mine, AccountID: 1, InReplyTo: "<>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("空的 In-Reply-To 标中了 %d 封", len(none))
	}
}

// 服务器那一半：别处回的信，靠 \Answered 传回来。
//
// 只往 true 走是有意的，所以单独钉住：服务器上没有这个标志有两种读法（真没
// 答过 / 这台服务器不保存它），而擦掉一个「答过」会让业务员照着它再写一封。
func TestAnsweredFromHostOnlyGoesForward(t *testing.T) {
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
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	owner := tenantID%100000 + 950001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, owner_id, account_id, folder, imap_uid, message_id, subject)
		VALUES ($1,$2,1,'INBOX',201,'x@buyer.com','报价') RETURNING id`,
		tenantID, owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	set := func() {
		t.Helper()
		if err := svc.q.SetInboundAnsweredByUID(ctx, store.SetInboundAnsweredByUIDParams{
			TenantID: tenantID, AccountID: 1, Folder: "INBOX", ImapUid: 201,
		}); err != nil {
			t.Fatal(err)
		}
	}
	answered := func() bool {
		t.Helper()
		var v bool
		if err := pool.QueryRow(ctx,
			"SELECT is_answered FROM email_inbound WHERE id=$1", id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	if answered() {
		t.Fatal("新收到的信不该一开始就是已回复")
	}
	set()
	if !answered() {
		t.Error("服务器说答过了，我们没跟上")
	}
	// 再来一次不出错、也不改变什么——每一轮对账都会走到这里。
	set()
	if !answered() {
		t.Error("第二次对账把标识擦掉了")
	}
}
