package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type deliveryQualityStub struct {
	err     error
	checked [][]int64
}

func (g *deliveryQualityStub) Check(_ context.Context, ids []int64) error {
	g.checked = append(g.checked, append([]int64(nil), ids...))
	return g.err
}

type outboundQualityNumbering struct{ next int }

func (n *outboundQualityNumbering) Next(context.Context, string) (string, error) {
	n.next++
	return fmt.Sprintf("QUALITY-OUT-%d", n.next), nil
}

type outboundQualityFixture struct {
	ctx                         context.Context
	pool                        *pgxpool.Pool
	svc                         *Service
	tenant, warehouse, outbound int64
}

func newOutboundQualityFixture(t *testing.T, source string, extraPurchase bool) *outboundQualityFixture {
	t.Helper()
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &outboundQualityFixture{ctx: ctx, pool: pool, tenant: time.Now().UnixNano()}
	f.svc = New(pool, &outboundQualityNumbering{})
	f.svc.UseContractGuard(&outboundContractStub{})
	t.Cleanup(func() {
		for _, table := range []string{"outbound_item_stocks", "outbound_items", "outbounds", "stock_reservation_lines", "stock_reservations", "stock_locks", "stock_ledger", "stocks", "outbox_events", "warehouses"} {
			if _, err := pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", f.tenant); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
		pool.Close()
	})
	if err := pool.QueryRow(ctx, `INSERT INTO warehouses(tenant_id,code,name) VALUES($1,'QUALITY','质检门禁测试仓') RETURNING id`, f.tenant).Scan(&f.warehouse); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	claim := func(context.Context, pgx.Tx) error { return nil }
	if source == "CLEARED_MANUAL" || source == "CLEARED_BAD_LEDGER" {
		// A historical anonymous lot was completely dispatched before the new
		// purchase arrived. It must not contaminate the later, traceable pool.
		if err := f.svc.ReceiveStock(ctx, f.tenant, f.warehouse, []ReceiveLine{{ProductID: 42, ProductCode: "Q42", ProductName: "质检产品", UomCode: "MT", Qty: "10", UnitCost: "5"}}, "旧库存", Operator{ID: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE stocks SET on_hand_qty=0,total_cost=0 WHERE tenant_id=$1`, f.tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO stock_ledger(tenant_id,stock_id,warehouse_id,sku_id,movement,qty,ref_type,ref_id,on_hand_after,available_after)
			SELECT tenant_id,id,warehouse_id,sku_id,'OUTBOUND',10,'OUTBOUND',1,0,0 FROM stocks WHERE tenant_id=$1`, f.tenant); err != nil {
			t.Fatal(err)
		}
		if source == "CLEARED_BAD_LEDGER" {
			// The final historical balance still reaches zero, but the first
			// recorded on-hand snapshot proves the movement chain is incomplete.
			if _, err := pool.Exec(ctx, `UPDATE stock_ledger SET qty=9 WHERE tenant_id=$1 AND movement IN ('INBOUND','OUTBOUND')`, f.tenant); err != nil {
				t.Fatal(err)
			}
		}
	}
	if source != "MANUAL" {
		if err := f.svc.ReceivePurchase(ctx, f.tenant, PurchaseReceived{POID: 7001, PONo: "PO-Q-1", ReceiptNo: "RC-Q-1", WarehouseID: f.warehouse, Lines: []PurchaseLine{{ProductID: 42, ProductCode: "Q42", ProductName: "质检产品", UomCode: "MT", Qty: "10", UnitCost: "5", Currency: "CNY"}}}, log, claim); err != nil {
			t.Fatal(err)
		}
	}
	if source == "MANUAL" || source == "MIXED" {
		if err := f.svc.ReceiveStock(ctx, f.tenant, f.warehouse, []ReceiveLine{{ProductID: 42, ProductCode: "Q42", ProductName: "质检产品", UomCode: "MT", Qty: "10", UnitCost: "5"}}, "无质检来源", Operator{ID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if extraPurchase {
		if err := f.svc.ReceivePurchase(ctx, f.tenant, PurchaseReceived{POID: 7002, PONo: "PO-Q-2", ReceiptNo: "RC-Q-2", WarehouseID: f.warehouse, Lines: []PurchaseLine{{ProductID: 42, ProductCode: "Q42", ProductName: "质检产品", UomCode: "MT", Qty: "10", UnitCost: "5", Currency: "CNY"}}}, log, claim); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.AllocateContract(ctx, f.tenant, ContractEffective{ContractID: 71, ContractNo: "Q-C-71", VersionID: 1, VersionNo: 1, CustomerName: "质检客户", Items: []ContractLine{{ItemID: 81, ProductID: 42, ProductCode: "Q42", ProductName: "质检产品", UomCode: "MT", Qty: "6"}}}, log, claim); err != nil {
		t.Fatal(err)
	}
	out, err := f.svc.CreateOutbound(ctx, f.tenant, CreateOutboundInput{ContractID: 71, Lines: []OutboundLine{{RefLineID: 81, Qty: "3"}}}, Operator{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.outbound = out.ID
	return f
}

type outboundQualitySnapshot struct {
	status, onHand, locked, shipped string
	ledgerCount, eventCount         int
}

func (f *outboundQualityFixture) snapshot(t *testing.T) outboundQualitySnapshot {
	t.Helper()
	var v outboundQualitySnapshot
	err := f.pool.QueryRow(f.ctx, `SELECT o.status,s.on_hand_qty::text,s.locked_qty::text,r.shipped_qty::text,
		(SELECT count(*) FROM stock_ledger WHERE tenant_id=$1 AND movement='OUTBOUND'),
		(SELECT count(*) FROM outbox_events WHERE tenant_id=$1 AND event_type='StockOutbound')
		FROM outbounds o JOIN stocks s ON s.tenant_id=o.tenant_id
		JOIN stock_reservations r ON r.tenant_id=o.tenant_id
		WHERE o.tenant_id=$1 AND o.id=$2`, f.tenant, f.outbound).Scan(&v.status, &v.onHand, &v.locked, &v.shipped, &v.ledgerCount, &v.eventCount)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOutboundQualityBlocksWithoutSideEffectsAndAllowsPassedRetry(t *testing.T) {
	f := newOutboundQualityFixture(t, "PURCHASE", false)
	before := f.snapshot(t)
	for _, test := range []struct {
		name  string
		guard DeliveryQualityGuard
	}{
		{"checker not configured", nil},
		{"uninspected", &deliveryQualityStub{err: apierr.Conflict("QUALITY_NOT_INSPECTED", "未质检")}},
		{"inspection in progress", &deliveryQualityStub{err: apierr.Conflict("QUALITY_IN_PROGRESS", "质检中")}},
		{"failed", &deliveryQualityStub{err: apierr.Conflict("QUALITY_FAILED", "不合格")}},
		{"reinspection", &deliveryQualityStub{err: apierr.Conflict("QUALITY_REINSPECTION", "复检中")}},
		{"service unavailable", &deliveryQualityStub{err: errors.New("procurement unavailable")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f.svc.UseDeliveryQualityGuard(test.guard)
			if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err == nil {
				t.Fatal("unverified goods were shipped")
			}
			if after := f.snapshot(t); after != before {
				t.Fatalf("refused confirmation changed stock/reservation/outbox: before=%+v after=%+v", before, after)
			}
		})
	}
	passed := &deliveryQualityStub{}
	f.svc.UseDeliveryQualityGuard(passed)
	if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(passed.checked, [][]int64{{7001}}) {
		t.Fatalf("wrong purchase evidence: %v", passed.checked)
	}
	after := f.snapshot(t)
	if after.status != "CONFIRMED" || after.onHand != "7.0000" || after.locked != "0.0000" || after.shipped != "3.0000" || after.ledgerCount != 1 || after.eventCount != 1 {
		t.Fatalf("passed goods did not ship exactly once: %+v", after)
	}
	if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err == nil {
		t.Fatal("duplicate confirmation accepted")
	}
	if again := f.snapshot(t); again != after {
		t.Fatalf("duplicate confirmation changed stock: %+v", again)
	}
}

func TestOutboundQualityChecksAllPooledPurchaseSources(t *testing.T) {
	f := newOutboundQualityFixture(t, "PURCHASE", true)
	g := &deliveryQualityStub{err: apierr.Conflict("QUALITY_NOT_INSPECTED", "第二份采购未质检")}
	f.svc.UseDeliveryQualityGuard(g)
	before := f.snapshot(t)
	if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err == nil {
		t.Fatal("one passed source masked an uninspected source")
	}
	if !reflect.DeepEqual(g.checked, [][]int64{{7001, 7002}}) {
		t.Fatalf("must check every source in the stock pool: %v", g.checked)
	}
	if after := f.snapshot(t); after != before {
		t.Fatalf("blocked mixed sources mutated stock: %+v", after)
	}
	// The same pending picking list remains cancellable without a passing inspection.
	if err := f.svc.CancelOutbound(f.ctx, f.tenant, f.outbound, "等待质检，取消拣货", Operator{ID: 1}); err != nil {
		t.Fatal(err)
	}
	after := f.snapshot(t)
	if after.status != "CANCELLED" || after.locked != "0.0000" || after.onHand != before.onHand || after.eventCount != 0 {
		t.Fatalf("cancel did not release unshipped stock safely: %+v", after)
	}
}

func TestOutboundQualityRejectsUnverifiableSources(t *testing.T) {
	for _, source := range []string{"MANUAL", "MIXED", "MISSING_LEDGER", "MISMATCHED_LEDGER", "INITIAL_IMPORT", "CLEARED_BAD_LEDGER"} {
		t.Run(source, func(t *testing.T) {
			f := newOutboundQualityFixture(t, source, false)
			switch source {
			case "MISSING_LEDGER":
				if _, err := f.pool.Exec(f.ctx, `DELETE FROM stock_ledger WHERE tenant_id=$1 AND movement='INBOUND'`, f.tenant); err != nil {
					t.Fatal(err)
				}
			case "MISMATCHED_LEDGER":
				if _, err := f.pool.Exec(f.ctx, `UPDATE stock_ledger SET qty=qty-1 WHERE tenant_id=$1 AND movement='INBOUND'`, f.tenant); err != nil {
					t.Fatal(err)
				}
			case "INITIAL_IMPORT":
				if _, err := f.pool.Exec(f.ctx, `UPDATE stock_ledger SET ref_type='INITIAL_IMPORT' WHERE tenant_id=$1 AND movement='INBOUND'`, f.tenant); err != nil {
					t.Fatal(err)
				}
			}
			g := &deliveryQualityStub{}
			f.svc.UseDeliveryQualityGuard(g)
			before := f.snapshot(t)
			err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1})
			if apierr.CodeFromError(err) != "IV_QUALITY_SOURCE_REQUIRED" {
				t.Fatalf("unknown source must not be assumed passed: %v", err)
			}
			if len(g.checked) != 0 {
				t.Fatalf("unverifiable provenance should fail before checking unrelated POs: %v", g.checked)
			}
			if after := f.snapshot(t); after != before {
				t.Fatalf("unknown source refusal mutated stock: %+v", after)
			}
		})
	}
}

func TestOutboundQualityIgnoresFullyClearedHistoricalPool(t *testing.T) {
	f := newOutboundQualityFixture(t, "CLEARED_MANUAL", false)
	g := &deliveryQualityStub{}
	f.svc.UseDeliveryQualityGuard(g)
	if err := f.svc.ConfirmOutbound(f.ctx, f.tenant, f.outbound, Operator{ID: 1}); err != nil {
		t.Fatalf("fully cleared anonymous lot must not block a later inspected purchase: %v", err)
	}
	if !reflect.DeepEqual(g.checked, [][]int64{{7001}}) {
		t.Fatalf("must check the new purchase supplying current stock: %v", g.checked)
	}
	after := f.snapshot(t)
	if after.status != "CONFIRMED" || after.onHand != "7.0000" || after.eventCount != 1 {
		t.Fatalf("new inspected pool did not ship: %+v", after)
	}
}

func TestOutboundQualityTenantCannotConfirmAnotherTenantsGoods(t *testing.T) {
	f := newOutboundQualityFixture(t, "PURCHASE", false)
	g := &deliveryQualityStub{}
	f.svc.UseDeliveryQualityGuard(g)
	before := f.snapshot(t)
	if err := f.svc.ConfirmOutbound(f.ctx, f.tenant+1, f.outbound, Operator{ID: 1}); apierr.CodeFromError(err) != "IV_OUTBOUND_NOT_FOUND" {
		t.Fatalf("cross tenant confirmation: %v", err)
	}
	if len(g.checked) != 0 {
		t.Fatalf("cross tenant request leaked source ids: %v", g.checked)
	}
	if after := f.snapshot(t); after != before {
		t.Fatalf("cross tenant request mutated stock: %+v", after)
	}
}
