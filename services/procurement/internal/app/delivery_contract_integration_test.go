package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type deliveryContractGuardStub struct {
	blocked   map[int64]string
	err       error
	wait      bool
	checked   []int64
	deadlines []time.Duration
}

func (g *deliveryContractGuardStub) Check(ctx context.Context, id int64) error {
	g.checked = append(g.checked, id)
	remaining := time.Duration(0)
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline)
	}
	g.deadlines = append(g.deadlines, remaining)
	if g.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	if g.err != nil {
		return g.err
	}
	if reason := g.blocked[id]; reason != "" {
		return apierr.Conflict("CONTRACT_EXECUTION_BLOCKED", reason)
	}
	return nil
}

type deliveryContractSnapshot struct {
	receipts, events                  int
	received, planned, actual, remark string
}

func (f deliveryQualityFixture) contractSnapshot(t *testing.T) deliveryContractSnapshot {
	t.Helper()
	var out deliveryContractSnapshot
	err := f.pool.QueryRow(context.Background(), `SELECT
	 (SELECT count(*) FROM purchase_receipts WHERE tenant_id=$1 AND po_id=$2),
	 (SELECT count(*) FROM outbox_events WHERE tenant_id=$1),
	 (SELECT sum(received_qty)::text FROM purchase_order_items WHERE tenant_id=$1 AND po_id=$2),
	 coalesce((SELECT planned_date::text FROM purchase_production_milestones WHERE tenant_id=$1 AND po_id=$2 AND node='SENT_TO_PORT'),''),
	 coalesce((SELECT actual_date::text FROM purchase_production_milestones WHERE tenant_id=$1 AND po_id=$2 AND node='SENT_TO_PORT'),''),
	 coalesce((SELECT remark FROM purchase_production_milestones WHERE tenant_id=$1 AND po_id=$2 AND node='SENT_TO_PORT'),'')`, f.tenant, f.po).
		Scan(&out.receipts, &out.events, &out.received, &out.planned, &out.actual, &out.remark)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDeliveryContractControlBlocksNewDeliveryWithoutSideEffects(t *testing.T) {
	for _, state := range []string{"PAUSED", "TERMINATING", "TERMINATED", "COMPLETED", "CANCELLED", "PENDING_SIGN", "EXECUTION_NOT_RELEASED"} {
		t.Run(state, func(t *testing.T) {
			f := newDeliveryQualityFixture(t)
			f.pass(t)
			guard := &deliveryContractGuardStub{blocked: map[int64]string{991: state}}
			f.svc.UseContractGuard(guard)
			ctx := context.Background()
			op := Operator{ID: 77, Name: "Buyer"}
			day := time.Now().Format("2006-01-02")
			// A plan is not a new physical delivery, including during a pause.
			if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", PlannedDate: day, Remark: "plan"}, op); err != nil {
				t.Fatal(err)
			}
			before := f.contractSnapshot(t)
			if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "1"}}, "blocked new delivery", op); code(err) != "CONTRACT_EXECUTION_BLOCKED" {
				t.Fatalf("direct delivery=%v", err)
			}
			if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", PlannedDate: day, ActualDate: day, Remark: "blocked new departure"}, op); code(err) != "CONTRACT_EXECUTION_BLOCKED" {
				t.Fatalf("first actual departure=%v", err)
			}
			if after := f.contractSnapshot(t); after != before {
				t.Fatalf("refused delivery mutated data: before=%+v after=%+v", before, after)
			}
			if !reflect.DeepEqual(guard.checked, []int64{991, 991}) {
				t.Fatalf("wrong persisted delivery contracts: %v", guard.checked)
			}
			for _, deadline := range guard.deadlines {
				if deadline <= 0 || deadline > 5*time.Second {
					t.Fatalf("contract check must have a five-second bound: %s", deadline)
				}
			}
		})
	}
}

func TestDeliveryContractControlFailsClosedWhenLinkedCheckUnavailable(t *testing.T) {
	for _, failure := range []string{"missing", "service error", "deadline"} {
		t.Run(failure, func(t *testing.T) {
			f := newDeliveryQualityFixture(t)
			f.pass(t)
			var guard ContractGuard
			switch failure {
			case "service error":
				guard = &deliveryContractGuardStub{err: errors.New("contract service unavailable")}
			case "deadline":
				guard = &deliveryContractGuardStub{wait: true}
			}
			f.svc.UseContractGuard(guard)
			before := f.contractSnapshot(t)
			for _, departure := range []bool{false, true} {
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				var err error
				if departure {
					_, err = f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: time.Now().Format("2006-01-02")}, Operator{ID: 77})
				} else {
					_, err = f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "1"}}, "", Operator{ID: 77})
				}
				cancel()
				if err == nil || (failure == "missing" && code(err) != "PO_CONTRACT_EXECUTION_UNAVAILABLE") {
					t.Fatalf("%s check allowed delivery: %v", failure, err)
				}
				if after := f.contractSnapshot(t); after != before {
					t.Fatalf("unavailable check mutated delivery: before=%+v after=%+v", before, after)
				}
			}
		})
	}
}

