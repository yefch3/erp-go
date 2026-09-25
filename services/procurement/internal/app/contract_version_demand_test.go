package app

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestContractVersionDemandIncludesOldCommitments(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenant) }()
	svc := New(pool, Deps{})
	event := ContractEffective{ContractID: 123, ContractNo: "CT-version", SourceSalesRemark: "original sales inquiry note", VersionID: 1, VersionNo: 1, Items: []ContractLine{{ItemID: 11, LineNo: 1, ProductName: "plate", Spec: "Q235", Qty: "20", UomCode: "KG"}}}
	if err := svc.RequirementsFromContract(ctx, tenant, event, slog.Default(), noopClaim); err != nil {
		t.Fatal(err)
	}
	var sourceRemark string
	if err := pool.QueryRow(ctx, `SELECT source_sales_remark FROM purchase_requirements WHERE tenant_id=$1 LIMIT 1`, tenant).Scan(&sourceRemark); err != nil || sourceRemark != event.SourceSalesRemark {
		t.Fatalf("purchase requirement source sales remark = %q, err = %v", sourceRemark, err)
	}
	var oldID int64
	if err := pool.QueryRow(ctx, `UPDATE purchase_requirements SET ordered_qty=15,status='PARTIALLY_ORDERED' WHERE tenant_id=$1 RETURNING id`, tenant).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	event.VersionID = 2
	event.VersionNo = 2
	event.Items = []ContractLine{{ItemID: 21, LineNo: 1, ProductName: "plate", Spec: "Q235", Qty: "10", UomCode: "KG"}, {ItemID: 22, LineNo: 2, ProductName: "plate", Spec: "Q235", Qty: "10", UomCode: "KG"}}
	if err := svc.RequirementsFromContract(ctx, tenant, event, slog.Default(), noopClaim); err != nil {
		t.Fatal(err)
	}
	assertOpen := func(id int64, want string) {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT contract_requirement_open($1,$2)::text`, tenant, id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if !decimal.RequireFromString(value).Equal(decimal.RequireFromString(want)) {
			t.Fatalf("requirement %d open %s want %s", id, value, want)
		}
	}
	assertOpen(oldID, "0")
	var ids []int64
	rows, err := pool.Query(ctx, `SELECT id FROM purchase_requirements WHERE tenant_id=$1 AND version_no=2 ORDER BY id`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	assertOpen(ids[0], "5")
	assertOpen(ids[1], "5")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE purchase_requirements SET ordered_qty=3 WHERE tenant_id=$1 AND id=$2`, tenant, ids[0]); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, `UPDATE purchase_requirements SET ordered_qty=3 WHERE tenant_id=$1 AND id=$2`, tenant, ids[1])
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("second commitment did not wait for contract lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("concurrent orders exceeded latest contract total")
	}
	assertOpen(ids[1], "2")
	if _, err := pool.Exec(ctx, `UPDATE purchase_requirements SET ordered_qty=10 WHERE tenant_id=$1 AND id=$2`, tenant, oldID); err != nil {
		t.Fatal(err)
	}
	assertOpen(ids[1], "7") // Cancelling old commitments restores demand dynamically.
	event.VersionID = 1
	event.VersionNo = 1
	event.Items = []ContractLine{{ItemID: 11, ProductName: "plate", Spec: "Q235", Qty: "99", UomCode: "KG"}}
	if err := svc.RequirementsFromContract(ctx, tenant, event, slog.Default(), noopClaim); err != nil {
		t.Fatal(err)
	}
	assertOpen(oldID, "0")
	assertOpen(ids[1], "7")
}
