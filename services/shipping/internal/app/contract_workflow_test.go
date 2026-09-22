package app

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

type workflowAllowedContract struct{}

func (workflowAllowedContract) Check(context.Context, int64) error { return nil }
func TestContractHandoffRejectsOldVersionAndTenant(t *testing.T) {
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
	tenant := time.Now().UnixNano()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant) }()
	svc := New(pool)
	svc.UseContractGuard(workflowAllowedContract{})
	event := ContractEffective{ContractID: 41, ContractNo: "CT-41", VersionID: 11, VersionNo: 1, Shipments: []ContractEffectiveShipment{{BatchNo: 1, Currency: "USD", FreightAmount: "0"}}}
	claim := func(context.Context, pgx.Tx) error { return nil }
	if err := svc.HandoffsFromContract(ctx, tenant, event, claim); err != nil {
		t.Fatal(err)
	}
	var oldID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	event.VersionID = 12
	event.VersionNo = 2
	if err := svc.HandoffsFromContract(ctx, tenant, event, claim); err != nil {
		t.Fatal(err)
	}
	var currentID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM contract_shipping_handoffs WHERE tenant_id=$1 AND version_no=2`, tenant).Scan(&currentID); err != nil {
		t.Fatal(err)
	}
	if err := svc.checkHandoffExecution(ctx, tenant, oldID); err == nil {
		t.Fatal("old version continued booking")
	}
	if err := svc.checkHandoffExecution(ctx, tenant+1, currentID); err == nil {
		t.Fatal("another tenant accessed handoff")
	}
	if err := svc.checkHandoffExecution(ctx, tenant, currentID); err != nil {
		t.Fatal(err)
	}
	event.VersionID = 11
	event.VersionNo = 1
	if err := svc.HandoffsFromContract(ctx, tenant, event, claim); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM contract_shipping_handoffs WHERE tenant_id=$1 AND id=$2`, tenant, currentID).Scan(&status); err != nil || status != "WAITING_REQUOTE" {
		t.Fatal("late event superseded latest handoff", status, err)
	}
}
