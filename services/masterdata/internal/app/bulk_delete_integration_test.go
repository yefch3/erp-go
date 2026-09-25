package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/masterref"
)

func TestBulkDeleteProtectsReferencesSnapshotsAndTenants(t *testing.T) {
	dsn := os.Getenv("MD_BULK_DELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("MD_BULK_DELETE_TEST_DSN requires an isolated migrated database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	must := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE bulk_delete_reference_fixture(tenant_id bigint NOT NULL, customer_id bigint, body jsonb)`)
	defer func() { _, _ = pool.Exec(ctx, `DROP TABLE bulk_delete_reference_fixture`) }()
	tenant := time.Now().UnixNano()
	create := func(code string, ten int64, created string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO customers(tenant_id,code,name,created_at) VALUES($1,$2,$2,$3::timestamptz) RETURNING id`, ten, code, created).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := create("BULK-READY", tenant, "2026-09-24T00:00:00Z")
	b := create("BULK-USED", tenant, "2026-09-24T12:00:00Z")
	other := create("BULK-OTHER-TENANT", tenant+1, "2026-09-24T12:00:00Z")
	_ = create("BULK-OUTSIDE", tenant, "2026-09-25T00:00:00Z")
	must(`INSERT INTO customer_contacts(tenant_id,customer_id,name) VALUES($1,$2,'Contact')`, tenant, a)
	must(`INSERT INTO bulk_delete_reference_fixture VALUES($1,$2,'{}'),($3,$4,'{}')`, tenant, b, tenant+1, a)
	svc := New(pool)
	svc.UseReferenceChecker(func(context.Context, int64, string, []masterref.Candidate) (map[int64]string, error) {
		return map[int64]string{}, nil
	})
	input := BulkDeleteInput{Entity: "CUSTOMER", StartAt: "2026-09-24T00:00:00Z", EndAt: "2026-09-25T00:00:00Z"}
	preview := func() BulkDeleteResult {
		t.Helper()
		got, err := svc.BulkDelete(ctx, tenant, 9, "Admin", input)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := preview()
	if len(got.Rows) != 2 || got.Rows[0].ID != a || got.Rows[0].BlockedReason != "" || got.Rows[1].ID != b || got.Rows[1].BlockedReason == "" {
		t.Fatalf("unexpected preview: %+v", got)
	}
	run := func(sel ...BulkDeleteSelection) (BulkDeleteResult, error) {
		in := input
		in.Execute = true
		in.Selections = sel
		return svc.BulkDelete(ctx, tenant, 9, "Admin", in)
	}
	selections := func(result BulkDeleteResult) []BulkDeleteSelection {
		out := []BulkDeleteSelection{}
		for _, r := range result.Rows {
			out = append(out, BulkDeleteSelection{ID: r.ID, Version: r.Version})
		}
		return out
	}
	if _, err := run(selections(got)...); err == nil {
		t.Fatal("referenced row accepted")
	}
	if _, err := run(BulkDeleteSelection{ID: other, Version: got.Rows[0].Version}); err == nil {
		t.Fatal("cross-tenant deletion accepted")
	}
	if _, err := run(BulkDeleteSelection{ID: a, Version: got.Rows[0].Version}, BulkDeleteSelection{ID: a, Version: got.Rows[0].Version}); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
	must(`UPDATE customers SET name='Changed after preview' WHERE id=$1`, a)
	if _, err := run(BulkDeleteSelection{ID: a, Version: got.Rows[0].Version}); err == nil {
		t.Fatal("stale preview accepted")
	}
	got = preview()
	// Quotes can store identifiers as strings inside nested JSON bodies.
	must(`INSERT INTO bulk_delete_reference_fixture(tenant_id,body) VALUES($1,jsonb_build_object('nested',jsonb_build_array(jsonb_build_object('customerId',$2::bigint::text))))`, tenant, a)
	if _, err := run(BulkDeleteSelection{ID: a, Version: got.Rows[0].Version}); err == nil {
		t.Fatal("new JSON business reference ignored")
	}
	must(`DELETE FROM bulk_delete_reference_fixture WHERE tenant_id=$1 AND customer_id IS NULL`, tenant)
	svc.UseReferenceChecker(func(context.Context, int64, string, []masterref.Candidate) (map[int64]string, error) {
		return nil, errors.New("peer unavailable")
	})
	if _, err := run(BulkDeleteSelection{ID: a, Version: got.Rows[0].Version}); err == nil {
		t.Fatal("unavailable peer permitted deletion")
	}
	svc.UseReferenceChecker(func(context.Context, int64, string, []masterref.Candidate) (map[int64]string, error) {
		return map[int64]string{}, nil
	})
	result, err := run(BulkDeleteSelection{ID: a, Version: got.Rows[0].Version})
	if err != nil || result.DeletedCount != 1 {
		t.Fatalf("delete: %+v %v", result, err)
	}
	var remaining, contacts, audits int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM customers WHERE id=ANY($1::bigint[])),(SELECT count(*) FROM customer_contacts WHERE customer_id=$2),(SELECT count(*) FROM customer_change_logs WHERE customer_id=$2 AND action='DELETE')`, []int64{a, b, other}, a).Scan(&remaining, &contacts, &audits); err != nil {
		t.Fatal(err)
	}
	if remaining != 2 || contacts != 0 || audits != 1 {
		t.Fatalf("remaining=%d contacts=%d audits=%d", remaining, contacts, audits)
	}
	if _, err := run(BulkDeleteSelection{ID: a, Version: got.Rows[0].Version}); err == nil {
		t.Fatal("replayed selection accepted")
	}
}
func TestBulkDeleteSupplierAndPort(t *testing.T) {
	dsn := os.Getenv("MD_BULK_DELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated database required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := New(pool)
	svc.UseReferenceChecker(func(context.Context, int64, string, []masterref.Candidate) (map[int64]string, error) {
		return map[int64]string{}, nil
	})
	tenant := time.Now().UnixNano()
	for _, item := range []struct{ entity, sql string }{{"SUPPLIER", `INSERT INTO suppliers(tenant_id,code,name,created_at) VALUES($1,'DELETE-SUP','Unused supplier','2026-09-24T12:00:00Z')`}, {"PORT", `INSERT INTO ports(tenant_id,unlocode,name_en,country_code,timezone,created_at) VALUES($1,'CNBLK','Unused port','CN','Asia/Shanghai','2026-09-24T12:00:00Z')`}} {
		if _, err := pool.Exec(ctx, item.sql, tenant); err != nil {
			t.Fatal(err)
		}
		in := BulkDeleteInput{Entity: item.entity, StartAt: "2026-09-24T00:00:00Z", EndAt: "2026-09-25T00:00:00Z"}
		got, err := svc.BulkDelete(ctx, tenant, 9, "Admin", in)
		if err != nil || len(got.Rows) != 1 || got.Rows[0].BlockedReason != "" {
			t.Fatalf("%s preview %+v %v", item.entity, got, err)
		}
		in.Execute = true
		in.Selections = []BulkDeleteSelection{{ID: got.Rows[0].ID, Version: got.Rows[0].Version}}
		out, err := svc.BulkDelete(ctx, tenant, 9, "Admin", in)
		if err != nil || out.DeletedCount != 1 {
			t.Fatalf("%s delete %+v %v", item.entity, out, err)
		}
	}
}
func TestBulkDeleteWindowRejectsInvalidRanges(t *testing.T) {
	for _, in := range []BulkDeleteInput{{}, {StartAt: "2026-09-25T00:00:00Z", EndAt: "2026-09-24T00:00:00Z"}, {StartAt: "2026-09-24T00:00:00Z", EndAt: "2026-09-24T00:00:00Z"}} {
		if _, _, err := bulkWindow(in); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}
