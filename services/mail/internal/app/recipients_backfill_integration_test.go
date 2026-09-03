package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 只会读的假对象存储：键 → 原件。
type rawStore struct {
	Files
	objects map[string][]byte
}

func (f *rawStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := f.objects[key]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// 00052 之前入库的信只留了第一个收件人。补全任务要从原件里把整段 To 头
// 读回来；原件读不到、To 头本来就空的行，补不了但也不能让任务转圈。
func TestRecipientsBackfillRestoresEveryRecipientFromTheArchivedOriginal(t *testing.T) {
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

	files := &rawStore{objects: map[string][]byte{
		"raw/group": []byte("From: client@buyer.com\r\n" +
			"To: Ana <a@co.com>, b@co.com, c@co.com\r\n" +
			"Subject: rfq\r\n\r\nhi\r\n"),
		"raw/bcc-only": []byte("From: client@buyer.com\r\n" +
			"Subject: no to header\r\n\r\nhi\r\n"),
	}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// 这家公司得有一个绑着的信箱，tenantsToServe 才会把它列进来——生产上
	// 补全任务正是这样挨家跑的。
	if _, err := svc.VerifyMailSecret(ctx, tenantID, tenantID%100000+990001, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	}); err != nil {
		t.Fatal(err)
	}

	uid := int64(0)
	insert := func(rawKey, toEmail, subject string) int64 {
		t.Helper()
		uid++
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, to_email, subject, raw_key, received_at)
			VALUES ($1, 1, 1, $2, $3, 'INBOX', $4, 'client@buyer.com', $5, $6, $7, now())
			RETURNING id`, tenantID, subject+"@mid", subject+"-thr", uid, toEmail, subject, rawKey).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	group := insert("raw/group", "a@co.com", "群发")
	bccOnly := insert("raw/bcc-only", "a@co.com", "只有密送")
	missing := insert("raw/gone", "a@co.com", "原件丢了")
	noRaw := insert("", "a@co.com", "从没存过原件")

	// 一批只取一行，而 id 最大的那几行正好是补不了的（原件丢了）。从前那道
	// 「整批没进展就停」的闸会在第一批就停下，群发那封永远补不上。
	toAllBatch, toAllInterval = 1, 0
	defer func() { toAllBatch, toAllInterval = 50, 2 * time.Second }()
	// **不带租户号**，和 main.go 里那次调用一模一样。上线那天它就是这么静默
	// 空跑的：SyncConfig 里没有租户，拿着 0 去查一行都查不到。
	svc.RunToAllBackfill(ctx, SyncConfig{})

	toAll := func(id int64) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, `SELECT to_all FROM email_inbound WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := toAll(group); got != "Ana <a@co.com>, b@co.com, c@co.com" {
		t.Errorf("群发那封应该三个人都回来：%q", got)
	}
	// To 头本来就空：写回第一个收件人，让它离开队列，而不是永远排着。
	if got := toAll(bccOnly); got != "a@co.com" {
		t.Errorf("没有 To 头的信应该退回 to_email：%q", got)
	}
	// 补不了的两行留空——任务停了而不是转圈（跑到这里本身就说明没转圈）。
	if got := toAll(missing); got != "" {
		t.Errorf("原件读不到的行不该被写：%q", got)
	}
	if got := toAll(noRaw); got != "" {
		t.Errorf("没有 raw_key 的行不在队列里：%q", got)
	}

	// 搜索文本跟着重算：搜第三个同事的地址要能搜到这封群发。
	var searchText string
	if err := pool.QueryRow(ctx, `SELECT search_text FROM email_inbound WHERE tenant_id=$1 AND id=$2`, tenantID, group).Scan(&searchText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(searchText, "c@co.com") {
		t.Errorf("search_text 没跟着补：%q", searchText)
	}

	// 读的那一侧：补过的信，收件人拆成三个人；没补的退回第一个。
	v, err := svc.GetInbound(ctx, tenantID, 1, group)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.ToParties) != 3 || v.ToParties[0].Name != "Ana" {
		t.Errorf("详情应该拆出三个收件人：%+v", v.ToParties)
	}
	v2, err := svc.GetInbound(ctx, tenantID, 1, missing)
	if err != nil {
		t.Fatal(err)
	}
	if v2.ToAll != "a@co.com" || len(v2.ToParties) != 1 {
		t.Errorf("没补上的信应该退回第一个收件人：%q %+v", v2.ToAll, v2.ToParties)
	}
}
