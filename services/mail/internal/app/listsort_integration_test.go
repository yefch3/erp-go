package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

// walkInbox 从第一页翻到底，把每一页的主题接起来。页很小（两行），所以
// 排序对不对和游标接不接得上是同一次断言：错一个字都会露出来。
func walkInbox(t *testing.T, svc *Service, tenantID, employeeID, acct int64, sort ListSort) []string {
	t.Helper()
	var out []string
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		page, err := svc.ListInbound(context.Background(), tenantID, employeeID, acct, "", "INBOX", cursor, 2, sort)
		if err != nil {
			t.Fatalf("%+v: %v", sort, err)
		}
		for _, m := range page.Mails {
			out = append(out, m.Subject)
		}
		if page.NextCursor == "" {
			return out
		}
		cursor = page.NextCursor
	}
	t.Fatalf("%+v: 翻了十页还没到底——游标在原地打转", sort)
	return nil
}

func sameOrder(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	}
}

func TestInboxSortsByTheColumnClickedAndPagesWithoutRepeating(t *testing.T) {
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
	employeeID := tenantID%100000 + 930001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 五封信、五条会话，每一列都有故意安排的顺序，而且大小上有一对并列
	// （id 兜底的那条规矩就靠它验）。发件人有一封没名字，只有地址。
	base := time.Now().Add(-5 * time.Hour)
	uid := int64(0)
	add := func(fromName, fromEmail, subject string, size int64, offset time.Duration) {
		t.Helper()
		uid++
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_name, from_email, subject, body_text, raw_size, received_at)
			VALUES ($1,$2,$3,$4,$5,'INBOX',$6,$7,$8,$9,'hi',$10,$11)`,
			tenantID, work.AccountID, employeeID,
			"m"+strconv.FormatInt(uid, 10)+"@x", "thr"+strconv.FormatInt(uid, 10), uid,
			fromName, fromEmail, subject, size, base.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	add("Carlos", "carlos@acero.pe", "b-steel", 300, 1*time.Hour)
	add("alice", "alice@coil.com", "A-coil", 5000, 2*time.Hour)
	add("", "zed@x.com", "c-plate", 100, 3*time.Hour)
	add("Bob", "bob@pipe.com", "d-pipe", 5000, 4*time.Hour)
	add("alice", "alice@coil.com", "e-bar", 42, 5*time.Hour)

	acct := work.AccountID
	// 一直以来的顺序：新的在前。这条走的是靠索引的旧查询，不能因为加了排序
	// 而变样。
	sameOrder(t, "默认", walkInbox(t, svc, tenantID, employeeID, acct, ListSort{}),
		[]string{"e-bar", "d-pipe", "c-plate", "A-coil", "b-steel"})
	sameOrder(t, "日期升序", walkInbox(t, svc, tenantID, employeeID, acct, ListSort{By: "date", Dir: "asc"}),
		[]string{"b-steel", "A-coil", "c-plate", "d-pipe", "e-bar"})
	// 大小：两封 5000 并列，id 大的在前（和日期倒序同一条规矩）。
	sameOrder(t, "大小倒序", walkInbox(t, svc, tenantID, employeeID, acct, ListSort{By: "size"}),
		[]string{"d-pipe", "A-coil", "b-steel", "c-plate", "e-bar"})
	sameOrder(t, "大小升序", walkInbox(t, svc, tenantID, employeeID, acct, ListSort{By: "size", Dir: "asc"}),
		[]string{"e-bar", "c-plate", "b-steel", "A-coil", "d-pipe"})
	// 主题不分大小写：A-coil 排在 b-steel 前面，而不是因为大写 A 排到最后。
	sameOrder(t, "主题", walkInbox(t, svc, tenantID, employeeID, acct, ListSort{By: "subject"}),
		[]string{"A-coil", "b-steel", "c-plate", "d-pipe", "e-bar"})
	// 发件人按显示的那个名字排；没名字的按地址。两封 alice 并列，升序时
	// id 小的在前。
	sameOrder(t, "发件人", walkInbox(t, svc, tenantID, employeeID, acct, ListSort{By: "from"}),
		[]string{"A-coil", "e-bar", "d-pipe", "b-steel", "c-plate"})

	// 按大小排时列表要能显示大小——否则排了也看不出排了什么。
	page, err := svc.ListInbound(ctx, tenantID, employeeID, acct, "", "INBOX", "", 1, ListSort{By: "size"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Mails) != 1 || page.Mails[0].RawSize != 5000 {
		t.Fatalf("sorted rows must carry the size: %+v", page.Mails)
	}
	if page.Total != 5 {
		t.Fatalf("总数和排序无关，应该还是 5，得到 %d", page.Total)
	}

	// 搜索和排序不能一起：报错，不是悄悄按日期排。
	_, err = svc.ListInbound(ctx, tenantID, employeeID, acct, "coil", "INBOX", "", 20, ListSort{By: "size"})
	if apierr.CodeFromError(err) != "MAIL_SORT_NOT_WITH_KEYWORD" {
		t.Fatalf("keyword + sort should be refused, got %v", err)
	}
	// 拼错的列名同样报错。
	_, err = svc.ListInbound(ctx, tenantID, employeeID, acct, "", "INBOX", "", 20, ListSort{By: "sender"})
	if apierr.CodeFromError(err) != "MAIL_SORT_INVALID" {
		t.Fatalf("unknown column should be refused, got %v", err)
	}
}

func TestSentSortsAcrossBothLegsAndOldCursorsStillTurnThePage(t *testing.T) {
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
	employeeID := tenantID%100000 + 940001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-5 * time.Hour)
	// 服务器留下的三封副本，收件人和大小各有各的顺序。
	host := func(uid int32, to, subject string, size int64, offset time.Duration) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, to_email, subject, snippet, body_text, raw_size, received_at, sent_at)
			VALUES ($1,$2,$3,$4,$5,'SENT',$6,'me@qq.com',$7,$8,'…','…',$9,$10,$10)`,
			tenantID, work.AccountID, employeeID, subject+"-mid", subject+"-thr", uid,
			to, subject, size, base.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	host(1, "zoe@c.com", "to-zoe", 900, 1*time.Hour)
	host(2, "adam@a.com", "to-adam", 100, 2*time.Hour)
	host(3, "mia@b.com", "to-mia", 500, 3*time.Hour)
	// 一条 ERP 自己的投递记录，服务器没留副本：没有原件，大小按 0 算。
	if _, err := pool.Exec(ctx, `INSERT INTO email_messages
		(tenant_id, message_key, sender_id, account_id, to_email, subject, body,
		 from_email, status, sent_at)
		VALUES ($1, gen_random_uuid(), $2, $3, 'bob@x.com', 'to-bob', 'hi',
		        'me@qq.com', 'DELIVERED', $4)`,
		tenantID, employeeID, work.AccountID, base.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}

	walk := func(sort ListSort, keyword string) []string {
		t.Helper()
		var out []string
		cursor := ""
		for pages := 0; pages < 10; pages++ {
			page, err := svc.ListMailboxSent(ctx, tenantID, employeeID, work.AccountID, keyword, cursor, 2, sort)
			if err != nil {
				t.Fatalf("%+v: %v", sort, err)
			}
			for _, m := range page.Mails {
				out = append(out, m.Subject)
			}
			if page.NextCursor == "" {
				return out
			}
			cursor = page.NextCursor
		}
		t.Fatalf("%+v: 翻了十页还没到底", sort)
		return nil
	}
	sameOrder(t, "默认", walk(ListSort{}, ""),
		[]string{"to-bob", "to-mia", "to-adam", "to-zoe"})
	sameOrder(t, "收件人", walk(ListSort{By: "to"}, ""),
		[]string{"to-adam", "to-bob", "to-mia", "to-zoe"})
	// 投递记录没有大小，按大小倒序沉在最底下——而不是拿正文长度冒充一个数。
	sameOrder(t, "大小倒序", walk(ListSort{By: "size"}, ""),
		[]string{"to-zoe", "to-mia", "to-adam", "to-bob"})
	// 已发送的搜索和排序可以一起用：同一条查询。
	sameOrder(t, "搜索+排序", walk(ListSort{By: "to"}, "@"),
		[]string{"to-adam", "to-bob", "to-mia", "to-zoe"})

	// 改版之前的游标（时间:kind:id）接着翻，第二页要和新游标翻出来的一样。
	first, err := svc.ListMailboxSent(ctx, tenantID, employeeID, work.AccountID, "", "", 2, ListSort{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Mails) != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	last := first.Mails[1]
	legacy := encodeSentCursor(last.SentAt, last.Kind, last.ID)
	viaLegacy, err := svc.ListMailboxSent(ctx, tenantID, employeeID, work.AccountID, "", legacy, 2, ListSort{})
	if err != nil {
		t.Fatalf("legacy cursor refused: %v", err)
	}
	viaNew, err := svc.ListMailboxSent(ctx, tenantID, employeeID, work.AccountID, "", first.NextCursor, 2, ListSort{})
	if err != nil {
		t.Fatal(err)
	}
	subjectsOf := func(p InboundPage) []string {
		out := make([]string, 0, len(p.Mails))
		for _, m := range p.Mails {
			out = append(out, m.Subject)
		}
		return out
	}
	sameOrder(t, "旧游标翻出来的第二页", subjectsOf(viaLegacy), subjectsOf(viaNew))
	sameOrder(t, "第二页本身", subjectsOf(viaNew), []string{"to-adam", "to-zoe"})
}
