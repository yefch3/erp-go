package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

type deliveryQualityFixture struct {
	pool                          *pgxpool.Pool
	svc                           *Service
	tenant, requirement, po, item int64
}

func newDeliveryQualityFixture(t *testing.T) deliveryQualityFixture {
	t.Helper()
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := deliveryQualityFixture{pool: pool, tenant: time.Now().UnixNano()}
	f.svc = New(pool, Deps{Numbering: &sequenceNumbering{}, Warehouses: warehouseDirectoryStub{warehouse: Warehouse{ID: 3, Type: "NORMAL", Status: "ACTIVE"}}})
	f.svc.UseContractGuard(&deliveryContractGuardStub{})
	t.Cleanup(func() {
		for _, table := range []string{"quality_inspection_tasks", "purchase_inspections", "purchase_receipt_exceptions", "purchase_receipts", "purchase_production_milestones", "outbox_events", "purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", f.tenant)
		}
		pool.Close()
	})
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,991,'CT-QUALITY',1,992,11,'Coil','MT',10,10,'ORDERED') RETURNING id`, f.tenant).Scan(&f.requirement); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name) VALUES($1,'PO-QUALITY-GATE',9,'Factory','CNY',1000,'ORDERED',77,'Buyer') RETURNING id`, f.tenant).Scan(&f.po); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_name,uom_code,qty,unit_price,amount) VALUES($1,$2,$3,11,'Coil','MT',10,100,1000) RETURNING id`, f.tenant, f.po, f.requirement).Scan(&f.item); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f deliveryQualityFixture) check(t *testing.T, in DeliveryQualityInput, allowed bool) DeliveryQualityResult {
	t.Helper()
	got, err := f.svc.CheckDeliveryQuality(context.Background(), f.tenant, in)
	if err != nil || got.Allowed != allowed || (!allowed && (got.Code == "" || got.Reason == "")) {
		t.Fatalf("gate allowed=%v got=%+v err=%v", allowed, got, err)
	}
	return got
}

func (f deliveryQualityFixture) apply(t *testing.T) QualityTask {
	t.Helper()
	task, err := f.svc.ApplyQualityInspection(context.Background(), f.tenant, ApplyQualityInput{POID: f.po}, Operator{ID: 77, Name: "Buyer"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func (f deliveryQualityFixture) pass(t *testing.T) QualityTask {
	t.Helper()
	task := f.apply(t)
	lines := make([]SubmitQualityLine, 0, len(task.Lines))
	for _, line := range task.Lines {
		lines = append(lines, SubmitQualityLine{TaskLineID: line.ID, Result: "PASS", InspectedQty: line.RequestedQty, QualifiedQty: line.RequestedQty, UnqualifiedQty: "0"})
	}
	task, err := f.svc.SubmitQualityRound(context.Background(), f.tenant, task.ID, SubmitQualityInput{Lines: lines}, Operator{ID: 88, Name: "QC"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestDeliveryQualityRequiresPassingInspectionBeforeAnyReceipt(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	in := DeliveryQualityInput{POIDs: []int64{f.po}}
	f.check(t, in, false)
	assertBlockedReceipt := func() {
		t.Helper()
		for _, warehouse := range []int64{0, 3} {
			if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, warehouse, []ReceiptLine{{POItemID: f.item, Qty: "1"}}, "", Operator{ID: 77}); err == nil {
				t.Fatal("uninspected delivery accepted")
			}
		}
		var receipts, events int
		var received string
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM purchase_receipts WHERE tenant_id=$1`, f.tenant).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1`, f.tenant).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if err := f.pool.QueryRow(ctx, `SELECT received_qty::text FROM purchase_order_items WHERE tenant_id=$1 AND id=$2`, f.tenant, f.item).Scan(&received); err != nil {
			t.Fatal(err)
		}
		if receipts != 0 || events != 0 || received != "0.0000" {
			t.Fatalf("blocked receipt mutated data receipts=%d events=%d received=%s", receipts, events, received)
		}
	}
	assertBlockedReceipt()
	task := f.apply(t)
	f.check(t, in, false)
	assertBlockedReceipt()
	task, err := f.svc.StartQualityTask(ctx, f.tenant, task.ID, Operator{ID: 88, Name: "QC"})
	if err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	assertBlockedReceipt()
	task, err = f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PARTIAL", InspectedQty: "10", QualifiedQty: "8", UnqualifiedQty: "2"}}}, Operator{ID: 88, Name: "QC"})
	if err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	assertBlockedReceipt()
	if _, err = f.svc.RecordQualityProcurementHandling(ctx, f.tenant, f.po, task.ID, "REWORK", "返工后复检", Operator{ID: 77, Name: "Buyer"}); err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	if _, err = f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PASS", InspectedQty: "2", QualifiedQty: "2", UnqualifiedQty: "0"}}}, Operator{ID: 88, Name: "QC"}); err != nil {
		t.Fatal(err)
	}
	f.check(t, in, true)
	for _, qty := range []string{"4", "6"} {
		if _, err = f.svc.ReceiveOrder(ctx, f.tenant, f.po, 0, []ReceiptLine{{POItemID: f.item, Qty: qty}}, "", Operator{ID: 77, Name: "Buyer"}); err != nil {
			t.Fatal(err)
		}
	}
	head, err := f.svc.GetOrder(ctx, f.tenant, f.po)
	if err != nil || head.Status != "RECEIVED" {
		t.Fatalf("head=%+v err=%v", head, err)
	}
}

func TestDeliveryQualityRejectsCancelledShortageAndIncompleteEvidence(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	in := DeliveryQualityInput{POIDs: []int64{f.po}}
	task := f.apply(t)
	task, err := f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "FAIL", InspectedQty: "10", QualifiedQty: "0", UnqualifiedQty: "10"}}}, Operator{ID: 88, Name: "QC"})
	if err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	if _, err = f.svc.RecordQualityProcurementHandling(ctx, f.tenant, f.po, task.ID, "CANCEL_SHORTAGE", "取消未合格数量", Operator{ID: 77, Name: "Buyer"}); err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	// A COMPLETED label is insufficient: actual current order lines, approved
	// quantities and their matching unit must all be covered by passing results.
	for _, statement := range []string{
		`UPDATE quality_inspection_task_lines SET qualified_qty=10,unresolved_qty=0,final_result='PASS',approved_release_qty=9 WHERE tenant_id=$1`,
		`UPDATE quality_inspection_task_lines SET approved_release_qty=10,spec='different goods' WHERE tenant_id=$1`,
		`UPDATE quality_inspection_task_lines SET approved_release_qty=10,uom_code='KG' WHERE tenant_id=$1`,
		`DELETE FROM quality_inspection_round_lines WHERE tenant_id=$1`,
		`DELETE FROM quality_inspection_task_lines WHERE tenant_id=$1`,
	} {
		if _, err = f.pool.Exec(ctx, statement, f.tenant); err != nil {
			t.Fatal(err)
		}
		f.check(t, in, false)
	}
}

func TestDeliveryQualityContractQuantityAndTenantIsolation(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	f.pass(t)
	f.check(t, DeliveryQualityInput{ContractID: 991, ContractItemIDs: []int64{992}}, true)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "4"}}}, true)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "11"}}}, false)
	f.check(t, DeliveryQualityInput{ContractID: 991, ContractItemIDs: []int64{993}}, false)
	f.check(t, DeliveryQualityInput{ContractID: 990, ContractItemIDs: []int64{992}}, false)
	f.check(t, DeliveryQualityInput{}, false)
	f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}, ContractID: 991}, false)
	if got, err := f.svc.CheckDeliveryQuality(ctx, f.tenant+1, DeliveryQualityInput{POIDs: []int64{f.po}}); err != nil || got.Allowed {
		t.Fatalf("cross tenant got=%+v err=%v", got, err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_requirements SET required_qty=20 WHERE tenant_id=$1 AND id=$2`, f.tenant, f.requirement); err != nil {
		t.Fatal(err)
	}
	f.check(t, DeliveryQualityInput{ContractID: 991, ContractItemIDs: []int64{992}}, false)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "10"}}}, true)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "12"}}}, false)
}

