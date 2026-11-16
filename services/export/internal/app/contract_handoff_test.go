package app

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

func TestHandoffAllocationsConserveEachProduct(t *testing.T) {
	v := ContractView{Version: store.GetContractVersionRow{}, Items: []store.ListContractItemsRow{{ID: 11, LineNo: 1, ProductName: "HRC", Spec: "3mm", Qty: "60", UomCode: "MT", Remark: "dry"}, {ID: 12, LineNo: 2, ProductName: "HRC", Spec: "4mm", Qty: "40", UomCode: "MT"}}}
	// The version concrete type is populated by the database tests below.
	good := []HandoffBatch{{BatchNo: 1, Quantities: map[string]string{"11": "30", "12": "10"}}, {BatchNo: 2, Quantities: map[string]string{"11": "30", "12": "30"}}}
	got, err := allocatedShipments(v, good)
	if err != nil || len(got) != 2 || got[0].Items[0].Qty != "30" || got[0].Items[0].Remark != "dry" {
		t.Fatalf("%+v %v", got, err)
	}
	for name, batches := range map[string][]HandoffBatch{
		"over":      {{BatchNo: 1, Quantities: map[string]string{"11": "61", "12": "40"}}},
		"under":     {{BatchNo: 1, Quantities: map[string]string{"11": "59", "12": "40"}}},
		"foreign":   {{BatchNo: 1, Quantities: map[string]string{"999": "60", "12": "40"}}},
		"negative":  {{BatchNo: 1, Quantities: map[string]string{"11": "-60", "12": "40"}}},
		"duplicate": {{BatchNo: 1, Quantities: map[string]string{"11": "30", "12": "10"}}, {BatchNo: 1, Quantities: map[string]string{"11": "30", "12": "30"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := allocatedShipments(v, batches); err == nil {
				t.Fatal("invalid allocation accepted")
			}
		})
	}
}

func TestHandoffPlanFinanceRelease(t *testing.T) {
	dsn := os.Getenv("EXPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("EXPORT_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	ctx = grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: 1, Name: "Sales"})
	svc := New(pool, Deps{Customers: dueCustomerStub{}, Products: dueProductStub{}, Rates: d2OfferRates{}, Numbering: &d2OfferNumber{}, Approvals: d2Approvals{}, Files: d2Files{}, Scopes: d2OfferScopes{}})
	op := Operator{ID: 1, Name: "Sales"}
	v, err := svc.CreateContract(ctx, tenant, DirectContractInput{CustomerID: 7, Currency: "USD", Terms: Terms{DeliveryDate: "2026-12-15", Text: "contract packing note"}, Items: []ItemInput{{ProductName: "HRC", Spec: "3mm", Qty: "60", UomCode: "MT", UnitPrice: "500", Remark: "dry packing"}, {ProductName: "HRC", Spec: "4mm", Qty: "40", UomCode: "MT", UnitPrice: "600", Remark: "no oil"}}}, op)
	if err != nil {
		t.Fatal(err)
	}
	plan := HandoffPlan{VersionID: v.Version.ID, Batches: []HandoffBatch{{BatchNo: 1, Title: "first", Quantities: map[string]string{}}, {BatchNo: 2, Title: "second", Quantities: map[string]string{}}}}
	id1, id2 := fmtID(v.Items[0].ID), fmtID(v.Items[1].ID)
	plan.Batches[0].Quantities = map[string]string{id1: "30", id2: "10"}
	plan.Batches[1].Quantities = map[string]string{id1: "30", id2: "30"}
	raw, _ := json.Marshal(plan)
	cmd := WorkflowCommand{ID: v.Contract.ID, Action: "handoff_save", Reason: "test allocations", Data: raw}
	saved, err := svc.ContractWorkflow(ctx, tenant, cmd, op)
	if err != nil {
		t.Fatal(err)
	}
	if saved.(HandoffPlan).Revision != 1 {
		t.Fatal("missing revision")
	}
	if _, err := svc.ContractWorkflow(ctx, tenant, cmd, op); err == nil {
		t.Fatal("stale save overwrote plan")
	}
	// Use the established approved/executing fixture boundary; this test owns
	// only its isolated tenant and exercises the real Finance transaction.
	if _, err := pool.Exec(ctx, `UPDATE contract_versions SET status='APPROVED' WHERE tenant_id=$1 AND id=$2`, tenant, v.Version.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contracts SET status='EXECUTING',current_version_id=$3 WHERE tenant_id=$1 AND id=$2`, tenant, v.Contract.ID, v.Version.ID); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if _, err := svc.ConfirmContractExecutionCondition(ctx, tenant, v.Contract.ID, ConditionNoPrepaymentRequired, "test", op); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT count(*),max(payload::text)::jsonb FROM outbox_events WHERE tenant_id=$1 AND event_type='ContractEffective'`, tenant).Scan(&count, &payload); err != nil {
		t.Fatal(err)
	}
	var event contractEffectiveEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(event.Items) != 2 || len(event.Shipments) != 2 || event.Shipments[1].Items[1].Qty != "30" || event.Shipments[0].Items[0].Remark != "dry packing" {
		t.Fatalf("event=%+v count=%d", event, count)
	}
	plan.Revision = 1
	cmd.Data, _ = json.Marshal(plan)
	if _, err := svc.ContractWorkflow(ctx, tenant, cmd, op); err == nil {
		t.Fatal("released allocation was editable")
	}
}

func fmtID(id int64) string { b, _ := json.Marshal(id); return string(b) }
