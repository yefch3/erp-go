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

// 搜索横跨信箱，但只跨**交给它的那些**。
//
// 这条口径掉过一次头，两半都要钉住：
//
//	跨得到 —— 一个人有 QQ 和 163 两个箱，记得客户说过"钢卷"，不记得那封信
//	          落在哪个箱。跨不过去的话，他得站到每个箱里各搜一遍，也就是
//	          让人代替搜索干活。这是从「只搜当前这个箱」改回来的原因。
//	跨不过头 —— 范围是网关按解锁令牌划的（见 unlockedAccountsFor）。退出了
//	          163 之后它那把令牌就核不过，163 也就该从结果里消失。这一半要是
//	          松了，「退出」就只是个界面动作。
func TestSearchSpansTheMailboxesItWasGiven(t *testing.T) {
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
	employeeID := tenantID%100000 + 960001
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
	personal, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@163.com", Provider: "netease163", Secret: "code-163",
	})
	if err != nil {
		t.Fatal(err)
	}

	uid := int64(0)
	add := func(acct int64, subject string) {
		t.Helper()
		uid++
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, subject, body_text, search_text, received_at)
			VALUES ($1,$2,$3,$4,$5,'INBOX',$6,'buyer@overseas.com',$7,'hi',$7,now())`,
			tenantID, acct, employeeID, subject+"@mid", subject+"-thr", uid, subject); err != nil {
			t.Fatal(err)
		}
	}
	add(work.AccountID, "钢卷询价 QQ")
	add(personal.AccountID, "钢卷询价 163")

	search := func(accts []int64) []string {
		t.Helper()
		page, err := svc.SearchMail(ctx, tenantID, employeeID, accts, "钢卷", "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if int(page.Total) != len(page.Hits) {
			t.Fatalf("范围 %v：命中 %d 行，计数说 %d——两条 SQL 的筛选口径不一致",
				accts, len(page.Hits), page.Total)
		}
		out := make([]string, 0, len(page.Hits))
		for _, h := range page.Hits {
			if h.AccountID == 0 {
				t.Fatalf("命中没说自己在哪个箱：%q——界面靠它换令牌才点得开", h.Subject)
			}
			out = append(out, h.Subject)
		}
		return out
	}

	// 两个箱一起交进去：两封都要在。这是这次改动本身。
	if got := search([]int64{work.AccountID, personal.AccountID}); len(got) != 2 {
		t.Fatalf("两个箱都开着的时候，两封信都该搜得到：%v", got)
	}
	// 只交一个：另一个箱不该漏出来。退出之后的样子就是这个。
	if got := search([]int64{work.AccountID}); len(got) != 1 || got[0] != "钢卷询价 QQ" {
		t.Fatalf("只把 QQ 交进去，163 的信不该出现：%v", got)
	}
	if got := search([]int64{personal.AccountID}); len(got) != 1 || got[0] != "钢卷询价 163" {
		t.Fatalf("只把 163 交进去，QQ 的信不该出现：%v", got)
	}
	// 空 = 不限。一个箱都没绑的人走这条，nil 和空切片都不能变成「什么都搜不到」。
	if got := search(nil); len(got) != 2 {
		t.Fatalf("不限信箱时两封都该在（nil 传下去若成了 SQL NULL，这里会是 0）：%v", got)
	}
	if got := search([]int64{}); len(got) != 2 {
		t.Fatalf("不限信箱时两封都该在：%v", got)
	}
	// 别人的箱号硬塞进来也没用：owner_id 那一条挡在前面。范围是网关按令牌
	// 划的，这里再兜一层——两道都设，谁也不替谁。
	if got := search([]int64{work.AccountID + 987654}); len(got) != 0 {
		t.Fatalf("不存在的箱号不该搜出任何东西：%v", got)
	}
}
