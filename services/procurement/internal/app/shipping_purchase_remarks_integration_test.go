package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestShippingPurchaseRemarksContractAndPermissionBoundary(t *testing.T) {
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
	defer func() {
		for _, table := range []string{"purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1 OR tenant_id=$2", tenant, tenant+1)
		}
	}()
	var lineID int64
	seed := func(tid, contractID, sourceID int64, contractNo, state, kind, factory string) int64 {
		t.Helper()
		lineID++
		var req, po int64
		err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,ordered_qty,status,owner_id) VALUES($1,$2,$3,1,$4,11,'P11','Coil',7,'MT',10,10,'ORDERED',55) RETURNING id`, tid, contractID, contractNo, lineID).Scan(&req)
		if err != nil {
			t.Fatal(err)
		}
		err = pool.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_code,supplier_name,currency,total_amount,status,buyer_id,buyer_name,source_business_id,business_type,remark) VALUES($1,$2,9,'F9',$2,'CNY',987654,$3,77,'Buyer',$4,$5,$6) RETURNING id`, tid, factory, state, sourceID, kind, factory+" packing\nno mixing").Scan(&po)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_code,product_name,spec,uom_id,uom_code,qty,unit_price,amount) VALUES($1,$2,$3,11,'P11','Coil','same spec',7,'MT',10,98765,987654)`, tid, po, req)
		if err != nil {
			t.Fatal(err)
		}
		return po
	}
	a := seed(tenant, 7, 7, "CT-OWN", "ORDERED", "PROCUREMENT", "Factory A")
	seed(tenant, 7, 7, "CT-OWN", "PARTIALLY_RECEIVED", "PROCUREMENT", "Factory B")
	seed(tenant, 7, 0, "CT-OWN", "RECEIVED", "PROCUREMENT", "Factory Legacy")
	seed(tenant+1, 7, 7, "CT-OWN", "ORDERED", "PROCUREMENT", "Foreign tenant")
	seed(tenant, 8, 8, "CT-OWN", "ORDERED", "PROCUREMENT", "Wrong ID")
	seed(tenant, 7, 7, "CT-OTHER", "ORDERED", "PROCUREMENT", "Wrong number")
	seed(tenant, 7, 8, "CT-OWN", "ORDERED", "PROCUREMENT", "Wrong header")
	seed(tenant, 7, 7, "CT-OWN", "DRAFT", "PROCUREMENT", "Draft")
	svc := New(pool, Deps{Scopes: &listAccess{}})
	shipping := Operator{ID: 301}
	if err = svc.AuthorizeOrder(ctx, tenant, a, shipping); err == nil {
		t.Fatal("shipping-only user gained full PO access")
	}
	rows, err := svc.ListShippingPurchaseRemarks(ctx, tenant, 7, " CT-OWN ", shipping)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	for i, name := range []string{"Factory A", "Factory B", "Factory Legacy"} {
		if rows[i].SupplierName != name || rows[i].PurchaseOrderRemark != name+" packing\nno mixing" {
			t.Fatal(rows)
		}
	}
	_, err = pool.Exec(ctx, `UPDATE purchase_orders SET remark='' WHERE tenant_id=$1 AND id=$2`, tenant, a)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = svc.ListShippingPurchaseRemarks(ctx, tenant, 7, "CT-OWN", shipping)
	if err != nil || rows[0].PurchaseOrderRemark != "" {
		t.Fatalf("current empty note not preserved: %+v %v", rows, err)
	}
	for _, id := range []int64{0, 201, 999} {
		if _, err = svc.ListShippingPurchaseRemarks(ctx, tenant, 7, "CT-OWN", Operator{ID: id}); err == nil {
			t.Fatalf("employee %d bypassed shipping permission", id)
		}
	}
	if _, err = New(pool, Deps{}).ListShippingPurchaseRemarks(ctx, tenant, 7, "CT-OWN", shipping); err == nil {
		t.Fatal("missing IAM must fail closed")
	}
	if _, err = svc.ListShippingPurchaseRemarks(ctx, tenant, 0, "CT-OWN", shipping); err == nil {
		t.Fatal("invalid contract accepted")
	}
	if _, err = svc.ListShippingPurchaseRemarks(ctx, 0, 7, "CT-OWN", shipping); err == nil {
		t.Fatal("invalid tenant accepted")
	}
	rows, err = svc.ListShippingPurchaseRemarks(ctx, tenant, 99, "CT-OWN", shipping)
	if err != nil || len(rows) != 0 {
		t.Fatalf("unrelated contract leaked: %+v %v", rows, err)
	}
}
