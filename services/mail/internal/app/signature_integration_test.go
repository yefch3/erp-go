package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// What only a database can prove about signatures: that the owner guard on
// list, edit, delete and send actually holds, and that the one-default-per-
// owner index is moved rather than tripped over.
//
// Run with: MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail

func signatureTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	id := scratchTenant(t, ctx, pool)
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx,
			"DELETE FROM email_signatures WHERE tenant_id = $1", id); err != nil {
			t.Errorf("cleanup email_signatures: %v", err)
		}
	})
	return id
}

// Deleting was scoped to the tenant only, so any colleague holding
// mail:email:write could remove the sign-off somebody else writes under —
// and, once editing exists, rewrite it.
func TestOnePersonCannotEditOrDeleteAnothersSignature(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := signatureTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	mine := Operator{ID: 5001, Name: "李娜"}
	theirs := Operator{ID: 5002, Name: "王强"}

	id, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		Name: "李娜的签名", Content: "Best regards, 李娜", Format: FormatText,
	}, mine)
	if err != nil {
		t.Fatal(err)
	}

	// The colleague has a default of their own. Their refused edit below asks
	// for "make it my default" — the clear-then-update runs in one
	// transaction, so the refusal must roll the clear back too, or the
	// colleague walks away from a failed edit with no default at all.
	theirDefault, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		Name: "王强的签名", Content: "Regards, 王强", Format: FormatText, IsDefault: true,
	}, theirs)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.UpdateSignature(ctx, tenantID, id, SignatureInput{
		Name: "改了", Content: "王强", Format: FormatText, IsDefault: true,
	}, theirs); err == nil {
		t.Error("a colleague rewrote somebody else's personal signature")
	}
	if err := svc.DeleteSignature(ctx, tenantID, id, theirs); err == nil {
		t.Error("a colleague deleted somebody else's personal signature")
	}

	var stillDefault bool
	if err := pool.QueryRow(ctx,
		"SELECT is_default FROM email_signatures WHERE tenant_id = $1 AND id = $2",
		tenantID, theirDefault).Scan(&stillDefault); err != nil {
		t.Fatal(err)
	}
	if !stillDefault {
		t.Error("the refused edit cleared the colleague's own default on its way out")
	}

	// Still there, still saying what its owner wrote.
	var name, content string
	if err := pool.QueryRow(ctx,
		"SELECT name, content FROM email_signatures WHERE tenant_id = $1 AND id = $2",
		tenantID, id).Scan(&name, &content); err != nil {
		t.Fatal(err)
	}
	if name != "李娜的签名" || content != "Best regards, 李娜" {
		t.Fatalf("the row was changed: %q / %q", name, content)
	}

	// The owner can do both.
	if err := svc.UpdateSignature(ctx, tenantID, id, SignatureInput{
		Name: "李娜的新签名", Content: "Kind regards, 李娜", Format: FormatText,
	}, mine); err != nil {
		t.Fatalf("the owner could not edit their own: %v", err)
	}
	if err := svc.DeleteSignature(ctx, tenantID, id, mine); err != nil {
		t.Fatalf("the owner could not delete their own: %v", err)
	}
}

// 签名是各人自己的数据（2026-09-18 定的）：没有「公司统一」这一层了，
// 一条签名只出现在写它的人的清单里。
//
// 这条以前是反着写的——「公司的那块谁都能改」——把规则钉成测试，改规则
// 就得改测试，是个决定而不是漂移。现在规则改了，测试跟着翻过来。
func TestASignatureIsListedOnlyForItsOwner(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := signatureTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	mine := Operator{ID: 5001, Name: "李娜"}
	theirs := Operator{ID: 5002, Name: "王强"}

	id, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		Name: "李娜的签名", Content: "Best regards, 李娜", Format: FormatText,
	}, mine)
	if err != nil {
		t.Fatal(err)
	}

	seen := func(op Operator) bool {
		rows, err := svc.ListSignatures(ctx, tenantID, op)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ID == id {
				return true
			}
		}
		return false
	}
	if !seen(mine) {
		t.Error("the owner does not see their own signature")
	}
	if seen(theirs) {
		t.Error("a colleague sees somebody else's signature in their list")
	}
}

// 发信时按 id 取签名，也只认本人：拿着别人签名的 id 去发，等于「没有这条
// 签名」。少了这一道，清单上看不见的东西还是能靠猜 id 用上——列表只是
// 遮住，不是收回。
func TestSendingWithSomebodyElsesSignatureIsRefused(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := signatureTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	mine := Operator{ID: 5001, Name: "李娜"}
	theirs := Operator{ID: 5002, Name: "王强"}

	id, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		Name: "李娜的签名", Content: "Best regards, 李娜", Format: FormatText,
	}, mine)
	if err != nil {
		t.Fatal(err)
	}
	in := CampaignInput{Body: "报价见附件", Format: FormatText, SignatureID: id}

	body, _, _, err := svc.composeBody(ctx, tenantID, in, mine)
	if err != nil {
		t.Fatalf("the owner could not send with their own signature: %v", err)
	}
	if !strings.Contains(body, "Best regards, 李娜") {
		t.Fatalf("the owner's signature was not appended: %q", body)
	}

	if _, _, _, err := svc.composeBody(ctx, tenantID, in, theirs); err == nil {
		t.Fatal("a colleague sent a mail signed with somebody else's signature")
	}
}

// One default per owner is a partial unique index, so setting a second one is
// a constraint violation unless the first is cleared in the same breath.
func TestMakingASecondSignatureTheDefaultMovesTheFlag(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := signatureTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	op := Operator{ID: 5003}

	first, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		Name: "一", Content: "A", Format: FormatText, IsDefault: true,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		Name: "二", Content: "B", Format: FormatText,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	// Promoting the second through an edit, which is the path the button uses.
	if err := svc.UpdateSignature(ctx, tenantID, second, SignatureInput{
		Name: "二", Content: "B", Format: FormatText, IsDefault: true,
	}, op); err != nil {
		t.Fatalf("promoting by edit failed: %v", err)
	}

	var defaults []int64
	rows, err := pool.Query(ctx,
		"SELECT id FROM email_signatures WHERE tenant_id = $1 AND is_default ORDER BY id", tenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		defaults = append(defaults, id)
	}
	if len(defaults) != 1 || defaults[0] != second {
		t.Fatalf("defaults are %v, want exactly [%d] — %d should have been demoted",
			defaults, second, first)
	}
}
