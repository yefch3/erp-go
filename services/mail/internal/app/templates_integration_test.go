package app

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// What only a database can prove about the template library: that the owner
// guard on edit and delete actually holds, that the shared phrasebook is
// editable by any writer while personal templates are not, and that the
// listing shows exactly the two layers a person may use.
//
// Run with: MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail

func TestTemplateOwnerGuard(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx,
			"DELETE FROM email_templates WHERE tenant_id = $1", tenantID); err != nil {
			t.Errorf("cleanup email_templates: %v", err)
		}
	})
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	mine := Operator{ID: 6001, Name: "李娜"}
	theirs := Operator{ID: 6002, Name: "王强"}

	id, err := svc.CreateTemplate(ctx, tenantID, TemplateInput{
		OwnerType: "EMPLOYEE", Name: "报价跟进", Lang: "en",
		Subject: "Following up on our quote {{contact_first_name}}",
		Content: "Dear {{contact_name}}, ...", Format: FormatText,
	}, mine)
	if err != nil {
		t.Fatal(err)
	}

	// A colleague can neither rewrite nor remove a personal template.
	if err := svc.UpdateTemplate(ctx, tenantID, id, TemplateInput{
		OwnerType: "EMPLOYEE", Name: "改了", Content: "王强的话术", Format: FormatText,
	}, theirs); err == nil {
		t.Error("a colleague rewrote somebody else's personal template")
	}
	if err := svc.DeleteTemplate(ctx, tenantID, id, theirs); err == nil {
		t.Error("a colleague deleted somebody else's personal template")
	}

	// The owner can do both.
	if err := svc.UpdateTemplate(ctx, tenantID, id, TemplateInput{
		OwnerType: "EMPLOYEE", Name: "报价跟进 v2", Lang: "en",
		Subject: "Re: our quote", Content: "Dear {{contact_name}}, updated.",
		Format: FormatText,
	}, mine); err != nil {
		t.Fatalf("the owner could not edit their own: %v", err)
	}

	// The shared phrasebook: created and edited by anyone holding write, the
	// same policy signatures follow. One policy for both features, or the
	// difference itself becomes a support question.
	sharedID, err := svc.CreateTemplate(ctx, tenantID, TemplateInput{
		OwnerType: "TENANT", Name: "付款条款", Lang: "zh",
		Content: "我们的付款条款是……", Format: FormatText,
	}, mine)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateTemplate(ctx, tenantID, sharedID, TemplateInput{
		OwnerType: "TENANT", Name: "付款条款", Lang: "zh",
		Content: "更新后的付款条款……", Format: FormatText,
	}, theirs); err != nil {
		t.Fatalf("a writer could not maintain the shared phrasebook: %v", err)
	}

	// The listing shows exactly two layers: shared, and one's own.
	rows, err := svc.ListTemplates(ctx, tenantID, theirs)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.OwnerType == "EMPLOYEE" && r.OwnerID != theirs.ID {
			t.Errorf("the listing leaked a stranger's personal template: %q", r.Name)
		}
	}

	if err := svc.DeleteTemplate(ctx, tenantID, id, mine); err != nil {
		t.Fatalf("the owner could not delete their own: %v", err)
	}
}

func TestTemplateValidation(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx,
			"DELETE FROM email_templates WHERE tenant_id = $1", tenantID); err != nil {
			t.Errorf("cleanup email_templates: %v", err)
		}
	})
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	op := Operator{ID: 6003, Name: "测试"}

	// Name and content are required; a rich editor's "empty" (<br>) counts
	// as empty, the same rule the signature learned the hard way.
	if _, err := svc.CreateTemplate(ctx, tenantID, TemplateInput{
		OwnerType: "EMPLOYEE", Name: " ", Content: "x", Format: FormatText,
	}, op); err == nil {
		t.Error("a blank name was accepted")
	}
	if _, err := svc.CreateTemplate(ctx, tenantID, TemplateInput{
		OwnerType: "EMPLOYEE", Name: "空", Content: "<p><br></p>", Format: FormatHTML,
	}, op); err == nil {
		t.Error("markup that renders as nothing was accepted")
	}

	// An unknown language is refused rather than stored: the picker filters
	// by this value, and a row filed under "fr" would simply never be seen
	// again.
	if _, err := svc.CreateTemplate(ctx, tenantID, TemplateInput{
		OwnerType: "EMPLOYEE", Name: "法语", Lang: "fr", Content: "Bonjour", Format: FormatText,
	}, op); err == nil {
		t.Error("an unsupported language tag was accepted")
	}

	// HTML content is sanitised on the way in, because the stored value is
	// both sent to customers and rendered back into our own UI.
	id, err := svc.CreateTemplate(ctx, tenantID, TemplateInput{
		OwnerType: "EMPLOYEE", Name: "带脚本", Lang: "zh",
		Content: `<p>正文</p><script>alert(1)</script>`, Format: FormatHTML,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := pool.QueryRow(ctx,
		"SELECT content FROM email_templates WHERE tenant_id = $1 AND id = $2",
		tenantID, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(stored), "<script") {
		t.Errorf("a script tag survived sanitisation: %q", stored)
	}
}
