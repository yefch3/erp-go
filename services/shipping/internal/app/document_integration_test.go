package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDocumentLifecycle proves that a replacement creates a new immutable
// version and that invalidation preserves the old row for audit and download.
func TestDocumentLifecycle(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("set SHIPPING_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()/1000 + 930000000
	defer cleanupShippingTenant(ctx, pool, tenantID)
	files := documentFilesStub{size: 1024, contentType: "application/pdf"}
	svc := New(pool, files)
	op := Operator{ID: 303, Name: "Document Test"}
	schedule, err := svc.CreateSchedule(ctx, tenantID, validInput(), op, false)
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	key1 := fmt.Sprintf("shipping-documents/%d/%d/first-contract.pdf", tenantID, schedule.ID)
	first, err := svc.RegisterDocument(ctx, tenantID, schedule.ID, key1, "contract.pdf", "EXPORT_CONTRACT", "初版", 0, op)
	if err != nil || first.Version != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	key2 := fmt.Sprintf("shipping-documents/%d/%d/signed-contract.pdf", tenantID, schedule.ID)
	second, err := svc.RegisterDocument(ctx, tenantID, schedule.ID, key2, "contract-signed.pdf", "OTHER", "签回版", first.ID, op)
	if err != nil || second.Version != 2 || second.DocumentGroupKey != first.DocumentGroupKey || second.Category != first.Category {
		t.Fatalf("second=%+v err=%v", second, err)
	}

	rows, err := svc.ListDocuments(ctx, tenantID, schedule.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("documents=%d err=%v", len(rows), err)
	}
	if url, err := svc.DocumentAccess(ctx, tenantID, schedule.ID, first.ID, "DOWNLOAD", op); err != nil || url == "" {
		t.Fatalf("download url=%q err=%v", url, err)
	}
	voided, err := svc.InvalidateDocument(ctx, tenantID, schedule.ID, second.ID, "上传了错误扫描件", op)
	if err != nil || voided.Status != "VOIDED" {
		t.Fatalf("voided=%+v err=%v", voided, err)
	}
	rows, err = svc.ListDocuments(ctx, tenantID, schedule.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("invalidation removed history: documents=%d err=%v", len(rows), err)
	}
	_, changes, err := svc.GetSchedule(ctx, tenantID, schedule.ID)
	if err != nil || len(changes) != 4 {
		t.Fatalf("document audit changes=%d err=%v", len(changes), err)
	}
}
