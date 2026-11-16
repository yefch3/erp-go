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
	retry, err := svc.RegisterDocument(ctx, tenantID, schedule.ID, key1, "contract.pdf", "EXPORT_CONTRACT", "初版", 0, op)
	if err != nil || retry.ID != first.ID {
		t.Fatalf("document registration retry: id=%d err=%v", retry.ID, err)
	}
	key2 := fmt.Sprintf("shipping-documents/%d/%d/signed-contract.pdf", tenantID, schedule.ID)
	second, err := svc.RegisterDocument(ctx, tenantID, schedule.ID, key2, "contract-signed.pdf", "OTHER", "签回版", first.ID, op)
	if err != nil || second.Version != 2 || second.DocumentGroupKey != first.DocumentGroupKey || second.Category != first.Category {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	retry, err = svc.RegisterDocument(ctx, tenantID, schedule.ID, key2, "contract-signed.pdf", "OTHER", "签回版", first.ID, op)
	if err != nil || retry.ID != second.ID || retry.Version != 2 {
		t.Fatalf("version retry created another version: %+v err=%v", retry, err)
	}
	if _, err = svc.RegisterDocument(ctx, tenantID, schedule.ID, key1, "contract.pdf", "EXPORT_CONTRACT", "更改原记录", 0, op); err == nil {
		t.Fatal("retry changed registered document metadata")
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

func TestConcurrentDocumentRegistrationReturnsOneVersion(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer cleanupShippingTenant(context.Background(), pool, tenant)
	svc := New(pool, documentFilesStub{size: 1024, contentType: "application/pdf"})
	op := Operator{ID: 303, Name: "并发单证验收"}
	schedule, err := svc.CreateSchedule(ctx, tenant, validInput(), op, false)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Rollback(context.Background()) }()
	if _, err = gate.Exec(ctx, "SELECT id FROM shipping_schedules WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, schedule.ID); err != nil {
		t.Fatal(err)
	}
	type result struct {
		id  int64
		err error
	}
	results := make(chan result, 2)
	key := fmt.Sprintf("shipping-documents/%d/%d/concurrent.pdf", tenant, schedule.ID)
	for range 2 {
		go func() {
			doc, e := svc.RegisterDocument(ctx, tenant, schedule.ID, key, "concurrent.pdf", "BOOKING_CONFIRMATION", "同一请求并发", 0, op)
			results <- result{doc.ID, e}
		}()
	}
	blocked := false
	for range 100 {
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '-- name: GetScheduleForUpdate%'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= 2 {
			blocked = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	first, second := <-results, <-results
	if !blocked || first.err != nil || second.err != nil || first.id == 0 || first.id != second.id {
		t.Fatalf("concurrent registration: blocked=%v first=%+v second=%+v", blocked, first, second)
	}
	docs, err := svc.ListDocuments(ctx, tenant, schedule.ID)
	if err != nil || len(docs) != 1 || docs[0].Version != 1 {
		t.Fatalf("duplicate document version: %+v err=%v", docs, err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM shipping_schedule_changes WHERE tenant_id=$1 AND schedule_id=$2 AND field_name='document'`, tenant, schedule.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate upload audit: count=%d err=%v", count, err)
	}
}
