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

// 搜索按信箱。站在 Gmail 箱里搜一个词，263 箱的信不该混进来——列表明明
// 只列 Gmail 的，搜索却横跨两个箱，是「按邮箱分」从这个门绕回来了。
func TestSearchStaysInsideTheMailboxBeingRead(t *testing.T) {
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

	subjects := func(acct int64) []string {
		t.Helper()
		page, err := svc.SearchMail(ctx, tenantID, employeeID, acct, "钢卷", "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if int(page.Total) != len(page.Hits) {
			t.Fatalf("账号 %d：命中 %d 行，计数说 %d——两条 SQL 的筛选口径不一致", acct, len(page.Hits), page.Total)
		}
		out := make([]string, 0, len(page.Hits))
		for _, h := range page.Hits {
			out = append(out, h.Subject)
		}
		return out
	}
	if got := subjects(work.AccountID); len(got) != 1 || got[0] != "钢卷询价 QQ" {
		t.Fatalf("站在 QQ 箱里搜，应该只看到 QQ 的：%v", got)
	}
	if got := subjects(personal.AccountID); len(got) != 1 || got[0] != "钢卷询价 163" {
		t.Fatalf("站在 163 箱里搜，应该只看到 163 的：%v", got)
	}
	// 0 = 全部：旧令牌和一个箱都没绑的人走这条，和改动之前一样。
	if got := subjects(0); len(got) != 2 {
		t.Fatalf("不指定信箱时应该两封都在：%v", got)
	}
}
