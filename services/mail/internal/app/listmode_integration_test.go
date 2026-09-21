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

// 单封档：一封信一行，不按会话合并。
//
// 合并档是 Gmail 的样子，单封档是 263 和 Foxmail 的样子。两条路读的是完全
// 不同的查询（一条走 mail_thread_view 的预计算，一条直接读 email_inbound），
// 所以最要钉住的是**它们对同一批信给出同一套判断**——哪些信在这个视图里、
// 总数和列表是不是同一批、开关和排序还认不认。

// listModeFixture 建一个有会话也有散信的信箱，返回服务、租户、收件人。
func listModeFixture(t *testing.T) (*Service, int64, int64, context.Context) {
	t.Helper()
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	ownerID := tenantID%100000 + 950001
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_list_prefs WHERE tenant_id=$1", tenantID)
	})
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	add := func(uid int64, thread, subject, from string, read bool, minutesAgo int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, owner_id, account_id, folder, imap_uid, message_id, thread_key,
			 subject, from_email, is_read, received_at)
			VALUES ($1,$2,1,'INBOX',$3,$4,$5,$6,$7,$8, now() - make_interval(mins => $9))`,
			tenantID, ownerID, uid, subject+"@x", thread, subject, from, read, minutesAgo); err != nil {
			t.Fatal(err)
		}
	}
	// 一条三封的会话（客户回了两次），外加两封各自独立的信。
	// 合并档看到 3 行，单封档看到 5 行。
	add(1, "t-a", "甲-3", "cust@a.com", false, 1)
	add(2, "t-a", "甲-2", "cust@a.com", true, 5)
	add(3, "t-a", "甲-1", "cust@a.com", true, 9)
	add(4, "", "乙", "b@b.com", true, 3)
	add(5, "", "丙", "c@c.com", false, 7)
	return svc, tenantID, ownerID, ctx
}

func subjectsOf(t *testing.T, p InboundPage) []string {
	t.Helper()
	out := make([]string, 0, len(p.Mails))
	for _, m := range p.Mails {
		out = append(out, m.Subject)
	}
	return out
}

// 同一批信，两档给出不同的行数——而两档的总数都得和自己的列表对得上。
func TestListModeMessageShowsEveryMail(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)

	merged, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", "", 25, ListSort{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Mails) != 3 || merged.Total != 3 {
		t.Fatalf("合并档应该 3 条会话，实际 %v（总数 %d）", subjectsOf(t, merged), merged.Total)
	}
	if merged.Mode != ListModeThread {
		t.Errorf("没设置过的人应该是合并档，实际 %q", merged.Mode)
	}

	if _, err := svc.SetListMode(ctx, tenantID, ownerID, string(ListModeMessage)); err != nil {
		t.Fatal(err)
	}
	flat, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", "", 25, ListSort{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(flat.Mails) != 5 {
		t.Fatalf("单封档应该 5 封信，实际 %v", subjectsOf(t, flat))
	}
	// 分页器数的和列表显示的必须是同一批——这一条在合并档里写错过一次
	// （数会话数、显示信的条数），单封档不能再错一遍。
	if flat.Total != 5 {
		t.Errorf("列表 5 行而总数写着 %d", flat.Total)
	}
	if flat.Mode != ListModeMessage {
		t.Errorf("回包该告诉前端这是单封档，实际 %q", flat.Mode)
	}
	// 每一行代表一封信，不是"一封里有 N 封"。徽标靠这个数决定显示不显示。
	for _, m := range flat.Mails {
		if m.ThreadCount != 1 {
			t.Errorf("%q 的 threadCount 是 %d，单封档里每行只代表一封", m.Subject, m.ThreadCount)
		}
	}
	// 最新的在最上面，和合并档同一个口径（received_at 倒序）。
	if got := subjectsOf(t, flat); got[0] != "甲-3" {
		t.Errorf("单封档第一行应该是最新那封，实际 %v", got)
	}
}

// 「只看未读」在单封档里问的是这一封读没读，不是"这条会话里还有没有没读的"。
//
// 这是两档之间**唯一一处有意不一致**的地方，所以要写下来：合并档下甲那条
// 会话整条算未读（里面有一封没读），单封档下只有没读的那一封留下。
func TestListModeMessageUnreadOnlyIsPerMail(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)
	if _, err := svc.SetListMode(ctx, tenantID, ownerID, string(ListModeMessage)); err != nil {
		t.Fatal(err)
	}
	p, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", "", 25, ListSort{}, true)
	if err != nil {
		t.Fatal(err)
	}
	got := subjectsOf(t, p)
	if len(got) != 2 {
		t.Fatalf("没读的信有两封（甲-3、丙），实际 %v", got)
	}
	if p.Total != 2 {
		t.Errorf("列表 2 行而总数写着 %d", p.Total)
	}
	for _, s := range got {
		if s == "甲-2" || s == "甲-1" {
			t.Errorf("读过的信不该出现在「只看未读」里：%v", got)
		}
	}
}

// 翻页。单封档的游标和合并档形状一样（received_at, id），第二页必须严格
// 接着第一页，不重不漏——keyset 翻页写错的样子正是某一封出现两次。
func TestListModeMessagePagesWithoutOverlap(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)
	if _, err := svc.SetListMode(ctx, tenantID, ownerID, string(ListModeMessage)); err != nil {
		t.Fatal(err)
	}
	first, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", "", 2, ListSort{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Mails) != 2 || first.NextCursor == "" {
		t.Fatalf("第一页要两行并给出下一页的位置，实际 %v cursor=%q",
			subjectsOf(t, first), first.NextCursor)
	}
	seen := map[string]bool{}
	for _, s := range subjectsOf(t, first) {
		seen[s] = true
	}
	cursor := first.NextCursor
	for range 3 {
		page, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", cursor, 2, ListSort{}, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range subjectsOf(t, page) {
			if seen[s] {
				t.Fatalf("%q 翻页时出现了两次", s)
			}
			seen[s] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("翻完应该见到全部 5 封，实际 %d 封：%v", len(seen), seen)
	}
}

// 排序栏那条路也认这个档。两条查询是同一份列表的两种读法，一条认一条不认
// 的样子是点一下「按主题排」，列表悄悄变回合并的。
func TestListModeMessageAppliesWhenSorting(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)
	if _, err := svc.SetListMode(ctx, tenantID, ownerID, string(ListModeMessage)); err != nil {
		t.Fatal(err)
	}
	p, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "", "INBOX", "", 25,
		ListSort{By: "subject", Dir: "asc"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Mails) != 5 {
		t.Fatalf("按主题排也该是 5 封，实际 %v", subjectsOf(t, p))
	}
	if p.Mode != ListModeMessage {
		t.Errorf("排序那条路也要报出档位，实际 %q", p.Mode)
	}
}

// 在这个视图里按词筛，单封档下也要按封筛。
//
// 合并档为这件事另开了一条查询（mail_thread_view 预计算不出"含这个词的
// 会话"）；单封档把条件直接加在行上，所以是同一条查询多一个参数。这条钉住
// 那个参数真的接上了——漏掉的样子是搜什么都返回整箱。
func TestListModeMessageFiltersByKeyword(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)
	if _, err := svc.SetListMode(ctx, tenantID, ownerID, string(ListModeMessage)); err != nil {
		t.Fatal(err)
	}
	p, err := svc.ListInbound(ctx, tenantID, ownerID, 1, "甲", "INBOX", "", 25, ListSort{}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := subjectsOf(t, p)
	if len(got) != 3 {
		t.Fatalf("甲那条会话有三封，按词筛该三封都在，实际 %v", got)
	}
	if p.Total != 3 {
		t.Errorf("列表 3 行而总数写着 %d", p.Total)
	}
}

// 认不出来的档位退回默认，不报错——这是个显示偏好，最坏的后果是看到一直
// 以来的样子。库里被人手工写进一个别的值时走的就是这条。
func TestListModeUnknownValueFallsBackToThread(t *testing.T) {
	if got := normalizeListMode("BANANA"); got != ListModeThread {
		t.Errorf("认不出来的档位该退回合并，实际 %q", got)
	}
	if got := normalizeListMode(""); got != ListModeThread {
		t.Errorf("空值该退回合并，实际 %q", got)
	}
	if got := normalizeListMode("MESSAGE"); got != ListModeMessage {
		t.Errorf("MESSAGE 该认出来，实际 %q", got)
	}
}

// 设置那一侧相反：认不出来必须报错。人刚点了「一封一行」，保存回来还是
// 合并的而屏幕上什么都没说，是最难查的那一种。
func TestSetListModeRejectsUnknownValue(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)
	if _, err := svc.SetListMode(ctx, tenantID, ownerID, "BANANA"); err == nil {
		t.Fatal("认不出来的档位该报错，实际收下了")
	}
	// 报错之后库里不该留下任何东西——还是默认档。
	if got := svc.ListMode(ctx, tenantID, ownerID); got != ListModeThread {
		t.Errorf("报错之后应该还是合并档，实际 %q", got)
	}
}

// 改档能改回去。ON CONFLICT 写错的样子是第二次保存悄悄没生效。
func TestSetListModeIsIdempotentAndReversible(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)
	for _, want := range []ListMode{ListModeMessage, ListModeMessage, ListModeThread, ListModeMessage} {
		got, err := svc.SetListMode(ctx, tenantID, ownerID, string(want))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("保存后回的是 %q，应该是 %q", got, want)
		}
		if got := svc.ListMode(ctx, tenantID, ownerID); got != want {
			t.Fatalf("再读出来是 %q，应该是 %q", got, want)
		}
	}
}

// 档位跟人走，不跟公司走——和签名、模板同一条口径（00069 / 00070）。
// 同一家公司里另一个人改了档，不该影响到我。
func TestListModeIsPerEmployee(t *testing.T) {
	svc, tenantID, ownerID, ctx := listModeFixture(t)
	colleague := ownerID + 1
	if _, err := svc.SetListMode(ctx, tenantID, colleague, string(ListModeMessage)); err != nil {
		t.Fatal(err)
	}
	if got := svc.ListMode(ctx, tenantID, ownerID); got != ListModeThread {
		t.Errorf("同事改了档，我这边跟着变了：%q", got)
	}
	if got := svc.ListMode(ctx, tenantID, colleague); got != ListModeMessage {
		t.Errorf("同事自己那一档没存上：%q", got)
	}
}
