package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// fakeFiles is object storage that never stores: it records what was asked
// of it so the test can assert the row-first-object-second choreography.
type fakeFiles struct {
	removed []string
	stored  []string
}

func (f *fakeFiles) PresignPut(_ context.Context, key string) (string, int32, error) {
	return "https://fake-store/put/" + key, 600, nil
}
func (f *fakeFiles) PresignGet(_ context.Context, key string) (string, error) {
	return "https://fake-store/get/" + key, nil
}
func (f *fakeFiles) Put(_ context.Context, key string, data []byte, _ string) error {
	f.stored = append(f.stored, key)
	return nil
}
func (f *fakeFiles) Remove(_ context.Context, key string) error {
	f.removed = append(f.removed, key)
	return nil
}

// The scan is the paper behind the claim. What this test pins down: the key
// a browser registers must be one this invoice's presign could have minted
// (not another invoice's, not another tenant's), a re-upload replaces and
// retires the old object, and the detail read mints a fresh download URL
// without ever persisting it.
func TestSupplierInvoiceAttachment(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed attachment test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM supplier_invoices WHERE tenant_id=$1`, tenantID)
	}()

	files := &fakeFiles{}
	svc := New(pool, Deps{Files: files})
	op := Operator{ID: 77, Name: "Buyer"}

	inv, err := svc.CreateSupplierInvoice(ctx, tenantID, SupplierInvoiceInput{
		SupplierID: 9, SupplierName: "Mill", InvoiceNo: "FP-ATT-001",
		Currency: "USD", TotalAmount: "300", InvoiceDate: "2026-08-20",
	}, op)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Presign namespaces the key under this tenant and invoice.
	p, err := svc.PresignSupplierInvoiceFile(ctx, tenantID, inv.ID, "扫描件 v1.pdf", op)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.HasPrefix(p.Key, "supplier-invoices/") || !strings.Contains(p.UploadURL, p.Key) {
		t.Fatalf("presign should mint a namespaced key and a URL for it: %+v", p)
	}
	if p.Expires <= 0 {
		t.Fatalf("an upload URL that never expires is a standing write capability: %+v", p)
	}

	// A key presigned for a different invoice must be refused — that is how
	// one clerk's upload would silently become another invoice's evidence.
	if _, err := svc.AttachSupplierInvoiceFile(ctx, tenantID, inv.ID,
		"supplier-invoices/1/999999/deadbeef-other.pdf", op); err == nil ||
		!strings.Contains(err.Error(), "INV_FILE_KEY_MISMATCH") {
		t.Fatalf("foreign key prefix must be refused, got %v", err)
	}

	got, err := svc.AttachSupplierInvoiceFile(ctx, tenantID, inv.ID, p.Key, op)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if got.AttachmentKey != p.Key {
		t.Fatalf("attach should record the key, got %q", got.AttachmentKey)
	}
	if got.AttachmentName != "扫描件 v1.pdf" {
		t.Fatalf("display name should survive the round trip, got %q", got.AttachmentName)
	}
	if !strings.Contains(got.AttachmentURL, p.Key) {
		t.Fatalf("detail should mint a download URL for the key, got %q", got.AttachmentURL)
	}
	if len(files.removed) != 0 {
		t.Fatalf("first attach has nothing to retire, removed %v", files.removed)
	}

	// A blurry first scan is normal: replacing points the row at the new
	// object and retires the old one — row first, object second.
	p2, err := svc.PresignSupplierInvoiceFile(ctx, tenantID, inv.ID, "rescan.pdf", op)
	if err != nil {
		t.Fatalf("second presign: %v", err)
	}
	got, err = svc.AttachSupplierInvoiceFile(ctx, tenantID, inv.ID, p2.Key, op)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got.AttachmentKey != p2.Key {
		t.Fatalf("replace should record the new key, got %q", got.AttachmentKey)
	}
	if len(files.removed) != 1 || files.removed[0] != p.Key {
		t.Fatalf("replace should retire exactly the old object, removed %v", files.removed)
	}

	// Without storage configured the endpoints refuse cleanly instead of
	// panicking — the difference between a 409 and a crashed pod.
	bare := New(pool, Deps{})
	if _, err := bare.PresignSupplierInvoiceFile(ctx, tenantID, inv.ID, "x.pdf", op); err == nil ||
		!strings.Contains(err.Error(), "INV_FILES_UNAVAILABLE") {
		t.Fatalf("nil files must refuse presign cleanly, got %v", err)
	}
}
