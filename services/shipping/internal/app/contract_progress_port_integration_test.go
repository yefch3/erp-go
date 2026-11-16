package app

import (
	"reflect"
	"testing"
)

func TestContractDispatchProgressCountsOnlyPhysicalMovements(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	schedules := deliverySchedules(t, ctx, pool, tenant, svc, 4)
	facts, err := svc.ContractDispatchProgress(ctx, tenant, 41)
	if err != nil || len(facts) != 0 {
		t.Fatalf("plans counted as dispatched: %+v %v", facts, err)
	}
	for i, status := range []string{"SAILED", "COMPLETED", "CANCELLED", "CANCELLED"} {
		_, err = pool.Exec(ctx, `UPDATE shipping_schedules SET status=$3,atd=CASE WHEN $4::boolean THEN current_date ELSE NULL END WHERE tenant_id=$1 AND id=$2`, tenant, schedules[i].ID, status, i < 2 || i == 3)
		if err != nil {
			t.Fatal(err)
		}
	}
	facts, err = svc.ContractDispatchProgress(ctx, tenant, 41)
	if err != nil || len(facts) != 1 || facts[0].Quantity != "30.0000" {
		t.Fatalf("completed/actually departed facts: %+v %v", facts, err)
	}
	foreign, err := svc.ContractDispatchProgress(ctx, tenant+1, 41)
	if err != nil || len(foreign) != 0 {
		t.Fatal("tenant leaked", err)
	}
}
func TestScheduleKeepsSelectedCanonicalPorts(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	_, err := pool.Exec(ctx, `INSERT INTO contract_shipping_handoffs(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status,port_of_loading,port_of_discharge) VALUES($1,41,'CT-PORTS',42,1,1,'USD','PAYMENT_REQUESTED','上海','洛杉矶')`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var handoff int64
	if err = pool.QueryRow(ctx, `SELECT id FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant).Scan(&handoff); err != nil {
		t.Fatal(err)
	}
	in := validInput()
	in.ContractHandoffID = handoff
	in.PortOfDischarge = "天津"
	in.DischargePortID = 3
	in.DischargePortCode = "CNTNJ"
	in.DischargePortTimezone = "Asia/Shanghai"
	schedule, err := svc.CreateSchedule(ctx, tenant, in, Operator{ID: 1, Name: "Test"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.PortOfDischarge != "天津" || schedule.DischargePortCode != "CNTNJ" {
		t.Fatal("contract snapshot overwrote selected port", schedule)
	}
}
func TestPortMetadataCorrectionPreservesCompletedMovement(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	schedule := deliverySchedules(t, ctx, pool, tenant, svc, 1)[0]
	_, err := pool.Exec(ctx, `UPDATE shipping_schedules SET port_of_discharge='洛杉矶',discharge_port_id=3,discharge_port_code='CNTNJ',discharge_port_timezone='Asia/Shanghai',status='COMPLETED',atd=current_date,ata=current_date+1 WHERE tenant_id=$1 AND id=$2`, tenant, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE shipping_route_nodes SET port_name='洛杉矶',port_id=3,port_code='CNTNJ',timezone='Asia/Shanghai',actual_arrival_at='2026-11-15T20:00:00Z' WHERE tenant_id=$1 AND schedule_id=$2 AND node_type='DESTINATION'`, tenant, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.GetScheduleDetails(ctx, tenant, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := PortMetadataCorrection{Terminal: "DESTINATION", Name: "洛杉矶", PortID: 99, Code: "USLAX", Timezone: "America/Los_Angeles", Reason: "纠正测试主数据", RouteVersion: before.Schedule.RouteVersion}
	out, err := svc.CorrectPortMetadata(ctx, tenant, schedule.ID, input, Operator{ID: 1, Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "COMPLETED" || !reflect.DeepEqual(out.Atd, before.Schedule.Atd) || dateText(out.Ata) != "2026-11-15" || out.DischargePortCode != "USLAX" || out.DischargePortTimezone != "America/Los_Angeles" {
		t.Fatal("correction changed progress", out)
	}
	after, err := svc.GetScheduleDetails(ctx, tenant, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Route[1].ActualArrivalAt, before.Route[1].ActualArrivalAt) || after.Route[1].PortCode != "USLAX" || len(after.Changes) != len(before.Changes)+1 {
		t.Fatal("timestamps or audit not preserved")
	}
	// Retrying an already-correct repair does not manufacture a new route event.
	input.RouteVersion = out.RouteVersion
	retried, err := svc.CorrectPortMetadata(ctx, tenant, schedule.ID, input, Operator{ID: 1, Name: "Test"})
	if err != nil || retried.RouteVersion != out.RouteVersion {
		t.Fatal("idempotent repair changed route version", err)
	}
	input.RouteVersion = before.Schedule.RouteVersion
	if _, err = svc.CorrectPortMetadata(ctx, tenant, schedule.ID, input, Operator{ID: 1, Name: "Test"}); errorCode(err) != "SHIPPING_ROUTE_VERSION_CONFLICT" {
		t.Fatal("stale correction was accepted", err)
	}
	input.Name = "另一个港口"
	input.RouteVersion = out.RouteVersion
	if _, err = svc.CorrectPortMetadata(ctx, tenant, schedule.ID, input, Operator{ID: 1, Name: "Test"}); err == nil {
		t.Fatal("physical port replacement accepted")
	}
	input.Name = "洛杉矶"
	if _, err = svc.CorrectPortMetadata(ctx, tenant+1, schedule.ID, input, Operator{ID: 1, Name: "Test"}); err == nil {
		t.Fatal("cross tenant correction")
	}
}
