package app

import (
	"context"
	"os"
	"testing"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

func TestMailDuplicatesFollowMasterDataStatus(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	svc := New(pool)
	svc.q = store.New(tx)
	const tenant int64 = 98374152
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var customerID int64
	err = tx.QueryRow(ctx, `INSERT INTO customers(tenant_id,code,name) VALUES($1,'MAIL-CUSTOMER','Mail Company') RETURNING id`, tenant).Scan(&customerID)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO customer_contacts(tenant_id,customer_id,name,email,is_primary) VALUES($1,$2,'Primary','primary@example.com',true),($1,$2,'Buyer','buyer@example.com',false)`, tenant, customerID)
	checkCustomer := func(ctx context.Context, tenant int64, want int) {
		t.Helper()
		rows, err := svc.CheckCustomerDuplicates(ctx, tenant, "", "", " BUYER@EXAMPLE.COM ", 0)
		if err != nil || len(rows) != want {
			t.Fatalf("customer matches %v: %v (want %d)", rows, err, want)
		}
		if want == 1 {
			if rows[0].ID != customerID || rows[0].Email != "buyer@example.com" || len(rows[0].MatchFields) != 1 || rows[0].MatchFields[0] != "EMAIL" {
				t.Fatalf("non-primary email matched incorrectly: %+v", rows[0])
			}
		}
	}
	checkCustomer(ctx, tenant, 1)
	checkCustomer(ctx, tenant+1, 0)
	checkCustomer(WithCustomerAccess(ctx, 999), tenant, 0)
	exec(`UPDATE customer_contacts SET status='INACTIVE' WHERE tenant_id=$1 AND customer_id=$2 AND email='buyer@example.com'`, tenant, customerID)
	checkCustomer(ctx, tenant, 0)
	exec(`UPDATE customer_contacts SET status='ACTIVE' WHERE tenant_id=$1 AND customer_id=$2`, tenant, customerID)
	exec(`UPDATE customers SET status='INACTIVE' WHERE tenant_id=$1 AND id=$2`, tenant, customerID)
	checkCustomer(ctx, tenant, 0)
	exec(`UPDATE customers SET status='ACTIVE' WHERE tenant_id=$1 AND id=$2`, tenant, customerID)
	checkCustomer(ctx, tenant, 1)

	exec(`INSERT INTO suppliers(tenant_id,code,name,contact_email) VALUES($1,'MAIL-SUPPLIER','Mail Supplier','supplier@example.com')`, tenant)
	checkSupplier := func(ctx context.Context, tenant int64, want int) {
		t.Helper()
		rows, err := svc.CheckSupplierDuplicates(ctx, tenant, "", "", "supplier@example.com", 0)
		if err != nil || len(rows) != want {
			t.Fatalf("supplier matches %v: %v (want %d)", rows, err, want)
		}
	}
	checkSupplier(ctx, tenant, 1)
	checkSupplier(ctx, tenant+1, 0)
	checkSupplier(WithSupplierAccess(ctx, 999), tenant, 0)
	exec(`UPDATE suppliers SET status='INACTIVE' WHERE tenant_id=$1 AND code='MAIL-SUPPLIER'`, tenant)
	checkSupplier(ctx, tenant, 0)
	exec(`UPDATE suppliers SET status='ACTIVE' WHERE tenant_id=$1 AND code='MAIL-SUPPLIER'`, tenant)
	checkSupplier(ctx, tenant, 1)
}
