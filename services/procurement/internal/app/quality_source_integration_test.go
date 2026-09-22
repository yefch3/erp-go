package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

func TestQualitySourceScopeAndCreation(t *testing.T) {
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
	tenant := time.Now().UnixNano()
	var req, po, item int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status,owner_id) VALUES($1,1,'CT-D5',1,$1,11,'P11','Coil',7,'MT',10,10,'ORDERED',55) RETURNING id`, tenant).Scan(&req); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,status,buyer_id,buyer_name,ordered_at) VALUES($1,'PO-D5',9,'F9','Factory 9','CNY',1000,'ORDERED',77,'Buyer',now()) RETURNING id`, tenant).Scan(&po); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_code,product_name,spec,uom_id,uom_code,qty,unit_price,amount) VALUES($1,$2,$3,11,'P11','Coil','Q235 / 2mm',7,'MT',10,100,1000) RETURNING id`, tenant, po, req).Scan(&item); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM quality_inspection_tasks WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenant)
	}()

	scopes := &scopeStub{views: map[int64]Visibility{88: {All: true}, 89: {EmployeeIDs: []int64{77}}, 90: {EmployeeIDs: []int64{999}}, 91: {EmployeeIDs: []int64{55}}}}
	svc := New(pool, Deps{Scopes: scopes})
	qc := Operator{ID: 88, Name: "QC"}
	for _, employee := range []int64{88, 89, 91} {
		orders, total, e := svc.ListQualitySourceOrders(ctx, tenant, 0, "PO-D5", 1, 20, Operator{ID: employee})
		if e != nil || total != 1 || len(orders) != 1 {
			t.Fatalf("scope %d: %v %d", employee, e, total)
		}
	}
	for _, input := range []struct{ tenant, id, employee int64 }{{tenant, po, 90}, {tenant + 1, po, 88}} {
		if _, _, e := svc.ListQualitySourceOrders(ctx, input.tenant, input.id, "", 1, 20, Operator{ID: input.employee}); e == nil {
			t.Fatal("unauthorized source visible")
		}
		if _, e := svc.CreateQualityInspection(ctx, input.tenant, ApplyQualityInput{POID: po}, Operator{ID: input.employee}); e == nil {
			t.Fatal("unauthorized create allowed")
		}
	}
	orders, _, e := svc.ListQualitySourceOrders(ctx, tenant, po, "", 1, 20, qc)
	if e != nil || len(orders[0].Lines) != 1 || orders[0].Lines[0].Qty != "10.0000" {
		t.Fatalf("source lines: %+v %v", orders, e)
	}
	if _, e = svc.CreateQualityInspection(ctx, tenant, ApplyQualityInput{POID: po, ExpectedDate: "bad"}, qc); e == nil {
		t.Fatal("bad date accepted")
	}
	if _, e = pool.Exec(ctx, `UPDATE purchase_orders SET status='DRAFT' WHERE tenant_id=$1 AND id=$2`, tenant, po); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateQualityInspection(ctx, tenant, ApplyQualityInput{POID: po}, qc); e == nil {
		t.Fatal("draft accepted")
	}
	if _, e = pool.Exec(ctx, `UPDATE purchase_orders SET status='ORDERED' WHERE tenant_id=$1 AND id=$2`, tenant, po); e != nil {
		t.Fatal(e)
	}
	results := make(chan QualityTask, 2)
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			q, e := svc.CreateQualityInspection(ctx, tenant, ApplyQualityInput{POID: po, ExpectedDate: "2026-10-01", Location: "Inspection site", ContactName: "Contact", ContactPhone: "123", Remark: "Check packaging"}, qc)
			results <- q
			errors <- e
		}()
	}
	first, second := <-results, <-results
	if e := <-errors; e != nil {
		t.Fatal(e)
	}
	if e := <-errors; e != nil {
		t.Fatal(e)
	}
	if first.ID == 0 || first.ID != second.ID || first.Status != "WAITING" || len(first.Lines) != 1 || first.ContactName != "Contact" {
		t.Fatalf("creation: %+v %+v", first, second)
	}
	orders, _, e = svc.ListQualitySourceOrders(ctx, tenant, po, "", 1, 20, qc)
	if e != nil || orders[0].ExistingTaskID != first.ID {
		t.Fatalf("existing task not linked: %v", e)
	}
}
