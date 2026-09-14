package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 「只看未读」按会话筛，不按单封筛。
//
// 这条分得开两种做法：一条会话里最后一封读过、前面还有一封没读，它该不该
// 留在「只看未读」里？该——列表一行代表一条会话，而这条会话里确实还有没
// 处理的东西。any_unread 就是这个意思，这里钉住它。
//
// 顺带钉住分页器：底下的总数必须和列表显示的是同一批，否则写着「共 3 封」
// 而列表只有 1 行。
func TestUnreadOnlyFiltersByConversation(t *testing.T) {
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
	ownerID := tenantID%100000 + 940001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// received_at 各不相同，好让「哪一封代表这条会话」是确定的：mail_thread_view
	// 取的是最新那封。分钟数单独传一个参数——同一个占位符既当 bigint 又拼进
	// interval，Postgres 推不出类型。
	add := func(uid int64, thread, subject string, read bool, minutesAgo int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, owner_id, account_id, folder, imap_uid, message_id, thread_key,
			 subject, is_read, received_at)
			VALUES ($1,$2,1,'INBOX',$3,$4,$5,$6,$7, now() - make_interval(mins => $8))`,
			tenantID, ownerID, uid, subject+"@x", thread, subject, read, minutesAgo); err != nil {
			t.Fatal(err)
		}
	}
	// 甲：一条会话两封，最新那封读过、前一封没读 —— 整条算未读。
	add(1, "t-jia", "甲-新", true, 1)
	add(2, "t-jia", "甲-旧", false, 9)
	// 乙：一封，没读。
	add(3, "t-yi", "乙", false, 2)
	// 丙：一封，读过 —— 筛掉。
	add(4, "t-bing", "丙", true, 3)

	subjects := func(unreadOnly bool) ([]string, int64) {
		t.Helper()
		p, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", "", 25, ListSort{}, unreadOnly)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(p.Mails))
		for _, m := range p.Mails {
			out = append(out, m.Subject)
		}
		return out, p.Total
	}

	all, allTotal := subjects(false)
	if len(all) != 3 || allTotal != 3 {
		t.Fatalf("不筛时应该三条会话，实际 %v（总数 %d）", all, allTotal)
	}

	unread, unreadTotal := subjects(true)
	if len(unread) != 2 {
		t.Fatalf("只看未读应该两条会话，实际 %v", unread)
	}
	// 分页器数的和列表显示的必须是同一批。
	if unreadTotal != 2 {
		t.Errorf("列表 2 条而总数写着 %d", unreadTotal)
	}
	has := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}
	// 甲那条会话的代表行是最新的那一封（读过的那封），但整条会话有未读，
	// 所以它在。按单封筛的实现会漏掉它。
	if !has(unread, "甲-新") {
		t.Errorf("会话里还有没读的信，却被筛掉了：%v", unread)
	}
	if !has(unread, "乙") {
		t.Errorf("整条都没读的会话不见了：%v", unread)
	}
	if has(unread, "丙") {
		t.Errorf("读过的会话没被筛掉：%v", unread)
	}
}

// 排序那条路（点了排序栏才走）也认这个开关——两条查询是同一份列表的两种
// 读法，一个认一个不认的话，点一下「按主题排」筛选就悄悄没了。
func TestUnreadOnlyAlsoAppliesWhenSorting(t *testing.T) {
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
	ownerID := tenantID%100000 + 930001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	for i, c := range []struct {
		subject string
		read    bool
	}{{"A 读过", true}, {"B 没读", false}} {
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, owner_id, account_id, folder, imap_uid, message_id, thread_key,
			 subject, is_read, received_at)
			VALUES ($1,$2,1,'INBOX',$3,$4,$5,$6,$7, now())`,
			tenantID, ownerID, int64(i+1), c.subject+"@x", "t"+c.subject, c.subject, c.read); err != nil {
			t.Fatal(err)
		}
	}
	p, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", "", 25,
		ListSort{By: "subject", Dir: "asc"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Mails) != 1 || p.Mails[0].Subject != "B 没读" {
		got := make([]string, 0, len(p.Mails))
		for _, m := range p.Mails {
			got = append(got, m.Subject)
		}
		t.Fatalf("按主题排时「只看未读」失效了：%v", got)
	}
	if p.Total != 1 {
		t.Errorf("排序路的总数没跟着筛：%d", p.Total)
	}
}
