package app

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestShippingContractReferenceIsolation(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("set EXPORT_TEST_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool, Deps{})
	tenant := time.Now().UnixNano() / 1000
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM contracts WHERE tenant_id IN ($1,$2)`, tenant, tenant+1) }()
	var first, foreign int64
	for _, r := range []struct {
		tenant int64
		no     string
		out    *int64
	}{{tenant, "CT-A", &first}, {tenant + 1, "CT-A", &foreign}} {
		if err = pool.QueryRow(ctx, `INSERT INTO contracts(tenant_id,contract_no,external_contract_no,customer_id) VALUES($1,$2,'OLD-A',1) RETURNING id`, r.tenant, r.no).Scan(r.out); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := svc.ShippingContractReferences(ctx, tenant, 0, "OLD-A", true)
	if err != nil || len(rows) != 1 || rows[0].ID != first {
		t.Fatal("exact alias", rows, err)
	}
	rows, err = svc.ShippingContractReferences(ctx, tenant, foreign, "", true)
	if err != nil || len(rows) != 0 {
		t.Fatal("foreign reference exposed", rows, err)
	}
	rows, err = svc.ShippingContractReferences(ctx, tenant, 0, "CT-", true)
	if err != nil || len(rows) != 0 {
		t.Fatal("partial exact match", rows, err)
	}
	rows, err = svc.ShippingContractReferences(ctx, tenant, 0, "CT-", false)
	if err != nil || len(rows) != 1 {
		t.Fatal("search", rows, err)
	}
}
