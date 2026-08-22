package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// fakeCertFiles records what was asked of it so the test can assert the
// row-first-object-second choreography without a real bucket.
type fakeCertFiles struct{ removed []string }

func (f *fakeCertFiles) PresignPut(_ context.Context, key string) (string, int32, error) {
	return "https://fake-store/put/" + key, 600, nil
}
func (f *fakeCertFiles) PresignGet(_ context.Context, key string) (string, error) {
	return "https://fake-store/get/" + key, nil
}
func (f *fakeCertFiles) Remove(_ context.Context, key string) error {
	f.removed = append(f.removed, key)
	return nil
}

// The certificate file column shipped with B4 but nothing could fill it.
// What this test pins down now that it can: a key must be one this
// factory's presign could have minted, the list mints a download URL per
// read, and deleting the row retires its object — row first, object second.
func TestFactoryCertificateFile(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed certificate file test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()

	var supplierID, factoryID int64
	if err = pool.QueryRow(ctx,
		`INSERT INTO suppliers (tenant_id, code, name) VALUES ($1, 'SUP-CF', 'Cert Mill') RETURNING id`,
		tenantID).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx,
		`INSERT INTO factories (tenant_id, supplier_id, code, name_zh) VALUES ($1, $2, 'FAC-CF', '证书工厂') RETURNING id`,
		tenantID, supplierID).Scan(&factoryID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM factory_change_logs WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM factory_certificates WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM factories WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM suppliers WHERE tenant_id=$1`, tenantID)
	}()

	files := &fakeCertFiles{}
	svc := New(pool)
	svc.UseFiles(files)

	p, err := svc.PresignFactoryCertificateFile(ctx, tenantID, factoryID, "ISO9001 证书.pdf")
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.HasPrefix(p.Key, "factory-certificates/") || !strings.Contains(p.UploadURL, p.Key) {
		t.Fatalf("presign should mint a namespaced key and a URL for it: %+v", p)
	}

	// A key presigned for another factory must be refused — that is how one
	// factory's upload would quietly become another factory's credential.
	if _, err := svc.CreateFactoryCertificate(ctx, tenantID, factoryID, FactoryCertificateInput{
		Name: "ISO9001", FileKey: "factory-certificates/1/999999/deadbeef-x.pdf",
	}); err == nil || !strings.Contains(err.Error(), "MD_FACTORY_CERT_KEY_MISMATCH") {
		t.Fatalf("foreign key prefix must be refused, got %v", err)
	}

	// A certificate without a scan stays a legal record — the register is
	// useful even before anyone scans the paper.
	if _, err := svc.CreateFactoryCertificate(ctx, tenantID, factoryID, FactoryCertificateInput{
		Name: "BSCI",
	}); err != nil {
		t.Fatalf("scanless certificate must be recordable: %v", err)
	}

	cert, err := svc.CreateFactoryCertificate(ctx, tenantID, factoryID, FactoryCertificateInput{
		Name: "ISO9001", CertificateNo: "Q-2026-001", FileKey: p.Key,
	})
	if err != nil {
		t.Fatalf("create with scan: %v", err)
	}

	list, err := svc.ListFactoryCertificates(ctx, tenantID, factoryID)
	if err != nil || len(list) != 2 {
		t.Fatalf("two certificates expected: %v %d", err, len(list))
	}
	var withFile, without *FactoryCertificateView
	for i := range list {
		if list[i].Row.ID == cert.ID {
			withFile = &list[i]
		} else {
			without = &list[i]
		}
	}
	if withFile == nil || !strings.Contains(withFile.FileURL, p.Key) {
		t.Fatalf("the scanned certificate should carry a download URL: %+v", withFile)
	}
	if withFile.FileName != "ISO9001 证书.pdf" {
		t.Fatalf("display name should survive the round trip, got %q", withFile.FileName)
	}
	if without == nil || without.FileURL != "" || without.FileName != "" {
		t.Fatalf("the scanless certificate should carry no URL: %+v", without)
	}

	// Deleting the record retires its object — a row pointing at nothing is
	// a broken link, an object no row points at is a silent leak.
	if err := svc.DeleteFactoryCertificate(ctx, tenantID, factoryID, cert.ID, 77, "Tester"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(files.removed) != 1 || files.removed[0] != p.Key {
		t.Fatalf("delete should retire exactly the certificate's object, removed %v", files.removed)
	}

	// Without storage configured the presign refuses cleanly, and the plain
	// register keeps working — the feature degrades, the record does not.
	bare := New(pool)
	if _, err := bare.PresignFactoryCertificateFile(ctx, tenantID, factoryID, "x.pdf"); err == nil ||
		!strings.Contains(err.Error(), "MD_FILES_UNAVAILABLE") {
		t.Fatalf("nil files must refuse presign cleanly, got %v", err)
	}
	if _, err := bare.ListFactoryCertificates(ctx, tenantID, factoryID); err != nil {
		t.Fatalf("listing must survive nil files: %v", err)
	}
}
