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

// 「待处理」按信箱分，但**别人的信不受我的信箱影响**。
//
// 这一栏兼着两件事：一是「我从这个箱发出去、出了问题的信」，二是数据范围
// 放宽的角色能看到下属的信。拿我的 account_id 去筛下属那些信，结果是一条
// 都不剩——那不是「按邮箱分」想表达的意思，是把另一个功能顺手关掉了。
func TestAttentionIsPerMailboxButDoesNotHideColleagues(t *testing.T) {
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
	me := tenantID%100000 + 980001
	colleague := me + 1
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_binding_log WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM mail_accounts WHERE tenant_id=$1", tenantID)
	}()
	// scopes 为 nil ⇒ visibleTo 回 All，正是「范围放宽的角色」那一档。
	svc := New(pool, Deps{Secrets: box, Numbering: &seqNumbers{}},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))

	work, err := svc.VerifyMailSecret(ctx, tenantID, me, BindRequest{
		Email: "me@qq.com", Provider: "qq", Secret: "code-qq",
	})
	if err != nil {
		t.Fatal(err)
	}
	personal, err := svc.VerifyMailSecret(ctx, tenantID, me, BindRequest{
		Email: "me@163.com", Provider: "netease163", Secret: "code-163",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 同事有自己的箱。**必须是真的一个号**，不能拿 0 充数——0 会走下面
	// 「老记录」那一支，于是「别人的信不受我的信箱影响」这条断言测不到
	// 任何东西：拿掉那一支它照样绿。第一版就是这么写的。
	theirs, err := svc.VerifyMailSecret(ctx, tenantID, colleague, BindRequest{
		Email: "colleague@qq.com", Provider: "qq", Secret: "code-c",
	})
	if err != nil {
		t.Fatal(err)
	}

	stuck := func(sender, acct int64, subject string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO email_messages
			(tenant_id, message_key, sender_id, account_id, to_email, subject, body,
			 from_email, status, attention_reason, attempt_count)
			VALUES ($1, gen_random_uuid(), $2, $3, 'buyer@overseas.com', $4, 'hi',
			        'me@x.com', 'HARD_BOUNCED', 'HARD_BOUNCE', 3)`,
			tenantID, sender, acct, subject); err != nil {
			t.Fatal(err)
		}
	}
	stuck(me, work.AccountID, "QQ 发失败了")
	stuck(me, personal.AccountID, "163 发失败了")
	stuck(me, 0, "上古失败记录")     // 00047 之前入队的，不知道从哪个箱走的
	stuck(colleague, theirs.AccountID, "同事发失败了") // 别人的箱，别人的信

	subjects := func(acct int64) []string {
		t.Helper()
		rows, _, _, err := svc.ListMessages(ctx, tenantID,
			MessageQuery{AttentionOnly: true, AccountID: acct, Size: 50}, Operator{ID: me})
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
	if has(qq, "163 发失败了") {
		t.Errorf("站在 QQ 箱里看到了 163 的失败记录：%v", qq)
	}
	if !has(qq, "QQ 发失败了") {
		t.Errorf("QQ 自己的失败记录不见了：%v", qq)
	}
	// 不知道从哪个箱走的老记录每个箱都列——藏起来等于让一封需要处理的信
	// 从眼前消失，那是这一栏最不该发生的事。
	if !has(qq, "上古失败记录") {
		t.Errorf("account_id=0 的老记录被藏起来了：%v", qq)
	}
	// **别人的信不受我的信箱影响。** 这一条要是红了，说明按邮箱分把
	// 「主管看得到下属」那件事顺手关掉了。
	if !has(qq, "同事发失败了") {
		t.Errorf("同事的失败记录被我的信箱筛掉了：%v", qq)
	}

	n163 := subjects(personal.AccountID)
	if has(n163, "QQ 发失败了") {
		t.Errorf("站在 163 箱里看到了 QQ 的失败记录：%v", n163)
	}
	if !has(n163, "163 发失败了") || !has(n163, "同事发失败了") {
		t.Errorf("163 这边少了东西：%v", n163)
	}

	if all := subjects(0); len(all) != 4 {
		t.Errorf("不指定信箱时应该四条都在：%v", all)
	}
}
