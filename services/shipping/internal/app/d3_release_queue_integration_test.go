package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestD3ReleasedContractStopsBeforeShippingSchedule(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("SHIPPING_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenantID) }()
	svc := New(pool)
	claim := func(context.Context, pgx.Tx) error { return nil }
	err = svc.HandoffsFromContract(ctx, tenantID, ContractEffective{ContractID: 32001, ContractNo: "CT-D3-SHIP", VersionID: 320011, VersionNo: 1, CustomerID: 1, CustomerName: "D3 Customer", Shipments: []ContractEffectiveShipment{{BatchNo: 1, Currency: "USD", FreightAmount: "800", CarrierForwarder: "Presales reference"}}}, claim)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenantID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "WAITING_REQUOTE" {
		t.Fatalf("status=%s", status)
	}
	var changed int
	if err = pool.QueryRow(ctx, `WITH changed AS (UPDATE contract_shipping_handoffs SET status='SCHEDULED' WHERE tenant_id=$1 AND status='PENDING' RETURNING 1) SELECT count(*) FROM changed`, tenantID).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatal("D3 task crossed the shipping-schedule fence")
	}
}
