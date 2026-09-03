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

// LinkedIn 给 text/plain 和 text/html 两个正文部件都标了 Content-ID，入库那一步
// 从前把带 Content-ID 的部件一律当附件：正文空着，附件表里多了两个
// attachment.img。恢复任务重读原件，把正文写回去、把误存成附件的两行删掉。
//
// **不带租户号**调用，和 main.go 里那次一模一样——从前这里拿着 0 去查，
// 一行都查不到，静默退出。这家公司先绑一个信箱，让 tenantsToServe 列到它。
func TestEmptyBodyRecoveryRunsForEveryTenantWithAMailbox(t *testing.T) {
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
	employeeID := tenantID%100000 + 950001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	const text = "You have 3 new messages"
	original := []byte("From: messages-noreply@linkedin.com\r\nSubject: s\r\nMessage-ID: <linkedin@x>\r\n" +
		"Content-Type: multipart/alternative; boundary=\"alt\"\r\n\r\n" +
		"--alt\r\nContent-Type: text/plain; charset=\"UTF-8\"\r\nContent-ID: <text-body>\r\n\r\n" +
		text + "\r\n" +
		"--alt\r\nContent-Type: text/html; charset=\"UTF-8\"\r\nContent-ID: <html-body>\r\n\r\n" +
		"<p>" + text + "</p>\r\n--alt--\r\n")
	files := &rawStore{objects: map[string][]byte{"raw/linkedin": original}}
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

	// 正文两栏都空着，has_attachments 却是真的：两个正文部件被当成了文件。
	var inboundID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject,
		 body_html, body_text, has_attachments)
		VALUES ($1, $2, $3, 'INBOX', 1, 'linkedin@x', 'raw/linkedin', 's', '', '', TRUE)
		RETURNING id`, tenantID, work.AccountID, employeeID).Scan(&inboundID); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ contentType, contentID string }{
		{`text/plain; charset="UTF-8"`, "text-body"},
		{`text/html; charset="UTF-8"`, "html-body"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO email_inbound_attachments
			(tenant_id, inbound_id, file_name, content_type, file_size, file_key, content_id)
			VALUES ($1, $2, 'attachment.img', $3, 23, 'att/misfiled-' || $4, $4)`,
			tenantID, inboundID, p.contentType, p.contentID); err != nil {
			t.Fatal(err)
		}
	}

	svc.RunEmptyBodyRecovery(ctx, SyncConfig{})

	var bodyHTML, bodyText string
	var hasAttachments bool
	if err := pool.QueryRow(ctx, `SELECT body_html, body_text, has_attachments FROM email_inbound
		WHERE tenant_id=$1 AND id=$2`, tenantID, inboundID).Scan(&bodyHTML, &bodyText, &hasAttachments); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bodyText, text) {
		t.Errorf("纯文本正文应该从原件里补回来：%q", bodyText)
	}
	if !strings.Contains(bodyHTML, text) {
		t.Errorf("HTML 正文应该从原件里补回来：%q", bodyHTML)
	}
	if hasAttachments {
		t.Errorf("误存的正文部件删掉之后再没有别的附件，has_attachments 应该是假")
	}
	var misfiled int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_inbound_attachments
		WHERE tenant_id=$1 AND inbound_id=$2`, tenantID, inboundID).Scan(&misfiled); err != nil {
		t.Fatal(err)
	}
	if misfiled != 0 {
		t.Errorf("误存成附件的两个正文部件应该删掉，还剩 %d 行", misfiled)
	}
}
