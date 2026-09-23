package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 会话视图只在会影响它的字段变了时才重算（00075）。三件事：无关的更新不碰
// 视图行；相关的更新照常刷；换了会话的信，新旧两个会话都要刷。
func TestThreadViewIgnoresUpdatesThatCannotChangeIt(t *testing.T) {
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
	const owner, account = int64(9201), int64(9202)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_thread_view WHERE tenant_id=$1", tenantID)
	}()

	insert := func(mid, thread string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, to_email, subject, received_at)
			VALUES ($1, $2, $3, $4, $5, 'INBOX', nextval('email_inbound_id_seq'), 'c@x', 'me@x', 's', now())
			RETURNING id`, tenantID, account, owner, mid, thread).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := insert("a@mid", "thr-1")
	b := insert("b@mid", "thr-1")

	// 视图行的物理身份：删掉重插的行 ctid 一定变。
	ident := func(group string) (string, bool) {
		t.Helper()
		var ctid string
		var unread bool
		if err := pool.QueryRow(ctx, `SELECT ctid::text, any_unread FROM mail_thread_view
			WHERE tenant_id=$1 AND owner_id=$2 AND account_id=$3 AND group_key=$4 AND view='INBOX'`,
			tenantID, owner, account, group).Scan(&ctid, &unread); err != nil {
			t.Fatalf("会话 %s 在收件箱视图里应当有一行：%v", group, err)
		}
		return ctid, unread
	}
	before, _ := ident("thr-1")

	// 一、和视图无关的更新：图片缓存盖章、记下服务器位置、补原件、挂客户。
	// 一条语句改两行，和真实代码里的批量更新一样。
	if _, err := pool.Exec(ctx, `UPDATE email_inbound
		SET images_cached_at = now(), host_folder = '已删除', host_uid = 77,
		    raw_key = 'k', customer_id = 5, customer_name = '某客户'
		WHERE id = ANY($1)`, []int64{a, b}); err != nil {
		t.Fatal(err)
	}
	if after, _ := ident("thr-1"); after != before {
		t.Fatalf("无关更新不该删插视图行：ctid %s → %s", before, after)
	}

	// 二、相关的更新：两封都读了，这个会话不再有未读。
	if _, err := pool.Exec(ctx, `UPDATE email_inbound SET is_read = true WHERE id = ANY($1)`, []int64{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, unread := ident("thr-1"); unread {
		t.Fatal("两封都读了，视图还说有未读——相关更新没刷到")
	}

	// 三、换会话：b 挪到 thr-2。旧会话剩一封，新会话出现一封。
	if _, err := pool.Exec(ctx, `UPDATE email_inbound SET thread_key = 'thr-2' WHERE id = $1`, b); err != nil {
		t.Fatal(err)
	}
	var oldCount, newCount int
	if err := pool.QueryRow(ctx, `SELECT
		coalesce(sum(msg_count) FILTER (WHERE group_key = 'thr-1'), 0),
		coalesce(sum(msg_count) FILTER (WHERE group_key = 'thr-2'), 0)
		FROM mail_thread_view WHERE tenant_id=$1 AND view='INBOX'`, tenantID).Scan(&oldCount, &newCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 1 || newCount != 1 {
		t.Fatalf("换会话后旧会话应剩 1 封、新会话 1 封，实际 %d / %d", oldCount, newCount)
	}
}
