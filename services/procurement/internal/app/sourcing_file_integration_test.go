package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// The customer's original inquiry Excel used to live as bytes in Postgres —
// the honest choice while procurement had no file adapter, and dead weight
// the moment it did. What this test pins down: with storage wired the
// original goes to the bucket and only the key touches the database, the
// detail mints a download URL, and without storage (or on upload failure)
// the old byte path still records the case — the evidence must land
// SOMEWHERE, and an intake must never be refused because a bucket napped.
func TestSourcingCaseSourceFile(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed sourcing file test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"sourcing_case_changes", "sourcing_lines", "sourcing_cases",
			"inquiry_template_fields", "inquiry_templates"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	files := &fakeFiles{}
	svc := New(pool, Deps{Files: files})
	op := Operator{ID: 77, Name: "Buyer"}
	excel := []byte("PK\x03\x04 pretend xlsx bytes")

	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "客户询盘", SourceFileName: "询盘 2026-08.xlsx",
		SourceContentType: "application/vnd.ms-excel", SourceFileData: excel,
		Lines: []SourcingLineInput{{Product: "Coil", Quantity: "10"}},
	}, op)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(files.stored) != 1 || !strings.HasPrefix(files.stored[0], "sourcing-cases/") {
		t.Fatalf("the original should land in the bucket under its namespace, stored %v", files.stored)
	}

	// The database keeps the key and NOT the bytes.
	var key string
	var blob []byte
	if err := pool.QueryRow(ctx,
		`SELECT source_file_key, source_file_data FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`,
		tenantID, created.Head.ID).Scan(&key, &blob); err != nil {
		t.Fatal(err)
	}
	if key != files.stored[0] {
		t.Fatalf("row should record the stored key, got %q want %q", key, files.stored[0])
	}
	if len(blob) != 0 {
		t.Fatalf("bytes must not ride into Postgres once the bucket has them, got %d bytes", len(blob))
	}

	// The detail makes the evidence reachable for the first time.
	view, err := svc.GetSourcingCase(ctx, tenantID, created.Head.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.SourceFileURL, key) {
		t.Fatalf("detail should mint a download URL for the key, got %q", view.SourceFileURL)
	}

	// Without storage the old path still works: bytes into the column, no
	// key, no URL — degraded, not refused.
	bare := New(pool, Deps{})
	legacy, err := bare.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "无存储时的询盘", SourceFileName: "fallback.xlsx",
		SourceFileData: excel,
		Lines:          []SourcingLineInput{{Product: "Sheet", Quantity: "5"}},
	}, op)
	if err != nil {
		t.Fatalf("storage being absent must not block an intake: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT source_file_key, source_file_data FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`,
		tenantID, legacy.Head.ID).Scan(&key, &blob); err != nil {
		t.Fatal(err)
	}
	if key != "" || len(blob) == 0 {
		t.Fatalf("fallback should store bytes and no key, got key=%q bytes=%d", key, len(blob))
	}
	if legacy.SourceFileURL != "" {
		t.Fatalf("no key, no URL — got %q", legacy.SourceFileURL)
	}
}
