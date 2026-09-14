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

// issue #368 的后三条：星标优先、未读优先、已读/未读切换之后顺序还得对。
//
// 为什么非要集成测试：分档是 SQL 里排序键前面的一个字符，而那个字符要
// 看方向（desc 时星标给 '1'、asc 时给 '0'）。这条规则错了，屏幕上的样子是
// 「点了升序，星标掉到最底下」——没有任何报错，而单元测试摸不到 SQL。
//
// 每一档都从第一页翻到底（walkInbox，一页两行），所以顺序对不对和游标接不
// 接得上是同一次断言。分档是排序键的**一部分**，游标要是没带上它，第二页
// 就会从上一档的位置重新开始——那正是最容易漏掉的地方。
func TestInboxPutsStarredAndUnreadOnTop(t *testing.T) {
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
	acct := work.AccountID

	// 六封信、六条会话。四种组合（加星/没加星 × 读过/没读过）都占到，
	// 而且同一档里不止一封——只有这样才验得出「档内还按日期排」。
	base := time.Now().Add(-10 * time.Hour)
	uid := int64(0)
	add := func(subject string, starred, read bool, offset time.Duration) {
		t.Helper()
		uid++
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_name, from_email, subject, body_text, raw_size,
			 is_starred, is_read, received_at)
			VALUES ($1,$2,$3,$4,$5,'INBOX',$6,'Someone','someone@x.com',$7,'hi',100,$8,$9,$10)`,
			tenantID, acct, employeeID,
			"t"+strconv.FormatInt(uid, 10)+"@x", "thr"+strconv.FormatInt(uid, 10), uid,
			subject, starred, read, base.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
	//     主题  加星   读过    收到时间（越靠后越新）
	add("a", true, false, 1*time.Hour)
	add("b", false, true, 2*time.Hour)
	add("c", true, true, 3*time.Hour)
	add("d", false, false, 4*time.Hour)
	add("e", false, true, 5*time.Hour)
	add("f", true, false, 6*time.Hour)

	walk := func(s ListSort) []string {
		return walkInbox(t, svc, tenantID, employeeID, acct, s)
	}

	// 不开分档：一直以来的样子，新的在前。加了这个功能不能动它。
	sameOrder(t, "不分档", walk(ListSort{}),
		[]string{"f", "e", "d", "c", "b", "a"})

	// 星标优先：先是加了星的三封（新的在前），然后才是其余三封（新的在前）。
	sameOrder(t, "星标优先", walk(ListSort{StarFirst: true}),
		[]string{"f", "c", "a", "e", "d", "b"})

	// 升序时星标**还是**在最上面。分档不跟着方向翻，这是这个功能的全部
	// 意思——翻的是档**内**的顺序。
	sameOrder(t, "星标优先 + 日期升序", walk(ListSort{By: "date", Dir: "asc", StarFirst: true}),
		[]string{"a", "c", "f", "b", "d", "e"})

	// 未读优先：没读过的三封在前。
	sameOrder(t, "未读优先", walk(ListSort{UnreadFirst: true}),
		[]string{"f", "d", "a", "e", "c", "b"})

	// 两个都开：星标那一档压过未读那一档。四档依次是
	// 「加星且没读」「加星读过」「没加星没读」「都不是」。
	sameOrder(t, "星标 + 未读", walk(ListSort{StarFirst: true, UnreadFirst: true}),
		[]string{"f", "a", "c", "d", "e", "b"})

	// 分档之上还能换列：按主题升序，档内才按字母。
	sameOrder(t, "星标优先 + 按主题", walk(ListSort{By: "subject", StarFirst: true}),
		[]string{"a", "c", "f", "b", "d", "e"})

	// ---- 验收标准最后一条：已读 / 未读切换之后顺序要对 ----
	//
	// 列表刻意不在人点开一封信的当下重排（那会把光标底下的行抽走），所以
	// 这里验的是**重新拉一次之后**：d 标成已读，它就该从未读那一档掉到
	// 已读那一档里去。
	setRead := func(subject string, read bool) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			"UPDATE email_inbound SET is_read=$3 WHERE tenant_id=$1 AND subject=$2",
			tenantID, subject, read); err != nil {
			t.Fatal(err)
		}
	}
	setRead("d", true)
	sameOrder(t, "d 标已读之后", walk(ListSort{UnreadFirst: true}),
		[]string{"f", "a", "e", "d", "c", "b"})
	// 反过来也要对：把读过的 b 标回未读，它该冒到未读那一档里。
	setRead("b", false)
	sameOrder(t, "b 标回未读之后", walk(ListSort{UnreadFirst: true}),
		[]string{"f", "b", "a", "e", "d", "c"})

	// 星标那一侧同理：给 e 加星，它就进第一档。
	if _, err := pool.Exec(ctx,
		"UPDATE email_inbound SET is_starred=true WHERE tenant_id=$1 AND subject='e'",
		tenantID); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, "e 加星之后", walk(ListSort{StarFirst: true}),
		[]string{"f", "e", "c", "a", "d", "b"})
}

// 游标里带着分档，所以换了开关的游标必须被拒——不拒的话第二页会从上一种
// 顺序里的某个位置接着翻，一封都不报错地漏掉或者重复。
func TestInboxTopCursorRefusesAMismatch(t *testing.T) {
	dsn := os.Getenv("MAIL_TEST_DSN")
	if dsn == "" {
		t.Skip("MAIL_TEST_DSN not set")
	}
	// 开着星标优先发出去的游标，拿回来时开关关了：形状能解开，分档对不上。
	cur := encodeSortCursor(ListSort{By: "date", Dir: "desc", StarFirst: true}, "20260101000000000000", 7)
	if _, _, err := decodeSortCursor(cur, ListSort{By: "date", Dir: "desc"}); err == nil {
		t.Fatal("换了分档的游标必须被拒")
	}
	// 反过来也一样。
	plain := encodeSortCursor(ListSort{By: "date", Dir: "desc"}, "20260101000000000000", 7)
	if _, _, err := decodeSortCursor(plain, ListSort{By: "date", Dir: "desc", StarFirst: true}); err == nil {
		t.Fatal("原本没分档的游标不能拿到分档的列表上用")
	}
	// 对得上就要能解开，而且解出来的还是发出去的那两个值。
	key, id, err := decodeSortCursor(cur, ListSort{By: "date", Dir: "desc", StarFirst: true})
	if err != nil {
		t.Fatalf("对得上的游标应该能解开：%v", err)
	}
	if key == nil || *key != "20260101000000000000" || id != 7 {
		t.Fatalf("解出来的不是发出去的：key=%v id=%d", key, id)
	}

	// 换版本那几分钟里，人手里攥着的是旧形状的游标（s1，不带分档）。
	// 不分档时要照样能接着翻——否则换版本的样子是「正翻到第三页的人被打回
	// 第一页」。
	legacy := base64.RawURLEncoding.EncodeToString([]byte("s1:date:desc:7:20260101000000000000"))
	key, id, err = decodeSortCursor(legacy, ListSort{By: "date", Dir: "desc"})
	if err != nil {
		t.Fatalf("旧游标在不分档时要认：%v", err)
	}
	if key == nil || *key != "20260101000000000000" || id != 7 {
		t.Fatalf("旧游标解出来的不对：key=%v id=%d", key, id)
	}
	// 但旧游标按的是没有分档的顺序，开着分档就不能接着用。
	if _, _, err := decodeSortCursor(legacy, ListSort{By: "date", Dir: "desc", UnreadFirst: true}); err == nil {
		t.Fatal("旧游标不能拿到分档的列表上用")
	}
}

// 搜索那条路按时间给结果，接不了分档——和排序同一条理由，也该是同一句
// 报错，否则前端要认两种。
func TestInboxTopNotWithKeyword(t *testing.T) {
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
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	_, err = svc.ListInbound(ctx, 1, 1, 0, "coil", "INBOX", "", 20, ListSort{StarFirst: true}, false)
	if code := apierr.CodeFromError(err); code != "MAIL_SORT_NOT_WITH_KEYWORD" {
		t.Fatalf("带关键词开分档应该报 MAIL_SORT_NOT_WITH_KEYWORD，得到 %q（%v）", code, err)
	}
}
