package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestSupplierAutoCodeSkipsImportedCodes(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM suppliers WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM number_sequences WHERE tenant_id=$1", tenantID)
		_, _ = pool.Exec(ctx, "DELETE FROM number_rules WHERE tenant_id=$1", tenantID)
	}()

	for _, code := range []string{"SU-0001", "SU-0002"} {
		if _, err := pool.Exec(ctx,
			"INSERT INTO suppliers (tenant_id, code, name) VALUES ($1, $2, $3)",
			tenantID, code, "Previously imported supplier"); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.CreateSupplier(ctx, tenantID, SupplierInput{NameZh: "New supplier"})
	if err != nil {
		t.Fatalf("create after imported codes: %v", err)
	}
	if created.Code != "SU-0003" {
		t.Fatalf("automatic code = %q, want SU-0003", created.Code)
	}

	_, imported, issues, err := svc.ImportSuppliers(ctx, tenantID,
		[]SupplierImportRow{{NameZh: "Another new supplier"}}, true, 0, "")
	if err != nil || imported != 1 || len(issues) != 0 {
		t.Fatalf("import after skipped codes: imported=%d issues=%v err=%v", imported, issues, err)
	}
	var code string
	if err := pool.QueryRow(ctx,
		"SELECT code FROM suppliers WHERE tenant_id=$1 AND name=$2",
		tenantID, "Another new supplier").Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "SU-0004" {
		t.Fatalf("imported supplier code = %q, want SU-0004", code)
	}
}
