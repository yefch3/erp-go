package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 从垃圾箱救回一封信，不该变成两封。
//
// 赛跑的形状：MoveMessages 一执行，收件箱的 IDLE 立刻收到推送，同步抢在挪信
// 收尾（repoint）之前把这封信当新邮件下载了一遍。目的位置被同一封信的年轻副本
// 占住，原来的 RepointInbound 撞上唯一约束就放弃——旧行留在垃圾箱，会话里同一
// 封信出现两次。因为 IDLE 就是为「秒级」设计的，这场赛跑几乎必输，用户第一次
// 救信就撞上了。
//
// 修法是 mergeRepoint：占位者与被挪的信同一个 Message-ID 时，清掉年轻副本
// （行、附件、对象一起，走 purgeOne），位置让给带着阅读状态和引用的旧行。
func TestRescueDoesNotLeaveTheMailTwice(t *testing.T) {
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
	const owner = 701
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()

	put := func(folder string, uid int64, messageID, subject string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, folder, imap_uid, message_id, thread_key,
			 from_email, from_name, subject, body_text, is_read, sent_at)
			VALUES ($1,1,$2,$3,$4,$5,'t-'||$5,'buyer@example.com','Ana',$6,'body',$7,now())
			RETURNING id`,
			tenantID, owner, folder, uid, messageID, subject, folder == "JUNK").Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	countByMessageID := func(messageID string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM email_inbound WHERE tenant_id=$1 AND message_id=$2",
			tenantID, messageID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	mid := fmt.Sprintf("<m-%d@example.com>", tenantID)

	// 旧行在垃圾箱（已读——阅读状态是要保住的历史）；年轻副本已被同步抢先
	// 塞进收件箱的新 UID 上，还带着一个附件行。
	oldID := put("JUNK", 100, mid, "测试邮件")
	dupID := put("INBOX", 200, mid, "测试邮件")
	if _, err := pool.Exec(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key)
		VALUES ($1,$2,'a.pdf','application/pdf',10,'')`, tenantID, dupID); err != nil {
		t.Fatal(err)
	}

	if err := svc.mergeRepoint(ctx, tenantID, 1, "JUNK", 100, "INBOX", 200, mid); err != nil {
		t.Fatalf("合并式收尾失败：%v", err)
	}

	// 一封信只剩一行，而且是带着历史的旧行，坐在新位置上。
	if n := countByMessageID(mid); n != 1 {
		t.Fatalf("救回一封信后库里有 %d 行——这正是用户看到的「一封信两次」", n)
	}
	var folder string
	var uid int64
	var isRead bool
	if err := pool.QueryRow(ctx,
		"SELECT folder, imap_uid, is_read FROM email_inbound WHERE id=$1", oldID).
		Scan(&folder, &uid, &isRead); err != nil {
		t.Fatalf("旧行没了——合并保错了方向，历史和引用跟着年轻副本丢了：%v", err)
	}
	if folder != "INBOX" || uid != 200 {
		t.Fatalf("旧行该坐到 INBOX/200，实际 %s/%d", folder, uid)
	}
	if !isRead {
		t.Fatal("阅读状态丢了——合并等于白保旧行")
	}
	// 年轻副本连同它的附件行一起消失，不留孤儿。
	var gone int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM email_inbound_attachments WHERE tenant_id=$1 AND inbound_id=$2",
		tenantID, dupID).Scan(&gone); err != nil {
		t.Fatal(err)
	}
	if gone != 0 {
		t.Fatalf("年轻副本的附件行成了孤儿：%d", gone)
	}

	// ---- 占位的是一封不同的信：不猜、不动、报错 ----
	otherMid := fmt.Sprintf("<other-%d@example.com>", tenantID)
	put("JUNK", 300, otherMid, "另一封")
	stranger := fmt.Sprintf("<stranger-%d@example.com>", tenantID)
	strangerID := put("INBOX", 400, stranger, "陌生信")
	if err := svc.mergeRepoint(ctx, tenantID, 1, "JUNK", 300, "INBOX", 400, otherMid); err == nil {
		t.Fatal("位置被别的信占着还不报错——那就是在猜")
	}
	var still int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM email_inbound WHERE id=$1", strangerID).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if still != 1 {
		t.Fatal("占位的陌生信被删了——合并只该清同一封信的副本")
	}

	// ---- 没人占位：普通收尾照旧 ----
	plain := fmt.Sprintf("<plain-%d@example.com>", tenantID)
	plainID := put("JUNK", 500, plain, "无人抢先")
	if err := svc.mergeRepoint(ctx, tenantID, 1, "JUNK", 500, "INBOX", 600, plain); err != nil {
		t.Fatalf("无冲突的收尾不该失败：%v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT folder, imap_uid FROM email_inbound WHERE id=$1", plainID).Scan(&folder, &uid); err != nil {
		t.Fatal(err)
	}
	if folder != "INBOX" || uid != 600 {
		t.Fatalf("普通收尾指错了位置：%s/%d", folder, uid)
	}
	_ = pgx.ErrNoRows // 让导入名副其实：上面的行为全依赖它被正确区分
}

