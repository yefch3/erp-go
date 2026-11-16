package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// TestCustomerNamesFollowsCustomerAccess checks the batch name lookup the
// inquiry list uses: one query, the same visibility as a single GetCustomer,
// and status passed through so the caller can refuse inactive customers.
func TestCustomerNamesFollowsCustomerAccess(t *testing.T) {
	dsn := os.Getenv("MD_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_TEST_DSN not set; skipping DB-backed test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)
	stamp := time.Now().UnixNano()

	create := func(name string) int64 {
		t.Helper()
		c, _, err := svc.CreateCustomer(ctx, 1, CustomerInput{Name: fmt.Sprintf("%s %d", name, stamp)})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, "DELETE FROM customer_owners WHERE tenant_id=1 AND customer_id=$1", c.ID)
			_, _ = pool.Exec(ctx, "DELETE FROM customer_change_logs WHERE tenant_id=1 AND customer_id=$1", c.ID)
			_, _ = pool.Exec(ctx, "DELETE FROM customers WHERE tenant_id=1 AND id=$1", c.ID)
		})
		return c.ID
	}
	owned, inactive, foreign := create("Owned"), create("Inactive"), create("Foreign")
	if _, err := pool.Exec(ctx, "UPDATE customers SET short_name='Short' WHERE tenant_id=1 AND id=$1", owned); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE customers SET status='INACTIVE' WHERE tenant_id=1 AND id=$1", inactive); err != nil {
		t.Fatal(err)
	}
	employee := 900000 + stamp%100000
	for _, id := range []int64{owned, inactive} {
		if _, err := svc.CreateCustomerOwner(ctx, 1, id, CustomerOwnerInput{EmployeeID: employee, EmployeeName: "Probe", ResponsibilityCode: "SALES", OperatorID: employee}); err != nil {
			t.Fatalf("owner: %v", err)
		}
	}
	ids := []int64{owned, inactive, foreign, owned, 0, -3}

	all, err := svc.CustomerNames(WithCustomerAccess(ctx, 0), 1, ids)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]CustomerName{}
	for _, c := range all {
		got[c.ID] = c
	}
	if len(all) != 3 || got[owned].ShortName != "Short" || got[inactive].Status != "INACTIVE" || got[foreign].Status != "ACTIVE" {
		t.Fatalf("unrestricted lookup wrong: %#v", all)
	}

	mine, err := svc.CustomerNames(WithCustomerAccess(ctx, employee), 1, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 {
		t.Fatalf("an owner saw a customer that is not theirs: %#v", mine)
	}
	for _, c := range mine {
		if c.ID == foreign {
			t.Fatalf("foreign customer leaked: %#v", c)
		}
	}

	if _, err := svc.CustomerNames(ctx, 1, make([]int64, customerNamesMax+1)); err == nil {
		t.Fatal("an unbounded batch was accepted")
	}
	if none, err := svc.CustomerNames(ctx, 1, nil); err != nil || len(none) != 0 {
		t.Fatalf("empty batch: %v %#v", err, none)
	}
}
