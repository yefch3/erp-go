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

// 三封信共用一份原件（老键 tenant/account/uid 的后遗症）。原件里的 Message-ID
// 说它是谁的，其余两行就得把 raw_key 放掉；读不到的原件则一行都不许留着。
//
// **不带租户号**调用，和 main.go 里那次一模一样。启动时的 SyncConfig 没有
// 租户，从前这里拿着 0 去查 `WHERE tenant_id = 0`：一行都查不到，静默退出，
// 既不报错也没日志——从多租户那天起它在生产上一直是这么空跑的。所以这家
// 公司先绑一个信箱，让 tenantsToServe 把它列进来。
func TestRawKeyCollisionRepairRunsForEveryTenantWithAMailbox(t *testing.T) {
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
	employeeID := tenantID%100000 + 970001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	files := &rawStore{objects: map[string][]byte{
		"raw/shared": []byte("Message-ID: <keeper@x>\r\nFrom: a@x\r\nSubject: s\r\n\r\nhi\r\n"),
		"raw/alone":  []byte("Message-ID: <alone@x>\r\nFrom: a@x\r\nSubject: s\r\n\r\nhi\r\n"),
	}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	work, err := svc.VerifyMailSecret(ctx, tenantID, employeeID, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}

	insert := func(folder string, uid int64, messageID, rawKey string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, raw_size, subject)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 42, 'collision')
			RETURNING id`, tenantID, work.AccountID, employeeID, folder, uid, messageID, rawKey).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// INBOX/SENT/JUNK 里 UID 都是 614 的三封信写到了同一个对象上。
	keeper := insert("INBOX", 614, "keeper@x", "raw/shared")
	loser := insert("SENT", 614, "loser@x", "raw/shared")
	other := insert("JUNK", 614, "other@x", "raw/shared")
	// 自己独占原件的信：不在修复范围里。
	alone := insert("INBOX", 615, "alone@x", "raw/alone")
	// 共用一个已经读不到的对象：谁都不是它的主人。
	goneA := insert("INBOX", 616, "gone-a@x", "raw/gone")
	goneB := insert("SENT", 616, "gone-b@x", "raw/gone")

	svc.RunRawKeyCollisionRepair(ctx, SyncConfig{})

	rawKey := func(id int64) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, `SELECT raw_key FROM email_inbound WHERE tenant_id=$1 AND id=$2`,
			tenantID, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := rawKey(keeper); got != "raw/shared" {
		t.Errorf("原件的主人应该留着 raw_key：%q", got)
	}
	for name, id := range map[string]int64{"SENT 那封": loser, "JUNK 那封": other} {
		if got := rawKey(id); got != "" {
			t.Errorf("%s不是原件的主人，raw_key 应该放掉：%q", name, got)
		}
	}
	if got := rawKey(alone); got != "raw/alone" {
		t.Errorf("独占原件的信不该被碰：%q", got)
	}
	for name, id := range map[string]int64{"gone-a": goneA, "gone-b": goneB} {
		if got := rawKey(id); got != "" {
			t.Errorf("原件读不到，%s 也不该继续认领：%q", name, got)
		}
	}
}
