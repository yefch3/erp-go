package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestD3ReleasedContractStopsBeforePurchaseOrder(t *testing.T) {
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
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID) }()
	svc := New(pool, Deps{})
	err = svc.RequirementsFromContract(ctx, tenantID, ContractEffective{ContractID: 31001, ContractNo: "CT-D3-QUEUE", VersionID: 310011, VersionNo: 1, CustomerName: "D3 Customer", Items: []ContractLine{{ItemID: 3100111, ProductID: 1, ProductName: "D3 Coil", Qty: "10", UomCode: "MT"}}}, slog.Default(), noopClaim)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM purchase_requirements WHERE tenant_id=$1`, tenantID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "WAITING_REQUOTE" {
		t.Fatalf("status=%s", status)
	}
	var changed int
	if err = pool.QueryRow(ctx, `WITH changed AS (UPDATE purchase_requirements SET ordered_qty=1 WHERE tenant_id=$1 AND status IN ('PENDING','PARTIALLY_ORDERED') RETURNING 1) SELECT count(*) FROM changed`, tenantID).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatal("D3 task crossed the purchase-order fence")
	}
}
