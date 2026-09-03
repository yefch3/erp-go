package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 既能读原件也记下写了什么的假对象存储。
type rawArchive struct {
	rawStore
	puts map[string][]byte
}

func (f *rawArchive) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.puts[key] = b
	return nil
}

// 贴进 Gmail 编辑器的图没有文件名，只有 Content-ID；入库那一步从前直接
// 读过去丢掉了，正文里的 cid: 指着一个不存在的部件。恢复任务重读原件，
// 把这些部件存下来、补上行。
//
// **不带租户号**调用，和 main.go 里那次一模一样——从前这里拿着 0 去查，
// 一行都查不到，静默退出。这家公司先绑一个信箱，让 tenantsToServe 列到它。
func TestEmbeddedRecoveryRunsForEveryTenantWithAMailbox(t *testing.T) {
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
	employeeID := tenantID%100000 + 960001
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()

	photo := []byte("PNG bytes of a pasted photo")
	original := []byte("From: a@x\r\nSubject: s\r\nMessage-ID: <pasted@x>\r\n" +
		"Content-Type: multipart/related; boundary=\"outer\"\r\n\r\n" +
		"--outer\r\nContent-Type: text/html; charset=\"UTF-8\"\r\n\r\n" +
		"<div>photo<img src=\"cid:ii_pasted\"></div>\r\n" +
		"--outer\r\nContent-Type: image/png\r\nContent-Disposition: inline\r\n" +
		"Content-Transfer-Encoding: base64\r\nContent-ID: <ii_pasted>\r\n\r\n" +
		base64.StdEncoding.EncodeToString(photo) + "\r\n--outer--\r\n")
	files := &rawArchive{
		rawStore: rawStore{objects: map[string][]byte{"raw/pasted": original}},
		puts:     map[string][]byte{},
	}
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

	// 正文指着 cid:ii_pasted，附件表里却一行都没有。
	var inboundID int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, folder, imap_uid, message_id, raw_key, subject, body_html)
		VALUES ($1, $2, $3, 'INBOX', 1, 'pasted@x', 'raw/pasted', 's', '<div>photo<img src="cid:ii_pasted"></div>')
		RETURNING id`, tenantID, work.AccountID, employeeID).Scan(&inboundID); err != nil {
		t.Fatal(err)
	}

	svc.RunEmbeddedRecovery(ctx, SyncConfig{})

	var fileName, fileKey string
	var fileSize int64
	err = pool.QueryRow(ctx, `SELECT file_name, file_key, file_size FROM email_inbound_attachments
		WHERE tenant_id=$1 AND inbound_id=$2 AND content_id='ii_pasted'`,
		tenantID, inboundID).Scan(&fileName, &fileKey, &fileSize)
	if err != nil {
		t.Fatalf("贴进去的图应该补出一行附件：%v", err)
	}
	wantKey := fmt.Sprintf("mail/inbound/%d/%d/att/%d-image.png", tenantID, work.AccountID, inboundID)
	if fileKey != wantKey || fileName != "image.png" || fileSize != int64(len(photo)) {
		t.Errorf("补出来的行不对：%s %s %d，想要 %s image.png %d", fileName, fileKey, fileSize, wantKey, len(photo))
	}
	if got := string(files.puts[wantKey]); got != string(photo) {
		t.Errorf("对象存储里应该是原件里的那张图：%q", got)
	}
}
