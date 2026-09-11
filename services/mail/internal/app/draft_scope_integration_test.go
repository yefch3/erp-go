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

// 草稿箱按信箱分。
//
// 草稿行从 00047 起就记着是从哪个箱写的（写信框选的发件人，存的时候还验过
// 那个箱是不是他的），而列表一直没读它——左栏切到哪个箱，草稿箱里都是同一堆。
// 左栏改成树之后这件事藏不住了：三个箱底下各挂一个「草稿箱」，点开是同一份。
func TestDraftsBelongToTheMailboxTheyWereWrittenFrom(t *testing.T) {
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
	employeeID := tenantID%100000 + 970001
	op := Operator{ID: employeeID}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_drafts WHERE tenant_id=$1", tenantID)
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

	draft := func(acct int64, subject string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_drafts
			(tenant_id, owner_id, account_id, subject, body, body_format, kind,
			 recipients, attachments)
			VALUES ($1,$2,$3,$4,'hi','TEXT','MARKETING','[]'::jsonb,'[]'::jsonb)`,
			tenantID, employeeID, acct, subject); err != nil {
			t.Fatal(err)
		}
	}
	draft(work.AccountID, "QQ 写了一半")
	draft(personal.AccountID, "163 写了一半")
	// 00047 之前存的：不知道自己属于哪个箱。
	draft(0, "上古草稿")

	subjects := func(acct int64) []string {
		t.Helper()
		rows, err := svc.ListDrafts(ctx, tenantID, acct, op)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Subject)
		}
		return out
	}
	has := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}

	qq := subjects(work.AccountID)
	if has(qq, "163 写了一半") {
		t.Errorf("站在 QQ 箱里看到了 163 的草稿：%v", qq)
	}
	if !has(qq, "QQ 写了一半") {
		t.Errorf("QQ 自己的草稿不见了：%v", qq)
	}
	// 不知道属于哪个箱的老草稿每个箱都列——藏起来等于让人写了一半的东西
	// 凭空消失，而塞进任何一个箱都是猜的。
	if !has(qq, "上古草稿") {
		t.Errorf("account_id=0 的老草稿被藏起来了：%v", qq)
	}

	n163 := subjects(personal.AccountID)
	if has(n163, "QQ 写了一半") {
		t.Errorf("站在 163 箱里看到了 QQ 的草稿：%v", n163)
	}
	if !has(n163, "163 写了一半") || !has(n163, "上古草稿") {
		t.Errorf("163 这边少了东西：%v", n163)
	}

	// 0 = 不指定信箱：旧令牌和一个箱都没绑的人走这条，三封都在。
	if all := subjects(0); len(all) != 3 {
		t.Errorf("不指定信箱时应该三封都在：%v", all)
	}
}

// 草稿箱列表要够画出三行式的那一行：写给谁、关于什么、写了个什么开头。
//
// 单独一条，因为这三样是**分三处**凑出来的（recipients 从 jsonb 解出来、
// snippet 由 body 剥成文字、hasAttachments 数附件），任何一处漏掉都不会报错，
// 只会让列表少一行字——而少了收件人那一行，草稿箱里每一封长得一模一样。
func TestDraftListCarriesWhatTheRowNeeds(t *testing.T) {
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
	employeeID := tenantID%100000 + 980001
	op := Operator{ID: employeeID}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_drafts WHERE tenant_id=$1", tenantID)
	}()
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if _, err := svc.SaveDraft(ctx, tenantID, DraftInput{
		Subject: "报价确认", Body: "<p>您好，</p><p>附上<b>最新</b>报价。</p>", Format: "HTML",
		Recipients: []Recipient{
			{Name: "林采购", Email: "lin@buyer.com"},
			{Email: "wu@buyer.com"},
		},
		Attachments: []PendingAttachment{{FileName: "quote.xlsx", FileKey: "k/quote.xlsx"}},
	}, op); err != nil {
		t.Fatal(err)
	}
	// 三样全空的一封。列表要照样列得出来——空草稿是很正常的东西。
	if _, err := svc.SaveDraft(ctx, tenantID, DraftInput{Format: "HTML"}, op); err != nil {
		t.Fatal(err)
	}

	rows, err := svc.ListDrafts(ctx, tenantID, 0, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("应该两封草稿，实际 %d", len(rows))
	}
	var full, empty DraftSummary
	for _, r := range rows {
		if r.Subject == "报价确认" {
			full = r
		} else {
			empty = r
		}
	}

	if len(full.Recipients) != 2 {
		t.Errorf("收件人没带出来：%+v", full.Recipients)
	} else if full.Recipients[0].Name != "林采购" || full.Recipients[1].Email != "wu@buyer.com" {
		t.Errorf("收件人对不上：%+v", full.Recipients)
	}
	if full.Snippet != "您好， 附上最新报价。" {
		t.Errorf("摘要不对：%q", full.Snippet)
	}
	if !full.HasAttachments {
		t.Error("带了附件却说没有")
	}
	if full.UpdatedAt == "" {
		t.Error("保存时间是空的——列表那一列会是一片空白")
	}

	if empty.Snippet != "" || empty.HasAttachments || len(empty.Recipients) != 0 {
		t.Errorf("空草稿不该凭空长出东西：%+v", empty)
	}
	if empty.UpdatedAt == "" {
		t.Error("空草稿也有保存时间")
	}
}