func TestDeliveryQualityCarriesOldInspectionsAcrossContractVersionsWithoutReusingQuantities(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	f.pass(t)
	var latest, sibling, secondPO, secondItem int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,version_no,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,991,'CT-QUALITY-V2',2,2,1992,11,'Coil','MT',10,0,'PENDING') RETURNING id`, f.tenant).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 1992, Qty: "10"}}}, true)
	// A changed specification is a different group and cannot borrow the old
	// passing result, even though the product name still matches.
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_requirements SET spec='new thickness' WHERE tenant_id=$1 AND id=$2`, f.tenant, latest); err != nil {
		t.Fatal(err)
	}
	f.check(t, DeliveryQualityInput{ContractID: 991, ContractItemIDs: []int64{1992}}, false)
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_requirements SET spec='' WHERE tenant_id=$1 AND id=$2`, f.tenant, latest); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,version_no,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,991,'CT-QUALITY-V2',2,2,1993,11,'Coil','MT',10,10,'ORDERED') RETURNING id`, f.tenant).Scan(&sibling); err != nil {
		t.Fatal(err)
	}
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 1992, Qty: "10"}}}, true)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 1993, Qty: "10"}}}, false)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 1992, Qty: "10"}, {ContractItemID: 1993, Qty: "10"}}}, false)
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name) VALUES($1,'PO-QUALITY-GATE-2',9,'Factory','CNY',1000,'ORDERED',77,'Buyer') RETURNING id`, f.tenant).Scan(&secondPO); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_order_items(tenant_id,po_id,requirement_id,product_id,product_name,uom_code,qty,unit_price,amount) VALUES($1,$2,$3,11,'Coil','MT',10,100,1000) RETURNING id`, f.tenant, secondPO, sibling).Scan(&secondItem); err != nil {
		t.Fatal(err)
	}
	// Newly committed but uninspected goods block the group; a completed old
	// order cannot mask the current missing inspection.
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 1992, Qty: "10"}}}, false)
	second := f
	second.po = secondPO
	second.item = secondItem
	second.pass(t)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 1992, Qty: "10"}, {ContractItemID: 1993, Qty: "10"}}}, true)
	// The old ten tonnes and the current second line's ten tonnes have each
	// been allocated once; a later identical line cannot reuse either source.
	if _, err := f.pool.Exec(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,version_no,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,991,'CT-QUALITY-V2',2,2,1994,11,'Coil','MT',1,0,'PENDING')`, f.tenant); err != nil {
		t.Fatal(err)
	}
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 1994, Qty: "1"}}}, false)
	// The old line cannot be mapped safely once several current lines have
	// the same product identity, even though their total supply is sufficient.
	if got := f.check(t, DeliveryQualityInput{ContractID: 991, ContractItemIDs: []int64{992}}, false); got.Code != "QUALITY_DELIVERY_LINE_NOT_FOUND" {
		t.Fatalf("ambiguous historical line=%+v", got)
	}
}

func TestDeliveryQualityMapsUnchangedHistoricalLineWithoutReusingItsQuantity(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	f.pass(t)
	var latest int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,version_no,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,991,'CT-QUALITY-V2',2,2,1992,11,'Coil','MT',10,0,'PENDING') RETURNING id`, f.tenant).Scan(&latest); err != nil {
		t.Fatal(err)
	}
	// A schedule or shipment draft saved before revision still carries 992.
	// The same product has one latest line, so its inspected goods stay valid.
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "10"}}}, true)
	f.check(t, DeliveryQualityInput{ContractID: 991, ContractItemIDs: []int64{992}}, true)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "4"}, {ContractItemID: 1992, Qty: "6"}}}, true)
	if got := f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "6"}, {ContractItemID: 1992, Qty: "6"}}}, false); got.Code != "QUALITY_DELIVERY_QTY_INVALID" {
		t.Fatalf("mixed IDs exceeded current demand=%+v", got)
	}
	// Increasing current demand must not make the old ten inspected tonnes
	// satisfy twelve requested tonnes under two different version IDs.
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_requirements SET required_qty=12 WHERE tenant_id=$1 AND id=$2`, f.tenant, latest); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "6"}, {ContractItemID: 1992, Qty: "6"}}}, false); got.Code != "QUALITY_DELIVERY_QTY_UNCOVERED" {
		t.Fatalf("mixed IDs reused inspected quantity=%+v", got)
	}
	// A source from another contract, even for the identical product, cannot
	// be used to find an otherwise matching latest line in this contract.
	if _, err := f.pool.Exec(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,990,'OTHER',3,3992,11,'Coil','MT',10,0,'PENDING')`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 3992, Qty: "1"}}}, false); got.Code != "QUALITY_DELIVERY_LINE_NOT_FOUND" {
		t.Fatalf("foreign contract source=%+v", got)
	}
	// Changed specifications are a new product identity, not an ID alias.
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_requirements SET spec='new thickness' WHERE tenant_id=$1 AND id=$2`, f.tenant, latest); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "1"}}}, false); got.Code != "QUALITY_DELIVERY_LINE_NOT_FOUND" {
		t.Fatalf("changed historical product=%+v", got)
	}
	// Removing the product altogether from the latest revision also leaves
	// no alias, even when the old procurement and passing result still exist.
	if _, err := f.pool.Exec(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,version_no,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,991,'CT-QUALITY-V2',2,2,1993,12,'Sheet','MT',1,0,'PENDING')`, f.tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1 AND id=$2`, f.tenant, latest); err != nil {
		t.Fatal(err)
	}
	if got := f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "1"}}}, false); got.Code != "QUALITY_DELIVERY_LINE_NOT_FOUND" {
		t.Fatalf("deleted historical product=%+v", got)
	}
}

func TestDeliveryQualityRetainsCurrentIdenticalLinesOwnProcurement(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	var secondRequirement int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_requirements(tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_name,uom_code,required_qty,ordered_qty,status) VALUES($1,991,'CT-QUALITY',1,993,11,'Coil','MT',10,10,'ORDERED') RETURNING id`, f.tenant).Scan(&secondRequirement); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_requirements SET ordered_qty=0,status='PENDING' WHERE tenant_id=$1 AND id=$2`, f.tenant, f.requirement); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_order_items SET requirement_id=$1 WHERE tenant_id=$2 AND id=$3`, secondRequirement, f.tenant, f.item); err != nil {
		t.Fatal(err)
	}
	f.pass(t)
	f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, true)
	// Only the second current line has been procured and passed inspection.
	// Its known source must stay with that line regardless of ID ordering.
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 993, Qty: "10"}}}, true)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "1"}}}, false)
	f.check(t, DeliveryQualityInput{ContractID: 991, Lines: []DeliveryQualityLine{{ContractItemID: 992, Qty: "10"}, {ContractItemID: 993, Qty: "10"}}}, false)
}

