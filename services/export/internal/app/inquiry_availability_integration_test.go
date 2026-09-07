package app

import (
	"context"
	"fmt"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"strings"
	"testing"
	"time"
)

func TestD1WithdrawalFence(t *testing.T) {
	dsn := os.Getenv("D1_EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("run with an explicit D1 isolated DSN; not part of the default unit run")
	}
	if os.Getenv("D1_CONTAINER_NETWORK") != "erp-d1-20260906_isolated" || !strings.Contains(dsn, "@postgres:5432/erp_export?") {
		t.Fatal("D1 isolated DSN required")
	}
	ctx := context.Background()
	pool, e := pgdb.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	svc := New(pool, Deps{})
	insert := func(caseID int64, status string) int64 {
		t.Helper()
		var id int64
		e := pool.QueryRow(ctx, `INSERT INTO quotations(tenant_id,quote_no,customer_id,currency,fx_rate,fx_rate_at,fx_source,source_sourcing_case_id,status) VALUES($1,$2,1,'USD',1,now(),'D1',$3,$4) RETURNING id`, tenant, fmt.Sprintf("D1-%d", caseID), caseID, status).Scan(&id)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	id := insert(1, "DRAFT")
	if e = svc.SetInquiryAvailability(ctx, tenant, 1, false); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM quotations WHERE id=$1`, id).Scan(&n); e != nil || n != 0 {
		t.Fatal("unconfirmed quote was not deleted", e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO quotations(tenant_id,quote_no,customer_id,currency,fx_rate,fx_rate_at,fx_source,source_sourcing_case_id) VALUES($1,'blocked',1,'USD',1,now(),'D1',1)`, tenant); e == nil {
		t.Fatal("fence allowed stale creation")
	}
	if e = svc.SetInquiryAvailability(ctx, tenant, 1, true); e != nil {
		t.Fatal(e)
	}
	insert(1, "DRAFT")
	insert(2, "ACCEPTED")
	if e = svc.SetInquiryAvailability(ctx, tenant, 2, false); e == nil {
		t.Fatal("accepted quote withdrawn")
	}
	id = insert(3, "DRAFT")
	if _, e = pool.Exec(ctx, `INSERT INTO contracts(tenant_id,contract_no,quotation_id,customer_id) VALUES($1,'D1-existing', $2,1)`, tenant, id); e != nil {
		t.Fatal(e)
	}
	if e = svc.SetInquiryAvailability(ctx, tenant, 3, false); e == nil {
		t.Fatal("existing contract quote withdrawn")
	}
	// Confirmation holds the same advisory lock as withdrawal. The withdrawal
	// must observe committed ACCEPTED, even when it started before that commit.
	id = insert(4, "SENT")
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE quotations SET status='ACCEPTED' WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() { result <- svc.SetInquiryAvailability(ctx, tenant, 4, false) }()
	select {
	case e := <-result:
		t.Fatal("withdrawal failed to wait for confirmation", e)
	case <-time.After(100 * time.Millisecond):
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("concurrent accepted quote deleted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("withdrawal did not finish")
	}
	t.Logf("tenant=%d: unconfirmed removal, stale creation fence, resubmission, accepted/contract protection and concurrent confirmation passed", tenant)
}
