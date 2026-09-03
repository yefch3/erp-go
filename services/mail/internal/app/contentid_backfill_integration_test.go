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

// 入库那一步从前丢掉 Content-ID：签名 logo 存下来了，正文里 cid: 指着它，
// 两边却接不上。补全任务重读原件，把标识写回已经存在的附件行。
//
// **不带租户号**调用，和 main.go 里那次一模一样——从前这里拿着 0 去查，
// 一行都查不到，静默退出。这家公司先绑一个信箱，让 tenantsToServe 列到它。
func TestContentIDBackfillRunsForEveryTenantWithAMailbox(t *testing.T) {
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
	employeeID := tenantID%100000 + 980001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	logo := []byte("PNG bytes of a signature logo")
	original := []byte("From: a@x\r\nSubject: s\r\nMessage-ID: <logo@x>\r\n" +
		"Content-Type: multipart/related; boundary=\"outer\"\r\n\r\n" +
		"--outer\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n\r\n" +
		"<p>Regards</p><img src=\"cid:logo@erp\">\r\n" +
		"--outer\r\nContent-Type: image/png; name=\"logo.png\"\r\n" +
		"Content-Disposition: inline; filename=\"logo.png\"\r\n" +
		"Content-Transfer-Encoding: base64\r\nContent-ID: <logo@erp>\r\n\r\n" +
		base64.StdEncoding.EncodeToString(logo) + "\r\n--outer--\r\n")
	files := &rawStore{objects: map[string][]byte{"raw/logo": original}}
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

	var inboundID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject, body_html)
		VALUES ($1, $2, $3, 'INBOX', 1, 'logo@x', 'raw/logo', 's', '<p>Regards</p><img src="cid:logo@erp">')
		RETURNING id`, tenantID, work.AccountID, employeeID).Scan(&inboundID); err != nil {
		t.Fatal(err)
	}
	// 附件行在，标识空着：正是 00031 之前入库的样子。
	var attachmentID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
		VALUES ($1, $2, 'logo.png', 'image/png', $3, 'att/logo', '')
		RETURNING id`, tenantID, inboundID, len(logo)).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}

	svc.RunContentIDBackfill(ctx, SyncConfig{})

	var contentID string
	if err := pool.QueryRow(ctx, `SELECT content_id FROM email_inbound_attachments WHERE tenant_id=$1 AND id=$2`,
		tenantID, attachmentID).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	if contentID != "logo@erp" {
		t.Errorf("附件行应该补上原件里的 Content-ID：%q", contentID)
	}
}