// 宿主侧的两扇门，方向相反，合并语义必须都对。
//
// 门一：在网页端点「不是垃圾」——对账发现信离开垃圾箱、在收件箱找到它。
// 门二：在网页端「标记为垃圾」——对账发现信离开收件箱、在垃圾箱找到它。
// 两扇门都由 mergeRepoint 收尾；这里钉的是**两个方向**都保旧行、清副本，
// 而不只是 #219 修的 ERP 内那扇。
func TestWebmailMovesMergeInBothDirections(t *testing.T) {
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
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()
	put := func(folder string, uid int64, messageID string, read bool) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, folder, imap_uid, message_id, thread_key,
			 from_email, from_name, subject, body_text, is_read, sent_at)
			VALUES ($1,1,801,$2,$3,$4,'t-'||$4,'x@example.com','X','s','b',$5,now())
			RETURNING id`, tenantID, folder, uid, messageID, read).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// 门一：垃圾箱行（已读）+ 收件箱抢先落库的副本 → 合并进 INBOX。
	m1 := fmt.Sprintf("<web-rescue-%d@x>", tenantID)
	oldA := put("JUNK", 10, m1, true)
	put("INBOX", 11, m1, false)
	if err := svc.mergeRepoint(ctx, tenantID, 1, "JUNK", 10, "INBOX", 11, m1); err != nil {
		t.Fatalf("门一合并失败：%v", err)
	}

	// 门二：收件箱行（已读）+ 垃圾箱抢先落库的副本 → 合并进 JUNK。
	m2 := fmt.Sprintf("<web-spam-%d@x>", tenantID)
	oldB := put("INBOX", 20, m2, true)
	put("JUNK", 21, m2, false)
	if err := svc.mergeRepoint(ctx, tenantID, 1, "INBOX", 20, "JUNK", 21, m2); err != nil {
		t.Fatalf("门二合并失败：%v", err)
	}

	for _, tc := range []struct {
		id     int64
		folder string
		uid    int64
		mid    string
	}{{oldA, "INBOX", 11, m1}, {oldB, "JUNK", 21, m2}} {
		var folder string
		var uid int64
		var read bool
		if err := pool.QueryRow(ctx,
			"SELECT folder, imap_uid, is_read FROM email_inbound WHERE id=$1", tc.id).
			Scan(&folder, &uid, &read); err != nil {
			t.Fatalf("旧行 %d 没了：%v", tc.id, err)
		}
		if folder != tc.folder || uid != tc.uid || !read {
			t.Fatalf("旧行 %d 该带着已读状态坐到 %s/%d，实际 %s/%d read=%v",
				tc.id, tc.folder, tc.uid, folder, uid, read)
		}
		var n int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM email_inbound WHERE tenant_id=$1 AND message_id=$2",
			tenantID, tc.mid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s 合并后仍有 %d 行", tc.mid, n)
		}
	}
}
