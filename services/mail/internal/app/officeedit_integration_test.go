package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 附件在浏览器里改完，存成新一版；**原件一个字节都不动**。
//
// 为什么是集成测试：这条路上的每一步坏起来都不报错。版本号没进缓存键——
// Document Server 端出改之前那一份；回调没验签名——谁都能往附件里塞一版；
// 版本号算错——两个人的改动互相覆盖。这些都要真的走一遍库才看得见。
func TestEditedAttachmentIsStoredAsANewVersionAndTheOriginalIsUntouched(t *testing.T) {
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
	const me, mate = int64(4201), int64(4202)
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM mail_attachment_revisions WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound_attachments WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_inbound WHERE tenant_id=$1", tenantID)
	}()

	const original = "PK\x03\x04 客户发来的原件"
	files := &previewStore{objects: map[string][]byte{"att/quote": []byte(original)}}
	box, err := NewSecretBox(base64.StdEncoding.EncodeToString(make([]byte, 32)), 1)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "test-docs-secret"
	svc := New(pool, Deps{Files: files, Secrets: box, Numbering: &seqNumbers{},
		Office: NewOffice("/docs", secret, "http://mail:9011")},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var mail int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound
		(tenant_id, account_id, owner_id, message_id, thread_key, folder, imap_uid,
		 from_email, to_email, subject, received_at)
		VALUES ($1, 1, $2, 'e@mid', 'e-thr', 'INBOX', 4201, 'client@buyer.com', 'me@co.com', '报价', now())
		RETURNING id`, tenantID, me).Scan(&mail); err != nil {
		t.Fatal(err)
	}
	var att int64
	if err := pool.QueryRow(ctx, `INSERT INTO email_inbound_attachments
		(tenant_id, inbound_id, file_name, content_type, file_size, file_key)
		VALUES ($1, $2, '报价单.xlsx',
		        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', 30, 'att/quote')
		RETURNING id`, tenantID, mail).Scan(&att); err != nil {
		t.Fatal(err)
	}

	// 一台假的 Document Server：它手里是"人改完之后的那一份"。
	var edited = "PK\x03\x04 改过第一遍"
	docs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(edited))
	}))
	defer docs.Close()

	// 它给我们发请求时带的签名。回调和取文件都用它签，密钥和配置那把是同一个。
	docsAuth := func(payload jwt.MapClaims) string {
		t.Helper()
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, payload).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return "Bearer " + s
	}

	// ---- 打开：以可编辑的方式 ----
	open, err := svc.OfficePreviewConfig(ctx, tenantID, Operator{ID: me, Name: "方晨"}, mail, att, "zh", true)
	if err != nil {
		t.Fatal(err)
	}
	if !open.Editable || open.Version != 0 {
		t.Fatalf("第一次打开该是可编辑的原件（第 0 版）：%+v", open)
	}
	cfg := parseOfficeCfg(t, open.ConfigJSON)
	if cfg.EditorConfig.Mode != "edit" || !cfg.Document.Permissions["edit"] {
		t.Fatalf("要的是可编辑：%+v", cfg)
	}
	// 工具栏上那颗"保存"：没有它，唯一的保存时机是"最后一个人关掉十来秒后"，
	// 人点了叉回到列表看不到「已改」，只会以为没存上。
	if on, _ := cfg.EditorConfig.Customization["forcesave"].(bool); !on {
		t.Error("可编辑时要给一颗保存按钮")
	}
	if !strings.HasPrefix(cfg.EditorConfig.CallbackURL, "http://mail:9011/office/save?t=") {
		t.Fatalf("回存地址不对：%q", cfg.EditorConfig.CallbackURL)
	}
	keyV0 := cfg.Document.Key

	saveTokenOf := func(cbURL string) string {
		t.Helper()
		u, err := url.Parse(cbURL)
		if err != nil {
			t.Fatal(err)
		}
		return u.Query().Get("t")
	}
	saveTok := saveTokenOf(cfg.EditorConfig.CallbackURL)

	// 回调正文：**明文那一份里的 url 不算数**，算数的是里面那个签过的 token。
	// 所以这里两份都造：plain 是它发过来的样子，signed 是同一份内容签过的。
	body := func(status int, users []string, url string) []byte {
		inner := map[string]any{"status": status, "url": url, "users": users}
		tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(inner)).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"status": status, "url": url, "users": users, "token": tok})
		return b
	}
	good := docs.URL + "/cache/out.xlsx"
	req := OfficeFileRequest{Token: saveTok, Authorization: docsAuth(jwt.MapClaims{"status": 2, "url": good})}

	// ---- 门：任何一道过不去都不许写 ----
	//
	// **最要紧的一条**：明文正文里写一个别的地址，而签过的那份写的是真地址
	// ——去取的必须是签过的那个。不然这个口就是一台"替你去容器网里任意地址
	// 取东西"的机器，取回来的还会被存成附件的新一版。
	evil, _ := json.Marshal(map[string]any{
		"status": 2, "url": "http://iam:9001/internal/secrets",
		"token": mustSign(t, jwt.MapClaims{"status": 2, "url": good}, secret),
	})
	if err := svc.SaveOfficeEdit(ctx, req, evil); err != nil {
		t.Fatalf("签过的那份是好的，该照常存：%v", err)
	}
	if n := countRevisions(t, ctx, pool, tenantID, att); n != 1 {
		t.Fatalf("该按签过的那个地址存下一版，实际 %d 条", n)
	}
	if string(files.objects[latestRevKey(t, ctx, pool, tenantID, att)]) != edited {
		t.Fatal("取的不是签过的那个地址")
	}
	// 清掉，后面那几条从零开始数。
	if _, err := pool.Exec(ctx, "DELETE FROM mail_attachment_revisions WHERE tenant_id=$1", tenantID); err != nil {
		t.Fatal(err)
	}

	// 一份没有签名的回调：谁都能发，不能认。
	plainOnly, _ := json.Marshal(map[string]any{"status": 2, "url": good})
	if err := svc.SaveOfficeEdit(ctx, OfficeFileRequest{Token: saveTok}, plainOnly); err == nil {
		t.Error("没有签名不该写得进去")
	}
	// 别人签的。
	badSigned, _ := json.Marshal(map[string]any{
		"status": 2, "url": good,
		"token": mustSign(t, jwt.MapClaims{"status": 2, "url": good}, "not-the-secret"),
	})
	if err := svc.SaveOfficeEdit(ctx, OfficeFileRequest{Token: saveTok}, badSigned); err == nil {
		t.Error("别人签的不该写得进去")
	}
	// 不是我们签的地址 token。
	if err := svc.SaveOfficeEdit(ctx, OfficeFileRequest{
		Token: "not-our-token", Authorization: req.Authorization,
	}, body(2, nil, good)); err == nil {
		t.Error("不是我们签的地址 token 不该写得进去")
	}
	// **取件的那段 token 拿来回存**：它连只读预览都会签出来，而且就摆在浏览器
	// 看得见的地方。用途对不上就得拒。
	if err := svc.SaveOfficeEdit(ctx, OfficeFileRequest{
		Token: tokenOf(t, cfg.Document.URL), Authorization: req.Authorization,
	}, body(2, nil, good)); err == nil {
		t.Error("取件的 token 不该能用在回存上")
	}
	// 开过但一个字没改（status 4）：什么都不该发生。
	if err := svc.SaveOfficeEdit(ctx, req, body(4, nil, good)); err != nil {
		t.Fatalf("没改的那一下该安静地过去：%v", err)
	}
	if n := countRevisions(t, ctx, pool, tenantID, att); n != 0 {
		t.Fatalf("上面几下都不该留下版本，实际有 %d 条", n)
	}

	// ---- 真的存一版 ----
	if err := svc.SaveOfficeEdit(ctx, req, body(2, []string{"4202"}, good)); err != nil {
		t.Fatalf("存不下：%v", err)
	}

	// 原件一个字节都不能动。
	if string(files.objects["att/quote"]) != original {
		t.Fatalf("原件被改了：%q", files.objects["att/quote"])
	}
	// 新的一版在别的对象上，内容是改完的那一份。
	var revKey string
	var revVer int32
	var revBy int64
	if err := pool.QueryRow(ctx, `SELECT version, file_key, edited_by
		FROM mail_attachment_revisions WHERE tenant_id=$1 AND attachment_id=$2
		ORDER BY version DESC LIMIT 1`, tenantID, att).Scan(&revVer, &revKey, &revBy); err != nil {
		t.Fatal(err)
	}
	if revVer != 1 {
		t.Fatalf("第一次改出来该是第 1 版，得到 %d", revVer)
	}
	if revKey == "att/quote" {
		t.Fatal("新一版不能占用原件那个对象键")
	}
	if string(files.objects[revKey]) != edited {
		t.Fatalf("存下来的不是改完那一份：%q", files.objects[revKey])
	}
	// 算在回调报的那个人头上，不是开编辑器的那个：两个人一起改，第一个人
	// 先走的话，全算在他头上就错了。
	if revBy != mate {
		t.Fatalf("这一版该算在 %d 头上，实际 %d", mate, revBy)
	}

	// ---- 再打开：拿到的是最新那一版，而且缓存键换了 ----
	open2, err := svc.OfficePreviewConfig(ctx, tenantID, Operator{ID: me}, mail, att, "zh", true)
	if err != nil {
		t.Fatal(err)
	}
	if open2.Version != 1 {
		t.Fatalf("再打开该是第 1 版，得到 %d", open2.Version)
	}
	cfg2 := parseOfficeCfg(t, open2.ConfigJSON)
	if cfg2.Document.Key == keyV0 {
		t.Fatal("换了一版还用同一个缓存键：Document Server 会把旧的那份端出来")
	}
	// 取件口递出去的也是最新那一版。
	f, err := svc.ServeOfficeFile(ctx, OfficeFileRequest{
		Token:         tokenOf(t, cfg2.Document.URL),
		Authorization: docsAuth(jwt.MapClaims{"payload": map[string]any{"url": cfg2.Document.URL}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Bytes) != edited {
		t.Fatalf("取件口该给最新那一版：%q", f.Bytes)
	}
	if f.Name != "报价单.xlsx" {
		t.Fatalf("文件名该跟着原件：%q", f.Name)
	}
	// 没有签名的取件请求要被拒：这个口递的是客户的文件。
	if _, err := svc.ServeOfficeFile(ctx, OfficeFileRequest{Token: tokenOf(t, cfg2.Document.URL)}); err == nil {
		t.Error("没有签名不该取得到文件")
	}
	// **签名得签着这一条请求**。拿一份别的东西的签名（比如交给浏览器的那份
	// 配置——它用的是同一把密钥）来敲这个口，要被拒。
	if _, err := svc.ServeOfficeFile(ctx, OfficeFileRequest{
		Token: tokenOf(t, cfg2.Document.URL), Authorization: "Bearer " + open2.Token,
	}); err == nil {
		t.Error("拿配置的签名冒充 Document Server 不该取得到文件")
	}

	// ---- 再改一遍：版本号往上走，第 1 版还在 ----
	edited = "PK\x03\x04 改过第二遍"
	req2 := OfficeFileRequest{
		Token:         saveTokenOf(cfg2.EditorConfig.CallbackURL),
		Authorization: docsAuth(jwt.MapClaims{"status": 6, "url": good}),
	}
	if err := svc.SaveOfficeEdit(ctx, req2, body(6, []string{"4201"}, good)); err != nil {
		t.Fatalf("第二次存不下：%v", err)
	}
	if n := countRevisions(t, ctx, pool, tenantID, att); n != 2 {
		t.Fatalf("该有两版，实际 %d", n)
	}
	// 上一版没被顶掉——「上一版我给的是什么价」是会被问到的。
	if string(files.objects[revKey]) != "PK\x03\x04 改过第一遍" {
		t.Fatalf("第 1 版被覆盖了：%q", files.objects[revKey])
	}
	if string(files.objects["att/quote"]) != original {
		t.Fatalf("原件被改了：%q", files.objects["att/quote"])
	}

	// ---- 附件列表要说得出「改过、第几版」 ----
	got, err := svc.GetInbound(ctx, tenantID, me, mail)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, a := range got.Attachments {
		if a.ID != att {
			continue
		}
		found = true
		if a.Revision != 2 {
			t.Errorf("列表上该写着第 2 版，实际 %d", a.Revision)
		}
		// 下载仍然给原件：客户发来的那一份是默认，改过的那一版要明着要。
		if !strings.Contains(a.DownloadURL, "att/quote") {
			t.Errorf("下载该还是原件：%q", a.DownloadURL)
		}
	}
	if !found {
		t.Fatal("附件不见了")
	}

	// ---- 信转给了别人：手里那段回存 token 立刻不算数 ----
	//
	// 那段 token 活 24 小时，而这中间信可能被转派、人可能离职被收回。查过
	// 一次就当永远查过，是长效凭据最容易出的那种错。
	if _, err := pool.Exec(ctx, "UPDATE email_inbound SET owner_id=$2 WHERE tenant_id=$1 AND id=$3",
		tenantID, mate, mail); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveOfficeEdit(ctx, req2, body(2, nil, good)); err == nil {
		t.Error("信已经不归他了，旧的回存 token 不该还能写")
	}
	if _, err := svc.ServeOfficeFile(ctx, OfficeFileRequest{
		Token:         tokenOf(t, cfg2.Document.URL),
		Authorization: docsAuth(jwt.MapClaims{"payload": map[string]any{"url": cfg2.Document.URL}}),
	}); err == nil {
		t.Error("信已经不归他了，旧的取件 token 也不该还能取")
	}
	if n := countRevisions(t, ctx, pool, tenantID, att); n != 2 {
		t.Fatalf("被拒的那几下不该留下版本，实际 %d 条", n)
	}
	if _, err := pool.Exec(ctx, "UPDATE email_inbound SET owner_id=$2 WHERE tenant_id=$1 AND id=$3",
		tenantID, me, mail); err != nil {
		t.Fatal(err)
	}

	// ---- 只读打开：不给回存地址 ----
	ro, err := svc.OfficePreviewConfig(ctx, tenantID, Operator{ID: me}, mail, att, "zh", false)
	if err != nil {
		t.Fatal(err)
	}
	if ro.Editable {
		t.Error("没要编辑就不该是可编辑的")
	}
	roCfg := parseOfficeCfg(t, ro.ConfigJSON)
	if roCfg.EditorConfig.CallbackURL != "" {
		t.Errorf("只读不该带回存地址：%q", roCfg.EditorConfig.CallbackURL)
	}
	if roCfg.Document.Permissions["edit"] {
		t.Error("只读不该给编辑权限")
	}
}

type officeCfgShape struct {
	Document struct {
		Key         string          `json:"key"`
		URL         string          `json:"url"`
		Permissions map[string]bool `json:"permissions"`
	} `json:"document"`
	EditorConfig struct {
		Mode          string         `json:"mode"`
		CallbackURL   string         `json:"callbackUrl"`
		Customization map[string]any `json:"customization"`
	} `json:"editorConfig"`
}

func parseOfficeCfg(t *testing.T, raw string) officeCfgShape {
	t.Helper()
	var c officeCfgShape
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("配置不是合法 JSON：%v\n%s", err, raw)
	}
	return c
}

func tokenOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("t")
}

func mustSign(t *testing.T, claims jwt.MapClaims, secret string) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func countRevisions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, attachmentID int64) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM mail_attachment_revisions WHERE tenant_id=$1 AND attachment_id=$2",
		tenantID, attachmentID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func latestRevKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, attachmentID int64) string {
	t.Helper()
	var k string
	if err := pool.QueryRow(ctx, `SELECT file_key FROM mail_attachment_revisions
		WHERE tenant_id=$1 AND attachment_id=$2 ORDER BY version DESC LIMIT 1`,
		tenantID, attachmentID).Scan(&k); err != nil {
		t.Fatal(err)
	}
	return k
}
