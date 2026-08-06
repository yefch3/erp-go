package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestScheduleLifecycle runs against an explicitly selected shipping database.
// It uses an isolated tenant and removes only the rows it created.
func TestScheduleLifecycle(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("set SHIPPING_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()/1000 + 900000000
	defer cleanupShippingTenant(ctx, pool, tenantID)
	svc := New(pool)
	op := Operator{ID: 101, Name: "D1 Test"}

	input := validInput()
	input.ContractNo = "CON-D1-001"
	input.CustomerName = "D1 Customer"
	created, err := svc.CreateSchedule(ctx, tenantID, input, op, false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ScheduleNo == "" || created.Status != "PLANNED" {
		t.Fatalf("created=%+v", created)
	}

	if _, err = svc.CreateSchedule(ctx, tenantID, input, op, false); errorCode(err) != "SHIPPING_POSSIBLE_DUPLICATE" {
		t.Fatalf("duplicate code=%q err=%v", errorCode(err), err)
	}
	second, err := svc.CreateSchedule(ctx, tenantID, input, op, true)
	if err != nil || second.ScheduleNo == created.ScheduleNo {
		t.Fatalf("confirmed duplicate: second=%+v err=%v", second, err)
	}

	bad := input
	bad.ETA = "2026-08-01"
	if _, err = svc.CreateSchedule(ctx, tenantID, bad, op, false); errorCode(err) != "SHIPPING_ETA_BEFORE_ETD" {
		t.Fatalf("invalid dates code=%q err=%v", errorCode(err), err)
	}

	rows, total, page, size, err := svc.ListSchedules(ctx, tenantID, ListFilter{Keyword: "CON-D1", PortOfLoading: "Shanghai", Page: 1, PageSize: 1})
	if err != nil || len(rows) != 1 || total != 2 || page != 1 || size != 1 {
		t.Fatalf("list rows=%d total=%d page=%d size=%d err=%v", len(rows), total, page, size, err)
	}
	if rows[0].ID != second.ID {
		t.Fatalf("default sort returned id=%d, want most recently updated id=%d", rows[0].ID, second.ID)
	}
	rows, total, _, _, err = svc.ListSchedules(ctx, tenantID, ListFilter{Keyword: "CON-D1", Page: 99, PageSize: 1})
	if err != nil || len(rows) != 0 || total != 2 {
		t.Fatalf("empty page rows=%d total=%d err=%v", len(rows), total, err)
	}

	changed := input
	changed.ETD = "2026-08-11"
	changed.ETA = "2026-09-06"
	if _, err = svc.UpdateSchedule(ctx, tenantID, created.ID, changed, "", op, true); errorCode(err) != "SHIPPING_DATE_REASON_REQUIRED" {
		t.Fatalf("missing date reason code=%q err=%v", errorCode(err), err)
	}
	updated, err := svc.UpdateSchedule(ctx, tenantID, created.ID, changed, "船公司调整计划", op, true)
	if err != nil || dateText(updated.Etd) != changed.ETD {
		t.Fatalf("update=%+v err=%v", updated, err)
	}

	updated, err = svc.UpdateScheduleStatus(ctx, tenantID, created.ID, "SAILED", "已收到开船确认", op)
	if err != nil || updated.Status != "SAILED" {
		t.Fatalf("status update=%+v err=%v", updated, err)
	}
	if _, err = svc.UpdateScheduleStatus(ctx, tenantID, created.ID, "COMPLETED", "跳级", op); errorCode(err) != "SHIPPING_STATUS_TRANSITION_INVALID" {
		t.Fatalf("invalid transition code=%q err=%v", errorCode(err), err)
	}

	cancelled, err := svc.CancelSchedule(ctx, tenantID, second.ID, "客户取消订舱", op)
	if err != nil || cancelled.Status != "CANCELLED" {
		t.Fatalf("cancel=%+v err=%v", cancelled, err)
	}
	rows, total, _, _, err = svc.ListSchedules(ctx, tenantID, ListFilter{Status: "ACTIVE", Page: 1, PageSize: 20})
	if err != nil || len(rows) != 1 || total != 1 || rows[0].ID != created.ID {
		t.Fatalf("active filter rows=%d total=%d err=%v", len(rows), total, err)
	}
	rows, total, _, _, err = svc.ListSchedules(ctx, tenantID, ListFilter{Status: "ARCHIVED", Page: 1, PageSize: 20})
	if err != nil || len(rows) != 1 || total != 1 || rows[0].ID != second.ID {
		t.Fatalf("archive filter rows=%d total=%d err=%v", len(rows), total, err)
	}
	if _, err = svc.UpdateSchedule(ctx, tenantID, second.ID, input, "", op, true); errorCode(err) != "SHIPPING_FINAL_STATE" {
		t.Fatalf("edit cancelled code=%q err=%v", errorCode(err), err)
	}
	rows, total, _, _, err = svc.ListSchedules(ctx, tenantID, ListFilter{Status: "CANCELLED", ETATo: "2026-09-05", Page: 1, PageSize: 20})
	if err != nil || len(rows) != 1 || total != 1 || rows[0].ID != second.ID {
		t.Fatalf("status/date filter rows=%d total=%d err=%v", len(rows), total, err)
	}

	got, changes, err := svc.GetSchedule(ctx, tenantID, created.ID)
	if err != nil || got.Status != "SAILED" || len(changes) != 3 {
		t.Fatalf("detail=%+v changes=%d err=%v", got, len(changes), err)
	}
}

func cleanupShippingTenant(ctx context.Context, pool *pgxpool.Pool, tenantID int64) {
	_, _ = pool.Exec(ctx, "DELETE FROM shipping_documents WHERE tenant_id=$1", tenantID)
	_, _ = pool.Exec(ctx, "DELETE FROM shipping_arrival_reminders WHERE tenant_id=$1", tenantID)
	_, _ = pool.Exec(ctx, "DELETE FROM shipping_delay_events WHERE tenant_id=$1", tenantID)
	_, _ = pool.Exec(ctx, "DELETE FROM shipping_schedule_changes WHERE tenant_id=$1", tenantID)
	_, _ = pool.Exec(ctx, "UPDATE shipping_schedules SET current_route_node_id=NULL WHERE tenant_id=$1", tenantID)
	_, _ = pool.Exec(ctx, "DELETE FROM shipping_route_nodes WHERE tenant_id=$1", tenantID)
	_, _ = pool.Exec(ctx, "DELETE FROM shipping_schedules WHERE tenant_id=$1", tenantID)
}

func TestRouteAndRepeatedDelays(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("set SHIPPING_TEST_DSN to a migrated PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()/1000 + 910000000
	defer cleanupShippingTenant(ctx, pool, tenantID)
	svc := New(pool)
	op := Operator{ID: 202, Name: "Route Test"}
	created, err := svc.CreateSchedule(ctx, tenantID, validInput(), op, false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	details, err := svc.GetScheduleDetails(ctx, tenantID, created.ID)
	if err != nil || len(details.Route) != 2 || len(details.Reminders) != 1 {
		t.Fatalf("initial details route=%d reminders=%d err=%v", len(details.Route), len(details.Reminders), err)
	}
	nodes, version, err := svc.AddRouteNode(ctx, tenantID, created.ID, RouteNodeInput{
		NodeType: "TEMPORARY", PortName: "Port Klang", InsertAfterNodeID: details.Route[0].ID,
		LatestETAAt: "2026-08-22T08:00:00+08:00", LatestETDAt: "2026-08-23T18:00:00+08:00",
		Reason: "临时补给", RouteVersion: created.RouteVersion,
	}, op)
	if err != nil || len(nodes) != 3 || version != created.RouteVersion+1 {
		t.Fatalf("add node nodes=%d version=%d err=%v", len(nodes), version, err)
	}
	if nodes[1].NodeType != "TEMPORARY" {
		t.Fatalf("middle node=%+v", nodes[1])
	}
	nodes, version, err = svc.AddRouteNode(ctx, tenantID, created.ID, RouteNodeInput{NodeType: "TRANSIT", PortName: "Busan", InsertAfterNodeID: nodes[1].ID, Reason: "增加中转港", RouteVersion: version}, op)
	if err != nil || len(nodes) != 4 {
		t.Fatalf("add transit nodes=%d err=%v", len(nodes), err)
	}
	nodes, version, err = svc.ReorderRoute(ctx, tenantID, created.ID, []int64{nodes[0].ID, nodes[2].ID, nodes[1].ID, nodes[3].ID}, "调整中转顺序", version, op)
	if err != nil || nodes[1].PortName != "Busan" || version != created.RouteVersion+3 {
		t.Fatalf("reorder nodes=%+v version=%d err=%v", nodes, version, err)
	}
	_, _, _, err = svc.UpdateProgress(ctx, tenantID, created.ID, ProgressInput{RouteNodeID: nodes[3].ID, Action: "UPDATE_ETA", LatestETA: "2026-09-08", ReasonCode: "WEATHER", Reason: "天气延误", ImpactType: "LEG", FromNodeID: nodes[0].ID, ToNodeID: nodes[3].ID, RouteVersion: version}, op)
	if err != nil {
		t.Fatalf("first delay: %v", err)
	}
	out, _, delays, err := svc.UpdateProgress(ctx, tenantID, created.ID, ProgressInput{RouteNodeID: nodes[3].ID, Action: "UPDATE_ETA", LatestETA: "2026-09-10", ReasonCode: "PORT_CONGESTION", Reason: "港口拥堵", ImpactType: "PORT", AffectedNodeID: nodes[3].ID, RouteVersion: version}, op)
	if err != nil {
		t.Fatalf("second delay: %v", err)
	}
	if len(delays) != 2 || delays[0].ChangeDays != 2 || out.DelayDays != 5 || out.EtaRevision != 3 {
		t.Fatalf("delays=%+v schedule=%+v", delays, out)
	}
	// A wrong arrival can be corrected without deleting audit history. Clearing
	// the actual time rolls the node and automatically derived schedule status
	// back to their prior state.
	out, correctedNodes, _, err := svc.UpdateProgress(ctx, tenantID, created.ID, ProgressInput{
		RouteNodeID: nodes[1].ID, Action: "UPDATE_TIMES", ActualArrivalAt: "2026-08-24T10:00:00+08:00",
		Reason: "更正中转港实际到港", RouteVersion: version,
	}, op)
	if err != nil || correctedNodes[1].NodeStatus != "ARRIVED" || out.Status != "IN_TRANSIT" {
		t.Fatalf("correct arrival schedule=%+v node=%+v err=%v", out, correctedNodes[1], err)
	}
	if _, _, err = svc.RemoveRouteNode(ctx, tenantID, created.ID, nodes[1].ID, "误删已有进度港口", version, op); errorCode(err) != "SHIPPING_ROUTE_NODE_HAS_PROGRESS" {
		t.Fatalf("remove progressed node code=%q err=%v", errorCode(err), err)
	}
	out, correctedNodes, _, err = svc.UpdateProgress(ctx, tenantID, created.ID, ProgressInput{
		RouteNodeID: nodes[1].ID, Action: "UPDATE_TIMES", Reason: "撤销误录到港", RouteVersion: version,
	}, op)
	if err != nil || correctedNodes[1].NodeStatus != "PLANNED" || out.Status != "PLANNED" {
		t.Fatalf("undo arrival schedule=%+v node=%+v err=%v", out, correctedNodes[1], err)
	}
	withUnused, addedVersion, err := svc.AddRouteNode(ctx, tenantID, created.ID, RouteNodeInput{
		NodeType: "TRANSIT", PortName: "Unused Port", InsertAfterNodeID: correctedNodes[2].ID,
		Reason: "测试误加港口", RouteVersion: version,
	}, op)
	if err != nil || len(withUnused) != 5 {
		t.Fatalf("add removable node nodes=%d err=%v", len(withUnused), err)
	}
	removed, removedVersion, err := svc.RemoveRouteNode(ctx, tenantID, created.ID, withUnused[3].ID, "撤销误加港口", addedVersion, op)
	if err != nil || len(removed) != 4 || removedVersion != addedVersion+1 {
		t.Fatalf("remove node nodes=%d version=%d err=%v", len(removed), removedVersion, err)
	}
	if _, _, err = svc.RemoveRouteNode(ctx, tenantID, created.ID, removed[0].ID, "尝试移除起运港", removedVersion, op); errorCode(err) != "SHIPPING_ROUTE_TERMINAL_REMOVE_FORBIDDEN" {
		t.Fatalf("remove origin code=%q err=%v", errorCode(err), err)
	}
	details, err = svc.GetScheduleDetails(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending, cancelled := 0, 0
	for _, r := range details.Reminders {
		if r.Status == "PENDING" {
			pending++
		}
		if r.Status == "CANCELLED" {
			cancelled++
		}
	}
	if pending != 1 || cancelled != 2 {
		t.Fatalf("reminders pending=%d cancelled=%d all=%+v", pending, cancelled, details.Reminders)
	}
}
