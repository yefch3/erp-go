package app

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
)

// What only a database can prove about the orphan sweep: that the reference
// count counted at the moment of the question actually protects every
// surface an image can be embedded in.
//
//  1. deleting a signature withdraws the image only it used
//  2. an image a second signature still uses survives that delete
//  3. an image a SENT MAIL uses survives everything — the customer re-opens
//     that mail next year and their client fetches the logo again; breaking
//     it retroactively is the one failure this sweep must never produce
//  4. editing an image out of a signature orphans it as surely as deleting
//
// Run with: MAIL_TEST_DSN=postgres://erp_mail:...@127.0.0.1:5433/erp_mail

func imgURL(token string) string {
	return fmt.Sprintf(`<p>hi</p><img src="http://x/api/public/mail-images/%s">`, token)
}

func TestOrphanedImagesAreWithdrawnAndSharedOnesSurvive(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := scratchTenant(t, ctx, pool)
	t.Cleanup(func() {
		for _, tbl := range []string{"email_signatures", "email_templates", "email_images"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+tbl+" WHERE tenant_id = $1", tenantID); err != nil {
				t.Errorf("cleanup %s: %v", tbl, err)
			}
		}
	})
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	op := Operator{ID: 8001, Name: "李娜"}

	// Three images: one only signature A uses, one A and B share, one a sent
	// mail also uses. Tokens are hex-shaped because the extractor's pattern
	// is as strict as the real generator.
	tokens := map[string]string{
		"onlyA":  "aaaa1111aaaa1111aaaa1111aaaa1111",
		"shared": "bbbb2222bbbb2222bbbb2222bbbb2222",
		"inSent": "cccc3333cccc3333cccc3333cccc3333",
	}
	for _, tok := range tokens {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_images (tenant_id, token, file_name, file_key)
			VALUES ($1, $2::text, 'logo.png', 'mail-images/t/'||$2::text||'.png')`,
			tenantID, tok); err != nil {
			t.Fatal(err)
		}
	}

	sigA, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		OwnerType: "EMPLOYEE", Name: "A",
		Content: imgURL(tokens["onlyA"]) + imgURL(tokens["shared"]) + imgURL(tokens["inSent"]),
		Format:  FormatHTML,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	sigB, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		OwnerType: "EMPLOYEE", Name: "B",
		Content: imgURL(tokens["shared"]), Format: FormatHTML,
	}, op)
	if err != nil {
		t.Fatal(err)
	}

	// A sent mail carrying the third image. Its campaign row satisfies the
	// foreign key and nothing more.
	var campaignID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO email_campaigns (tenant_id, campaign_no, subject_tpl, body_tpl, sender_id)
		VALUES ($1, 'TEST-SWEEP-1', 's', 'b', 8001) RETURNING id`,
		tenantID).Scan(&campaignID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM email_messages WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM email_campaigns WHERE tenant_id = $1", tenantID)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_messages (tenant_id, campaign_id, message_key, to_email, subject, body)
		VALUES ($1, $2, gen_random_uuid(), 'c@example.com', 's', $3)`,
		tenantID, campaignID, imgURL(tokens["inSent"])); err != nil {
		t.Fatal(err)
	}

	status := func(tok string) string {
		var s string
		if err := pool.QueryRow(ctx,
			"SELECT status FROM email_images WHERE tenant_id = $1 AND token = $2",
			tenantID, tok).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Deleting A: its exclusive image goes, the shared one and the one a
	// sent mail holds both stay reachable.
	if err := svc.DeleteSignature(ctx, tenantID, sigA, op); err != nil {
		t.Fatal(err)
	}
	if got := status(tokens["onlyA"]); got != "WITHDRAWN" {
		t.Errorf("the image only the deleted signature used is still %s", got)
	}
	if got := status(tokens["shared"]); got != "ACTIVE" {
		t.Errorf("an image another signature uses was withdrawn: %s", got)
	}
	if got := status(tokens["inSent"]); got != "ACTIVE" {
		t.Errorf("an image a SENT MAIL uses was withdrawn: %s — every past copy of that mail just lost its logo", got)
	}

	// Editing the image out of B orphans it as surely as deleting B would.
	if err := svc.UpdateSignature(ctx, tenantID, sigB, SignatureInput{
		OwnerType: "EMPLOYEE", Name: "B", Content: "<p>no image any more</p>",
		Format: FormatHTML,
	}, op); err != nil {
		t.Fatal(err)
	}
	if got := status(tokens["shared"]); got != "WITHDRAWN" {
		t.Errorf("the image edited out of its last signature is still %s", got)
	}
}