func TestDeliveryQualityGatesActualSentToPortButAllowsPlanning(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	op := Operator{ID: 77, Name: "Buyer"}
	day := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	later := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	actual := time.Now().Format("2006-01-02")
	for _, node := range []string{"READY_TO_SHIP", "QUALITY_INSPECTION"} {
		if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: node, ActualDate: actual}, op); err != nil {
			t.Fatal(err)
		}
	}
	// Recording the production inspection node is no substitute for an
	// actual passing inspection task and its complete product results.
	if _, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: actual, Remark: "uninspected"}, op); code(err) != "QUALITY_DELIVERY_NOT_INSPECTED" {
		t.Fatalf("uninspected actual delivery=%v", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM purchase_production_milestones WHERE tenant_id=$1 AND po_id=$2 AND node='SENT_TO_PORT'`, f.tenant, f.po).Scan(&count); err != nil || count != 0 {
		t.Fatalf("blocked actual created node count=%d err=%v", count, err)
	}
	plan, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", PlannedDate: day, Remark: "plan"}, op)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", PlannedDate: later, Remark: "revised plan"}, op)
	if err != nil || plan.PlannedDate != later || plan.ActualDate != "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if _, err = f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", PlannedDate: day, ActualDate: actual, Remark: "must roll back"}, op); code(err) != "QUALITY_DELIVERY_NOT_INSPECTED" {
		t.Fatal(err)
	}
	var savedPlan, savedActual, savedRemark string
	if err = f.pool.QueryRow(ctx, `SELECT planned_date::text,coalesce(actual_date::text,''),remark FROM purchase_production_milestones WHERE tenant_id=$1 AND id=$2`, f.tenant, plan.ID).Scan(&savedPlan, &savedActual, &savedRemark); err != nil || savedPlan != later || savedActual != "" || savedRemark != "revised plan" {
		t.Fatalf("blocked actual mutated plan=%s actual=%s remark=%s err=%v", savedPlan, savedActual, savedRemark, err)
	}
	var events int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1`, f.tenant).Scan(&events); err != nil || events != 0 {
		t.Fatalf("blocked actual emitted events count=%d err=%v", events, err)
	}
	f.pass(t)
	if _, err = f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", PlannedDate: later, ActualDate: actual, Remark: "passed delivery"}, op); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryQualityAllowsUnchangedHistoricalSentToPortFact(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	op := Operator{ID: 77, Name: "Buyer"}
	day := time.Now().Format("2006-01-02")
	later := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := f.pool.Exec(ctx, `INSERT INTO purchase_production_milestones(tenant_id,po_id,node,actual_date,remark,updated_by_id,updated_by_name) VALUES($1,$2,'SENT_TO_PORT',$3::date,'historical delivery',77,'Buyer')`, f.tenant, f.po, day); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: day, Remark: "notes only"}, op)
	if err != nil || got.ActualDate != day || got.Remark != "notes only" {
		t.Fatalf("unchanged delivery=%+v err=%v", got, err)
	}
	if _, err = f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: later, Remark: "changed fact"}, op); code(err) != "QUALITY_DELIVERY_NOT_INSPECTED" {
		t.Fatalf("changed actual bypassed gate: %v", err)
	}
	f.pass(t)
	if _, err = f.svc.SaveProductionMilestone(ctx, f.tenant, f.po, ProductionMilestone{Node: "SENT_TO_PORT", ActualDate: later, Remark: "passed correction"}, op); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryQualityRequiresWarehouseReinspectionAfterNewQualityProblem(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	buyer := Operator{ID: 77, Name: "Buyer"}
	inspector := Operator{ID: 88, Name: "QC"}
	in := DeliveryQualityInput{POIDs: []int64{f.po}}
	f.pass(t)
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 3, []ReceiptLine{{POItemID: f.item, Qty: "4"}}, "", buyer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ReceiveOrder(ctx, f.tenant, f.po, 3, []ReceiptLine{{POItemID: f.item, Qty: "6"}}, "", buyer); err != nil {
		t.Fatal(err)
	}
	var receipt, otherReceipt int64
	if err := f.pool.QueryRow(ctx, `SELECT min(id),max(id) FROM purchase_receipts WHERE tenant_id=$1 AND po_id=$2`, f.tenant, f.po).Scan(&receipt, &otherReceipt); err != nil {
		t.Fatal(err)
	}
	pass := func(qty string) {
		t.Helper()
		if _, err := f.svc.RecordInspection(ctx, f.tenant, f.po, PurchaseInspection{ReceiptID: receipt, POItemID: f.item, Result: "PASS", InspectedQty: qty}, inspector); err != nil {
			t.Fatal(err)
		}
	}
	pass("4")
	f.check(t, in, true)
	fail, err := f.svc.RecordInspection(ctx, f.tenant, f.po, PurchaseInspection{ReceiptID: receipt, POItemID: f.item, Result: "FAIL", InspectedQty: "4", DefectQty: "1", Note: "warehouse rust"}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	if _, err = f.svc.ResolveInspection(ctx, f.tenant, f.po, fail.ID, "DEDUCTION", "扣款处理备注", buyer); err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	if _, err = f.svc.RecordInspection(ctx, f.tenant, f.po, PurchaseInspection{ReceiptID: otherReceipt, POItemID: f.item, Result: "PASS", InspectedQty: "6"}, inspector); err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	pass("1")
	f.check(t, in, false)
	pass("4")
	f.check(t, in, true)
	// RETURN is also just a disposition in the old model; it neither deducts
	// stock nor proves that the remaining stock has passed reinspection.
	fail, err = f.svc.RecordInspection(ctx, f.tenant, f.po, PurchaseInspection{ReceiptID: receipt, POItemID: f.item, Result: "FAIL", InspectedQty: "4", DefectQty: "1", Note: "new damage"}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.ResolveInspection(ctx, f.tenant, f.po, fail.ID, "RETURN", "退货备注", buyer); err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	pass("4")
	f.check(t, in, true)
	issue, err := f.svc.ReportReceiptException(ctx, f.tenant, f.po, ReceiptException{ReceiptID: receipt, POItemID: f.item, Type: "QUALITY_DISPUTE", Qty: "1", Description: "new quality dispute"}, buyer)
	if err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	if _, err = f.svc.ResolveReceiptException(ctx, f.tenant, f.po, issue.ID, "仅补处理说明", buyer); err != nil {
		t.Fatal(err)
	}
	f.check(t, in, false)
	pass("4")
	f.check(t, in, true)
}

func TestDeliveryQualityReceivedOrderCanGetRealSupplementalInspection(t *testing.T) {
	f := newDeliveryQualityFixture(t)
	ctx := context.Background()
	op := Operator{ID: 77, Name: "Buyer"}
	// This is a persisted pre-gate order, not a new uninspected receipt.
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_orders SET status='RECEIVED' WHERE tenant_id=$1 AND id=$2`, f.tenant, f.po); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE purchase_order_items SET received_qty=qty WHERE tenant_id=$1 AND id=$2`, f.tenant, f.item); err != nil {
		t.Fatal(err)
	}
	var receipt int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO purchase_receipts(tenant_id,po_id,receipt_no,warehouse_id,operator_id,operator_name) VALUES($1,$2,'RCPT-HISTORICAL-QI',3,77,'Buyer') RETURNING id`, f.tenant, f.po).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO purchase_receipt_items(tenant_id,receipt_id,po_item_id,qty) VALUES($1,$2,$3,10)`, f.tenant, receipt, f.item); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RecordInspection(ctx, f.tenant, f.po, PurchaseInspection{ReceiptID: receipt, POItemID: f.item, Result: "PASS", InspectedQty: "10"}, Operator{ID: 88, Name: "QC"}); err != nil {
		t.Fatal(err)
	}
	// The old warehouse PASS does not substitute for a factory whole-order
	// inspection; a real supplemental task must still be requested and pass.
	f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, false)
	sources, _, err := f.svc.ListQualitySourceOrders(ctx, f.tenant, f.po, "", 1, 10, op)
	if err != nil || len(sources) != 1 || sources[0].ExistingTaskID != 0 {
		t.Fatalf("received source=%+v err=%v", sources, err)
	}
	task, err := f.svc.CreateQualityInspection(ctx, f.tenant, ApplyQualityInput{POID: f.po}, op)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "WAITING" {
		t.Fatalf("supplemental inspection should await actual QC: %+v", task)
	}
	f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, false)
	duplicate, err := f.svc.ApplyQualityInspection(ctx, f.tenant, ApplyQualityInput{POID: f.po}, op)
	if err != nil || duplicate.ID != task.ID {
		t.Fatalf("received apply retry=%+v err=%v", duplicate, err)
	}
	if _, err = f.svc.SubmitQualityRound(ctx, f.tenant, task.ID, SubmitQualityInput{Lines: []SubmitQualityLine{{TaskLineID: task.Lines[0].ID, Result: "PASS", InspectedQty: "10", QualifiedQty: "10", UnqualifiedQty: "0"}}}, Operator{ID: 88, Name: "QC"}); err != nil {
		t.Fatal(err)
	}
	f.check(t, DeliveryQualityInput{POIDs: []int64{f.po}}, true)
}
