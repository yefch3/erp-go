package app

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

func TestShippingBatchCargoIsNotDuplicated(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	svc := New(pool)
	refs := json.RawMessage(`[{"id":"q7","kind":"LOGISTICS","body":{"company":"Reference forwarder"}}]`)
	event := ContractEffective{ContractID: 50, VersionID: 501, VersionNo: 1, ContractNo: "CT-BATCH", Items: []ContractEffectiveItem{{ContractItemID: 11, LineNo: 1, ProductName: "HRC", Spec: "3mm", Qty: "60", UomCode: "MT", Remark: "dry"}, {ContractItemID: 12, LineNo: 2, ProductName: "HRC", Spec: "4mm", Qty: "40", UomCode: "MT", Remark: "no oil"}}}
	for index, qty := range [][2]string{{"30", "10"}, {"30", "30"}} {
		items := append([]ContractEffectiveItem{}, event.Items...)
		items[0].Qty = qty[0]
		items[1].Qty = qty[1]
		event.Shipments = append(event.Shipments, ContractEffectiveShipment{BatchNo: int32(index + 1), Currency: "USD", FreightAmount: "0", Items: items, AllocationStatus: "ALLOCATED", PresaleReferences: refs})
	}
	claim := func(context.Context, pgx.Tx) error { return nil }
	for n := 0; n < 2; n++ {
		if err := svc.HandoffsFromContract(ctx, tenant, event, claim); err != nil {
			t.Fatal(err)
		}
	}
	var tasks, lines int
	var quantity string
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*),sum(quantity)::text FROM contract_shipping_handoff_cargo WHERE tenant_id=$1`, tenant).Scan(&lines, &quantity); err != nil {
		t.Fatal(err)
	}
	if tasks != 2 || lines != 4 || quantity != "100.0000" {
		t.Fatal(tasks, lines, quantity)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM contract_shipping_handoffs WHERE tenant_id=$1 AND batch_no=1`, tenant).Scan(&id); err != nil {
		t.Fatal(err)
	}
	v, err := svc.GetContractHandoff(ctx, tenant, id)
	if err != nil || v.PresaleReferencesJSON != string(refs) || len(v.CargoItems) != 2 || v.CargoItems[0].Remark != "dry" {
		t.Fatal(v, err)
	}
	// Legacy multi-batch events without allocations produce one pending task.
	event.ContractID = 51
	event.VersionID = 511
	for i := range event.Shipments {
		event.Shipments[i].Items = nil
	}
	if err := svc.HandoffsFromContract(ctx, tenant, event, claim); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contract_shipping_handoffs WHERE tenant_id=$1 AND contract_id=51 AND allocation_status='PENDING_ALLOCATION'`, tenant).Scan(&pending); err != nil || pending != 1 {
		t.Fatal(pending, err)
	}
}
