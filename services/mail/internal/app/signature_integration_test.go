package app

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// What only a database can prove about signatures: that the owner guard on
// edit and delete actually holds, and that the one-default-per-owner index is
// moved rather than tripped over.
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
		OwnerType: "EMPLOYEE", Name: "李娜的签名",
		Content: "Best regards, 李娜", Format: FormatText,
	}, mine)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.UpdateSignature(ctx, tenantID, id, SignatureInput{
		OwnerType: "EMPLOYEE", Name: "改了", Content: "王强", Format: FormatText,
	}, theirs); err == nil {
		t.Error("a colleague rewrote somebody else's personal signature")
	}
	if err := svc.DeleteSignature(ctx, tenantID, id, theirs); err == nil {
		t.Error("a colleague deleted somebody else's personal signature")
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
		OwnerType: "EMPLOYEE", Name: "李娜的新签名",
		Content: "Kind regards, 李娜", Format: FormatText,
	}, mine); err != nil {
		t.Fatalf("the owner could not edit their own: %v", err)
	}
	if err := svc.DeleteSignature(ctx, tenantID, id, mine); err != nil {
		t.Fatalf("the owner could not delete their own: %v", err)
	}
}

// The shared block is the company's, so anyone who may write signatures may
// edit it. That is today's rule, written down as a test so that changing it
// is a decision rather than a drift.
func TestTheSharedSignatureIsEditableByAnyone(t *testing.T) {
	pool, ctx := exportTestPool(t)
	tenantID := signatureTenant(t, ctx, pool)
	svc := New(pool, Deps{}, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	id, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		OwnerType: "TENANT", Name: "公司统一", Content: "Acme Ltd", Format: FormatText,
	}, Operator{ID: 5001})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateSignature(ctx, tenantID, id, SignatureInput{
		OwnerType: "TENANT", Name: "公司统一", Content: "Acme Industry Ltd", Format: FormatText,
	}, Operator{ID: 5002}); err != nil {
		t.Fatalf("a colleague could not edit the shared block: %v", err)
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
		OwnerType: "EMPLOYEE", Name: "一", Content: "A", Format: FormatText, IsDefault: true,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateSignature(ctx, tenantID, SignatureInput{
		OwnerType: "EMPLOYEE", Name: "二", Content: "B", Format: FormatText,
	}, op)
	if err != nil {
		t.Fatal(err)
	}
	// Promoting the second through an edit, which is the path the button uses.
	if err := svc.UpdateSignature(ctx, tenantID, second, SignatureInput{
		OwnerType: "EMPLOYEE", Name: "二", Content: "B", Format: FormatText, IsDefault: true,
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
