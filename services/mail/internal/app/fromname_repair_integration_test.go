package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 改解析之前入库的信，from_name 就是 QQ 邮箱套在引号里的那串 =?utf-8?B?…?=。
// 修复任务只解这一列，search_text 跟着重算；解不开的原样留下，不堵后面的。
func TestFromNameRepairDecodesTheEncodedWordsAlreadyStored(t *testing.T) {
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
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// 这家公司得有一个绑着的信箱，tenantsToServe 才会把它列进来。
	if _, err := svc.VerifyMailSecret(ctx, tenantID, tenantID%100000+990002, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	}); err != nil {
		t.Fatal(err)
	}

	uid := int64(0)
	insert := func(fromName, subject string) int64 {
		t.Helper()
		uid++
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, from_name, to_email, subject, body_text, search_text, received_at)
			VALUES ($1, 1, 1, $2, $3, 'INBOX', $4, '875172387@qq.com', $5, 'erptest@263.net', $6, 'hi', $7, now())
			RETURNING id`, tenantID, subject+"@mid", subject+"-thr", uid, fromName, subject,
			strings.ToLower(subject+" "+fromName+" hi")).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	qq := insert("=?utf-8?B?RnVuY3Rpb24gWWU=?=", "QQ 的 B 编码")
	newsletter := insert("=?UTF-8?Q?Your_Friends_at_Colonial_Penn?=", "群发平台的 Q 编码")
	plain := insert("Ana Gomez", "本来就好的")
	// 编码方式认不得：解不开，原样留下。它的 id 最大，排在队列最前面——
	// 一批只取一行时，它不能把后面两行堵住。
	broken := insert("=?utf-8?X?zzz?=", "解不开的")

	fromNameBatch = 1
	defer func() { fromNameBatch = 50 }()
	// 不带租户号，和 main.go 里那次调用一模一样。
	svc.RunFromNameRepair(ctx, SyncConfig{})

	row := func(id int64) (name, search string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT from_name, search_text FROM email_inbound WHERE tenant_id=$1 AND id=$2`,
			tenantID, id).Scan(&name, &search); err != nil {
			t.Fatal(err)
		}
		return name, search
	}
	if name, search := row(qq); name != "Function Ye" || !strings.Contains(strings.ToLower(search), "function ye") {
		t.Errorf("QQ 那封应该解成 Function Ye，搜索文本一起换：%q / %q", name, search)
	}
	if name, _ := row(newsletter); name != "Your Friends at Colonial Penn" {
		t.Errorf("Q 编码那封没解开：%q", name)
	}
	if name, search := row(plain); name != "Ana Gomez" || search != "本来就好的 ana gomez hi" {
		t.Errorf("本来就好的行不该被碰：%q / %q", name, search)
	}
	if name, _ := row(broken); name != "=?utf-8?X?zzz?=" {
		t.Errorf("解不开的行应该原样留下：%q", name)
	}

	// 读的那一侧：列表上的发件人是人名，不是编码串。
	v, err := svc.GetInbound(ctx, tenantID, 1, qq)
	if err != nil {
		t.Fatal(err)
	}
	if v.FromName != "Function Ye" {
		t.Errorf("详情页的发件人：%q", v.FromName)
	}
}
