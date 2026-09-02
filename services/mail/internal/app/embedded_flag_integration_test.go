package app

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 00051 那条补存量的 UPDATE，拿真实的行跑一遍。
//
// 跑的是迁移文件里的那段 SQL 本身（读文件、去掉 goose 标记），不在测试里
// 再抄一份——抄一份的话，测试和迁移各自正确、彼此不一致的情形没人拦得住。
func TestBackfillTurnsOffTheClipForSignatureLogosOnly(t *testing.T) {
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
	employeeID := tenantID%100000 + 950001
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

	uid := int64(0)
	mail := func(subject, bodyHTML string, parts ...[2]string) int64 {
		t.Helper()
		uid++
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
			(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
			 from_email, subject, body_html, has_attachments, received_at)
			VALUES ($1,$2,$3,$4,$5,'INBOX',$6,'ceo@x.com',$7,$8,TRUE,now())
			RETURNING id`,
			tenantID, work.AccountID, employeeID, subject+"@mid", subject+"-thr", uid,
			subject, bodyHTML).Scan(&id); err != nil {
			t.Fatal(err)
		}
		for _, p := range parts {
			if _, err := pool.Exec(ctx, `INSERT INTO email_inbound_attachments
				(tenant_id, inbound_id, file_name, content_type, content_id)
				VALUES ($1,$2,$3,'image/png',$4)`, tenantID, id, p[0], p[1]); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	body := `<p>Best regards,<br>CEO</p><img src="cid:logo@erp">`
	onlyLogo := mail("只有签名", body, [2]string{"logo.png", "logo@erp"})
	logoAndFile := mail("签名加合同", body, [2]string{"logo.png", "logo@erp"}, [2]string{"contract.pdf", ""})
	unreferenced := mail("标了 inline 但正文没指", "<p>hi</p>", [2]string{"scan.png", "scan@x"})
	legacy := mail("00031 之前的：content_id 空", body, [2]string{"logo.png", ""})
	nothing := mail("一个部件都没有却标着有", body)

	// 迁移文件里的 Up 段。
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "00051_has_attachments_ignores_embedded.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up := string(raw)
	if i := strings.Index(up, "-- +goose Down"); i >= 0 {
		up = up[:i]
	}
	up = strings.ReplaceAll(up, "-- +goose Up", "")
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("migration SQL failed: %v", err)
	}

	flag := func(id int64) bool {
		t.Helper()
		var v bool
		if err := pool.QueryRow(ctx, `SELECT has_attachments FROM email_inbound WHERE tenant_id=$1 AND id=$2`,
			tenantID, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	want := map[string]struct {
		id   int64
		want bool
	}{
		"只有签名 logo：关掉":               {onlyLogo, false},
		"签名加合同：留着":                  {logoAndFile, true},
		"标了 inline 但正文没指：留着":       {unreferenced, true},
		"content_id 还空着的老行：留着，等补": {legacy, true},
		"一个部件都没有：关掉":              {nothing, false},
	}
	for name, c := range want {
		if got := flag(c.id); got != c.want {
			t.Errorf("%s: has_attachments=%v, want %v", name, got, c.want)
		}
	}
	// 会话视图跟着一起改了：列表读的是它，不是 email_inbound 上那一列。
	var anyAtt bool
	if err := pool.QueryRow(ctx, `SELECT any_attachment FROM mail_thread_view
		WHERE tenant_id=$1 AND owner_id=$2 AND last_id=$3`, tenantID, employeeID, onlyLogo).Scan(&anyAtt); err != nil {
		t.Fatal(err)
	}
	if anyAtt {
		t.Error("mail_thread_view.any_attachment 还是 TRUE——列表上的回形针照样亮")
	}
}
