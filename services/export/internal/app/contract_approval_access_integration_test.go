package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

type contractInvolvementFixture struct {
	documents map[int64][]int64
	err       error
}

func (f *contractInvolvementFixture) MyDocuments(_ context.Context, employeeID int64, bizType string) ([]int64, error) {
	if bizType != BizTypeContract {
		return nil, nil
	}
	return f.documents[employeeID], f.err
}

// Exercise both SQL list scoping and detail/file access with a customer the
// approver does not own. Approval access must never become customer ownership.
func TestContractApprovalSnapshotAccess(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"contract_attachments", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	}()
	create := func(no string) int64 {
		t.Helper()
		var id, version int64
		err := pool.QueryRow(ctx, `INSERT INTO contracts(tenant_id,contract_no,customer_id,customer_name,sales_employee_id,status,effective_at)
		VALUES($1,$2,20,'Recorded Customer',1,'EXECUTING',now()) RETURNING id`, tenant, no).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		err = pool.QueryRow(ctx, `INSERT INTO contract_versions(tenant_id,contract_id,version_no,status,currency,total_amount,created_by,fx_rate,fx_rate_at,fx_source)
		VALUES($1,$2,1,'APPROVED','USD',100,1,1,now(),'TEST') RETURNING id`, tenant, id).Scan(&version)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE contracts SET current_version_id=$2 WHERE id=$1`, id, version); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id, unrelated := create("CT-APPROVAL-READ"), create("CT-UNRELATED")
	customers := &customerAccessFixture{} // neither customer master is visible
	involvement := &contractInvolvementFixture{documents: map[int64][]int64{2: {id}}}
	svc := New(pool, Deps{Customers: customers, Involvement: involvement, Scopes: &receivableScopeStub{}})
	manager := Operator{ID: 2}
	view, err := svc.GetContractFor(ctx, tenant, id, 0, manager)
	if err != nil || view.Contract.CustomerName != "Recorded Customer" {
		t.Fatalf("approver cannot read recorded snapshot: %v", err)
	}
	rows, total, err := svc.ListContracts(ctx, tenant, "", 0, "", 1, 1, manager)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("approval list scope/pagination: %v total=%d rows=%v", err, total, rows)
	}
	if _, err = svc.ListContractFiles(ctx, tenant, id, manager); err != nil {
		t.Fatalf("approver cannot read attachment list: %v", err)
	}
	for _, check := range []struct{ tenant, id, actor int64 }{{tenant, unrelated, 2}, {tenant, id, 3}, {tenant + 1, id, 2}} {
		if _, err = svc.GetContractFor(ctx, check.tenant, check.id, 0, Operator{ID: check.actor}); err == nil {
			t.Fatalf("unrelated/cross-tenant read allowed: %+v", check)
		}
	}
	if err = svc.mustOwnContract(ctx, manager, view); err == nil {
		t.Fatal("approval involvement granted write access")
	}
	if _, err = customers.Get(ctx, 20); err == nil {
		t.Fatal("customer master access widened")
	}
	involvement.err = errors.New("approval unavailable")
	if _, err = svc.GetContractFor(ctx, tenant, id, 0, manager); err == nil {
		t.Fatal("approval service failure widened access")
	}
	involvement.err = nil
	involvement.documents = nil
	if _, err = svc.GetContractFor(ctx, tenant, id, 0, manager); err == nil {
		t.Fatal("removed involvement retained access")
	}

	// The displayed original number and system number must both find this
	// receivable, without weakening owner/tenant/filter boundaries.
	if _, err = pool.Exec(ctx, `UPDATE contracts SET external_contract_no='ORIGINAL-WEB-01' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for _, keyword := range []string{"CT-APPROVAL-READ", "original-web-01", "Recorded Customer"} {
		rows, total, err := svc.ListReceivableDue(ctx, tenant, ReceivableFilter{Keyword: keyword}, 1, 1, Operator{ID: 1})
		want := int64(1)
		if keyword == "Recorded Customer" {
			want = 2
		}
		if err != nil || total != want || len(rows) != 1 {
			t.Fatalf("receivable keyword %q total=%d err=%v", keyword, total, err)
		}
	}
	for _, actorTenant := range [][2]int64{{tenant, 2}, {tenant + 1, 1}} {
		rows, total, err := svc.ListReceivableDue(ctx, actorTenant[0], ReceivableFilter{Keyword: "ORIGINAL-WEB-01"}, 1, 100, Operator{ID: actorTenant[1]})
		if err != nil || total != 0 || len(rows) != 0 {
			t.Fatalf("receivable search crossed scope: %v %d", err, total)
		}
	}
}
