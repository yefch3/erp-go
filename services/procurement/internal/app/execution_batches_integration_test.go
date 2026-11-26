package app

import (
	"context"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestExecutionBatchListQuoteStatuses(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_execution_supplier_quotes WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()

	const me, colleague = 81, 82
	svc := New(pool, Deps{Scopes: &scopeStub{views: map[int64]Visibility{
		me:        {ScopeType: "SELF", EmployeeIDs: []int64{me}},
		colleague: {ScopeType: "ALL", All: true},
	}}})
	line := func(contractID, itemID, owner int64, contractNo, product, unit, date string, ordered int) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO purchase_requirements
			(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,
			 product_name,uom_code,required_qty,ordered_qty,required_date,status,owner_id)
			VALUES ($1,$2,$3,1,$4,$4,$5,$5,$6,10,$7,$8::text::date,'WAITING_REQUOTE',$9) RETURNING id`,
			tenantID, contractID, contractNo, itemID, product, unit, ordered, date, owner).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	quote := func(requirementID, author int64) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO purchase_execution_supplier_quotes
			(tenant_id,requirement_id,supplier_id,supplier_name,currency,unit_price,created_by_id)
			VALUES ($1,$2,$3,'Factory','CNY',50,$3)`, tenantID, requirementID, author)
		if err != nil {
			t.Fatal(err)
		}
	}

	first := line(1, 101, me, "CT-ONE", "Coil", "MT", "2027-06-01", 2)
	second := line(1, 102, me, "CT-ONE", "Search Target", "PCS", "2027-06-02", 0)
	other := line(2, 201, me, "CT-TWO", "Sheet", "MT", "2027-06-03", 0)
	line(3, 301, me, "CT-THREE", "Plate", "MT", "2027-06-04", 0)
	hidden := line(3, 302, colleague, "CT-THREE", "Hidden product", "MT", "2027-06-04", 0)
	line(4, 401, me, "CT-COVERED", "Covered", "MT", "2027-05-01", 10)
	var draftID int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_orders
		(tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name)
		VALUES ($1,'PO-RFQ-RESERVED',9,'Factory','CNY',500,'DRAFT',$2,'Buyer') RETURNING id`, tenantID, me).Scan(&draftID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO purchase_order_items
		(tenant_id,po_id,requirement_id,product_id,product_name,uom_code,qty,unit_price,amount)
		VALUES ($1,$2,$3,102,'Search Target','PCS',10,50,500)`, tenantID, draftID, second); err != nil {
		t.Fatal(err)
	}
	quote(first, me)
	quote(second, colleague)
	quote(other, colleague)
	quote(hidden, me)

	check := func(operator int64, keyword, batchKey, filter string, page, size int32, wantTotal int64, wantRows int, want map[string]string) {
		t.Helper()
		rows, total, got, err := svc.ListExecutionBatchRequirements(ctx, tenantID, Operator{ID: operator}, keyword, batchKey, filter, page, size)
		if err != nil || total != wantTotal || len(rows) != wantRows || !maps.Equal(got, want) {
			t.Fatalf("operator=%d search=%q batch=%q filter=%q page=%d: total=%d, rows=%d, statuses=%v, error=%v",
				operator, keyword, batchKey, filter, page, total, len(rows), got, err)
		}
	}
	const firstKey, secondKey, thirdKey = "CONTRACT-ID:1", "CONTRACT-ID:2", "CONTRACT-ID:3"
	check(me, "", "", "", 1, 20, 3, 4, map[string]string{firstKey: "MY_QUOTED", secondKey: "COLLEAGUE_QUOTED", thirdKey: "WAITING"})
	check(me, "", "", "MY_QUOTED", 1, 1, 1, 2, map[string]string{firstKey: "MY_QUOTED"})
	check(me, "", "", "COLLEAGUE_QUOTED", 1, 1, 1, 1, map[string]string{secondKey: "COLLEAGUE_QUOTED"})
	check(me, "", "", "WAITING", 1, 1, 1, 1, map[string]string{thirdKey: "WAITING"})
	check(me, "", "", "WAITING", 2, 1, 1, 0, nil)
	check(me, "", "", "", 2, 1, 3, 1, map[string]string{secondKey: "COLLEAGUE_QUOTED"})
	check(me, "search target", "", "", 1, 20, 1, 2, map[string]string{firstKey: "MY_QUOTED"})
	check(me, "ct-one", "", "", 1, 20, 1, 2, map[string]string{firstKey: "MY_QUOTED"})
	check(me, "Hidden product", "", "", 1, 20, 0, 0, nil)
	check(me, "", firstKey, "", 1, 20, 1, 2, map[string]string{firstKey: "MY_QUOTED"})
	check(colleague, "", thirdKey, "", 1, 20, 1, 2, map[string]string{thirdKey: "COLLEAGUE_QUOTED"})
}
