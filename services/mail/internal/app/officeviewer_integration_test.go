package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 在线 Office 的配置：只读、签了名、权限跟着信走。
func TestOfficePreviewConfigSignsAViewOnlyConfig(t *testing.T) {
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
	const me, colleague = int64(4101), int64(4102)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()

	files := &previewStore{objects: map[string][]byte{"att/quote": []byte("PK\x03\x04 pretend xlsx")}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "test-docs-secret"
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{},
		Office: NewOffice("https://erp.example/docs", secret)},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var mail int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at)
		VALUES ($1, 1, $2, 'q@mid', 'q-thr', 'INBOX', 4101, 'client@buyer.com', 'me@co.com', '报价', now())
		RETURNING id`, tenantID, me).Scan(&mail); err != nil {
		t.Fatal(err)
	}
	attach := func(name, key string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
			(tenant_id, inbound_id, file_name, content_type, file_size, file_key)
			VALUES ($1, $2, $3, 'application/octet-stream', 30, $4) RETURNING id`,
			tenantID, mail, name, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	xlsx := attach("2026 报价单.xlsx", "att/quote")
	zipped := attach("resources.zip", "att/quote")

	// 列表上这一档标成 office：前端靠它决定点预览去哪儿。
	got, err := svc.GetInbound(ctx, tenantID, me, mail)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got.Attachments {
		switch a.ID {
		case xlsx:
			if a.PreviewKind != PreviewOffice {
				t.Errorf("xlsx 该是 office 一档，实际 %q", a.PreviewKind)
			}
		case zipped:
			if a.PreviewKind != "" {
				t.Errorf("zip 不该有预览，实际 %q", a.PreviewKind)
			}
		}
	}

	open, err := svc.OfficePreviewConfig(ctx, tenantID, Operator{ID: me, Name: "方晨"}, mail, xlsx, "zh")
	if err != nil {
		t.Fatal(err)
	}
	if open.DocsURL != "https://erp.example/docs" {
		t.Errorf("编辑器地址不对：%q", open.DocsURL)
	}
	var cfg struct {
		DocumentType string `json:"documentType"`
		Document     struct {
			FileType    string          `json:"fileType"`
			Key         string          `json:"key"`
			Title       string          `json:"title"`
			URL         string          `json:"url"`
			Permissions map[string]bool `json:"permissions"`
		} `json:"document"`
		EditorConfig struct {
			Mode string `json:"mode"`
			Lang string `json:"lang"`
			User struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"user"`
		} `json:"editorConfig"`
	}
	if err := json.Unmarshal([]byte(open.ConfigJSON), &cfg); err != nil {
		t.Fatalf("配置不是合法 JSON：%v\n%s", err, open.ConfigJSON)
	}
	if cfg.DocumentType != "cell" || cfg.Document.FileType != "xlsx" || cfg.Document.Title != "2026 报价单.xlsx" {
		t.Errorf("文件描述不对：%+v", cfg)
	}
	if !strings.Contains(cfg.Document.URL, "att/quote") {
		t.Errorf("文件地址该是签名下载地址：%q", cfg.Document.URL)
	}
	if cfg.EditorConfig.Mode != "view" || cfg.EditorConfig.Lang != "zh-CN" {
		t.Errorf("该是只读、中文：%+v", cfg.EditorConfig)
	}
	// 看信的人写进配置：不写的话编辑器一打开先弹「输入用于协作的名称」。
	if cfg.EditorConfig.User.Name != "方晨" || cfg.EditorConfig.User.ID == "" {
		t.Errorf("用户没进配置：%+v", cfg.EditorConfig.User)
	}
	// 只读的意思：改不了、批注不了；但能下载、能打印、能复制。
	if cfg.Document.Permissions["edit"] || cfg.Document.Permissions["comment"] || cfg.Document.Permissions["review"] {
		t.Errorf("不该给编辑权限：%v", cfg.Document.Permissions)
	}
	if !cfg.Document.Permissions["download"] || !cfg.Document.Permissions["print"] || !cfg.Document.Permissions["copy"] {
		t.Errorf("下载、打印、复制该给：%v", cfg.Document.Permissions)
	}

	// 签名用的是同一把密钥，而且签的就是这份配置——改一个字段就对不上。
	claims := jwt.MapClaims{}
	tok, err := jwt.ParseWithClaims(open.Token, claims, func(*jwt.Token) (any, error) { return []byte(secret), nil })
	if err != nil || !tok.Valid {
		t.Fatalf("签名验不过：%v", err)
	}
	doc, _ := claims["document"].(map[string]any)
	if doc["key"] != cfg.Document.Key || doc["url"] != cfg.Document.URL {
		t.Errorf("签进去的和给出去的不是同一份：%v vs %+v", doc, cfg.Document)
	}
	if _, err := jwt.Parse(open.Token, func(*jwt.Token) (any, error) { return []byte("another-secret"), nil }); err == nil {
		t.Error("换一把密钥不该验得过")
	}

	// 不是自己的信：一律「不存在」，连附件存不存在都不说。
	if _, err := svc.OfficePreviewConfig(ctx, tenantID, Operator{ID: colleague}, mail, xlsx, "zh"); err == nil {
		t.Error("别人的信不该给配置")
	}
	// zip 不归在线 Office。
	if _, err := svc.OfficePreviewConfig(ctx, tenantID, Operator{ID: me}, mail, zipped, "zh"); err == nil || !strings.Contains(err.Error(), "MAIL_PREVIEW_FILE_TYPE") {
		t.Errorf("zip 该被拒：%v", err)
	}
	// 没配在线 Office：这一档整个不出现，配置也不给。
	off := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := off.OfficePreviewConfig(ctx, tenantID, Operator{ID: me}, mail, xlsx, "zh"); err == nil || !strings.Contains(err.Error(), "MAIL_OFFICE_NOT_CONFIGURED") {
		t.Errorf("没配时该说没配：%v", err)
	}
	got, err = off.GetInbound(ctx, tenantID, me, mail)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got.Attachments {
		if a.ID == xlsx && a.PreviewKind == PreviewOffice {
			t.Error("没配在线 Office 时列表上不该标 office")
		}
	}
}