func TestDeliveryContractControlRechecksEveryNewBatchAndAllowsFactCorrections(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	f.pass(t)
	ctx := context.Background()
	op := Operator{ID: 77, Name: "Buyer"}
	guard := &deliveryContractGuardStub{}
	f.svc.UseContractGuard(guard)
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "4"}}, "first batch", op); err != nil {
		t.Fatal(err)
	}
	day := time.Now().Format("2006-01-02")
	if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: day}, op); err != nil {
		t.Fatal(err)
	}
	guard.blocked = map[int64]string{991: "PAUSED"}
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "6"}}, "new batch after pause", op); code(err) != "CONTRACT_EXECUTION_BLOCKED" {
		t.Fatal(err)
	}
	checks := len(guard.checked)
	for _, actual := range []string{day, time.Now().AddDate(0, 0, -1).Format("2006-01-02")} {
		if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: actual, Remark: "correcting an existing fact"}, op); err != nil {
			t.Fatal(err)
		}
	}
	if len(guard.checked) != checks {
		t.Fatal("existing fact maintenance was treated as new fulfilment")
	}
	guard.blocked = nil // Resume permits the remaining new batch.
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "6"}}, "remaining batch after resume", op); err != nil {
		t.Fatal(err)
	}
	head, err := f.svc.GetOrder(ctx, f.tenant, f.po)
	if err != nil || head.Status != "RECEIVED" {
		t.Fatalf("normal resumed order=%+v err=%v", head, err)
	}
}

func TestDeliveryContractControlKeepsCustodyReceiptAndIssueHandlingAvailable(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	f.pass(t)
	guard := &deliveryContractGuardStub{blocked: map[int64]string{991: "TERMINATED"}}
	f.svc.UseContractGuard(guard)
	ctx := context.Background()
	op := Operator{ID: 77, Name: "Buyer"}
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 3, []ReceiptLine{{POItemID: f.item, Qty: "4"}}, "receive existing supplier delivery into custody", op); err != nil {
		t.Fatal(err)
	}
	issue, err := f.svc.ReportReceiptException(ctx, f.tenant, f.po, ReceiptException{POItemID: f.item, Type: "SHORT_SHIPMENT", Qty: "6", Description: "terminate remaining supply"}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ResolveReceiptException(ctx, f.tenant, f.po, issue.ID, "remaining supply cancelled", op); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.CloseOrder(ctx, f.tenant, f.po, CloseOrderInput{ShortfallAction: "DROPPED", Note: "closed during contract aftermath"}, op); err != nil {
		t.Fatal(err)
	}
	if len(guard.checked) != 0 {
		t.Fatalf("custody receipt and aftermath were treated as new dispatch: %v", guard.checked)
	}
}

func TestDeliveryContractControlUsesOnlyDeliveredItemsButAllPortSources(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	var requirement, item int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,993,'PAUSED-CONTRACT',1,994,12,'Sheet','MT',1,1,'ORDERED') RETURNING id`, f.tenant).Scan(&requirement); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_name,uom_code,qty,unit_price,amount) VALUES($1,$2,$3,12,'Sheet','MT',1,100,100) RETURNING id`, f.tenant, f.po, requirement).Scan(&item); err != nil {
		t.Fatal(err)
	}
	f.pass(t)
	guard := &deliveryContractGuardStub{blocked: map[int64]string{993: "PAUSED"}}
	f.svc.UseContractGuard(guard)
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "1"}}, "active contract's goods", Operator{ID: 77}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(guard.checked, []int64{991}) {
		t.Fatalf("unselected paused contract blocked another line: %v", guard.checked)
	}
	before := f.contractSnapshot(t)
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: item, Qty: "1"}}, "paused contract's goods", Operator{ID: 77}); code(err) != "CONTRACT_EXECUTION_BLOCKED" {
		t.Fatal(err)
	}
	if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: time.Now().Format("2006-01-02")}, Operator{ID: 77}); code(err) != "CONTRACT_EXECUTION_BLOCKED" {
		t.Fatal(err)
	}
	if after := f.contractSnapshot(t); after != before {
		t.Fatalf("blocked mixed-contract delivery mutated data: before=%+v after=%+v", before, after)
	}
}

func TestDeliveryContractControlAllowsIndependentAndHistoricalOrdersWithoutGuard(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{false: "independent", true: "historical"}[historical], func(t *testing.T) {
			f := newDeliveryQualityFixture(t)
			ctx := context.Background()
			if historical {
				f.svc = New(f.pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{9: {ID: 9, Code: "SUP9", Name: "Supplier", Status: "ACTIVE"}}})
				t.Cleanup(func() { _, _ = f.pool.Exec(ctx, `DELETE FROM historical_purchase_orders WHERE tenant_id=$1`, f.tenant) })
				po, err := f.svc.SaveHistoricalOrder(ctx, f.tenant, HistoricalOrderInput{SupplierID: 9, PONo: "DELIVERY-HISTORY", OriginalDate: "2025-01-01", Currency: "CNY", Confirm: true, Lines: []HistoricalOrderLine{{ProductID: 11, ProductName: "History Coil", UOM: "MT", Qty: "10", UnitPrice: "100"}}}, Operator{ID: 77, Name: "Buyer"})
				if err != nil {
					t.Fatal(err)
				}
				items, err := f.svc.OrderItems(ctx, f.tenant, po.ID)
				if err != nil || len(items) != 1 {
					t.Fatal(items, err)
				}
				f.po, f.item = po.ID, items[0].ID
			} else if _, err := f.pool.Exec(ctx, `UPDATE purchase_requirements SET contract_id=0,source='MANUAL' WHERE tenant_id=$1 AND id=$2`, f.tenant, f.requirement); err != nil {
				t.Fatal(err)
			}
			f.svc.UseContractGuard(nil)
			f.pass(t)
			if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: "1"}}, "independent delivery", Operator{ID: 77}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: time.Now().Format("2006-01-02")}, Operator{ID: 77}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
