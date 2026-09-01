package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestShipmentProgressSeparatesUncataloguedProductsByNameAndUOM(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"contract_shipments", "contract_items", "contract_versions", "contracts"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenantID)
		}
	}()

	var contractID, versionID int64
	if err := pool.QueryRow(ctx, `INSERT INTO contracts
		(tenant_id, contract_no, customer_id, customer_name, status, sales_employee_id, sales_employee, created_by, updated_by)
		VALUES ($1,'CT-ZERO-PRODUCT',1,'客户','EFFECTIVE',1,'销售',1,1) RETURNING id`, tenantID).Scan(&contractID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contract_versions
		(tenant_id, contract_id, version_no, status, currency, total_amount, created_by, fx_rate, fx_rate_at, fx_source)
		VALUES ($1,$2,1,'DRAFT','USD',1000,1,1,now(),'TEST') RETURNING id`, tenantID, contractID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	var hrcItemID, galvanizedItemID int64
	if err := pool.QueryRow(ctx, `INSERT INTO contract_items
		(tenant_id, contract_version_id, line_no, product_id, product_name, qty, uom_code, unit_price, amount)
		VALUES ($1,$2,1,0,'HOT ROLL COIL (HRC)',200,'tons',2,400) RETURNING id`, tenantID, versionID).Scan(&hrcItemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contract_items
		(tenant_id, contract_version_id, line_no, product_id, product_name, qty, uom_code, unit_price, amount)
		VALUES ($1,$2,2,0,'GALVANIZED ROLLS',300,'tons',2,600) RETURNING id`, tenantID, versionID).Scan(&galvanizedItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contract_versions SET status='APPROVED' WHERE id=$1`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contracts SET current_version_id=$2 WHERE id=$1`, contractID, versionID); err != nil {
		t.Fatal(err)
	}
	for _, shipment := range []struct {
		itemID int64
		no     string
		qty    string
	}{{hrcItemID, "OB-HRC", "50"}, {galvanizedItemID, "OB-GI", "75"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO contract_shipments
			(tenant_id, contract_id, contract_item_id, product_id, sku_id, outbound_no, qty)
			VALUES ($1,$2,$3,0,0,$4,$5::numeric)`, tenantID, contractID, shipment.itemID, shipment.no, shipment.qty); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := New(pool, Deps{}).ShipmentProgress(ctx, tenantID, contractID, versionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("uncatalogued products must remain separate, got %d rows: %+v", len(rows), rows)
	}
	if rows[0].ProductName != "HOT ROLL COIL (HRC)" || rows[0].Qty != "200.0000" || rows[0].ShippedQty != "50.0000" {
		t.Fatalf("unexpected HRC progress: %+v", rows[0])
	}
	if rows[1].ProductName != "GALVANIZED ROLLS" || rows[1].Qty != "300.0000" || rows[1].ShippedQty != "75.0000" {
		t.Fatalf("unexpected galvanized progress: %+v", rows[1])
	}
}
