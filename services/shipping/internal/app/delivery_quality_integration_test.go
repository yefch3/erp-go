package app

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
	"github.com/shopspring/decimal"
)

type deliveryQualityStub struct {
	mu         sync.Mutex
	err        error
	contractID int64
	lines      []DeliveryQualityLine
	limit      string
}

func (g *deliveryQualityStub) Check(_ context.Context, contractID int64, lines []DeliveryQualityLine) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.contractID, g.lines = contractID, append([]DeliveryQualityLine(nil), lines...)
	if g.err != nil {
		return g.err
	}
	if g.limit != "" {
		for _, line := range lines {
			if decimal.RequireFromString(line.Qty).GreaterThan(decimal.RequireFromString(g.limit)) {
				return apierr.Conflict("QUALITY_QTY_NOT_PASSED", "已合格数量不足以覆盖累计交货")
			}
		}
	}
	return nil
}

func deliveryTestDB(t *testing.T) (context.Context, *pgxpool.Pool, int64) {
	t.Helper()
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tenant := time.Now().UnixNano()
	t.Cleanup(func() {
		cleanupShippingTenant(ctx, pool, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant)
		pool.Close()
	})
	return ctx, pool, tenant
}

func deliverySchedules(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant int64, svc *Service, count int) []store.ShippingSchedule {
	t.Helper()
	svc.UseContractGuard(workflowAllowedContract{})
	event := ContractEffective{ContractID: 41, ContractNo: "CT-QUALITY", VersionID: 42, VersionNo: 1, CustomerID: 1, CustomerName: "质检客户"}
	for i := 0; i < count; i++ {
		event.Shipments = append(event.Shipments, ContractEffectiveShipment{BatchNo: int32(i + 1), Currency: "USD", FreightAmount: "0", PortOfLoading: "Shanghai", PortOfDischarge: "Hamburg", AllocationStatus: "ALLOCATED", Items: []ContractEffectiveItem{{ContractItemID: 101, LineNo: 1, ProductName: "钢材", Qty: "10", UomCode: "MT"}}})
	}
	if err := svc.HandoffsFromContract(ctx, tenant, event, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT id FROM contract_shipping_handoffs WHERE tenant_id=$1 ORDER BY batch_no`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var schedules []store.ShippingSchedule
	for _, id := range ids {
		if _, err := pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET status='PAYMENT_REQUESTED' WHERE tenant_id=$1 AND id=$2`, tenant, id); err != nil {
			t.Fatal(err)
		}
		in := validInput()
		in.ContractHandoffID = id
		schedule, err := svc.CreateSchedule(ctx, tenant, in, Operator{ID: 1, Name: "物流"}, true)
		if err != nil {
			t.Fatal(err)
		}
		schedules = append(schedules, schedule)
	}
	return schedules
}

// Legacy lifecycle fixtures now explicitly carry passed, linked goods. No
// production nil-guard bypass is used to preserve their unrelated assertions.
func attachPassedDeliveryFixture(t *testing.T, ctx context.Context, svc *Service, pool *pgxpool.Pool, tenant, scheduleID int64) {
	t.Helper()
	current, err := svc.q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenant, ID: scheduleID})
	if err != nil {
		t.Fatal(err)
	}
	event := ContractEffective{ContractID: scheduleID + 100000, ContractNo: current.ContractNo, VersionID: 1, VersionNo: 1, Shipments: []ContractEffectiveShipment{{BatchNo: 1, Currency: "USD", FreightAmount: "0", AllocationStatus: "ALLOCATED", Items: []ContractEffectiveItem{{ContractItemID: 101, LineNo: 1, ProductName: "已合格测试货物", Qty: "100", UomCode: "MT"}}}}}
	if err := svc.HandoffsFromContract(ctx, tenant, event, func(context.Context, pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var handoff int64
	if err := pool.QueryRow(ctx, `SELECT id FROM contract_shipping_handoffs WHERE tenant_id=$1 AND contract_id=$2`, tenant, event.ContractID).Scan(&handoff); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE shipping_schedules SET contract_id=$3,contract_handoff_id=$4 WHERE tenant_id=$1 AND id=$2`, tenant, scheduleID, event.ContractID, handoff); err != nil {
		t.Fatal(err)
	}
	svc.UseDeliveryQualityGuard(&deliveryQualityStub{})
	svc.UseContractGuard(workflowAllowedContract{})
}

func TestQualityGateAllowsPlanningButBlocksEveryActualDispatchPath(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	// Both schedules and planned dates can be created without a checker or QC.
	schedule := deliverySchedules(t, ctx, pool, tenant, svc, 1)[0]
	op := Operator{ID: 1, Name: "物流"}
	details, err := svc.GetScheduleDetails(ctx, tenant, schedule.ID)
	if err != nil {
		t.Fatal(err)
	}
	origin, destination := details.Route[0], details.Route[1]
	input := validInput()
	input.ETD = "2026-08-12"
	input.ETA = "2026-09-07"
	if _, err := svc.UpdateSchedule(ctx, tenant, schedule.ID, input, "提前调整计划", op, true); err != nil {
		t.Fatalf("planning blocked: %v", err)
	}
	baseHistory := 0
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM shipping_schedule_changes WHERE tenant_id=$1 AND schedule_id=$2`, tenant, schedule.ID).Scan(&baseHistory); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"未质检", "待检", "质检中", "不合格", "复检中", "质检服务不可用"} {
		t.Run(state, func(t *testing.T) {
			guard := &deliveryQualityStub{err: apierr.Conflict("QUALITY_DELIVERY_BLOCKED", state+"，不能交货")}
			if state == "质检服务不可用" {
				guard.err = errors.New("RPC unavailable")
			}
			svc.UseDeliveryQualityGuard(guard)
			if _, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{RouteNodeID: origin.ID, RouteVersion: schedule.RouteVersion, Action: "DEPART", ActualTime: "2026-08-12T10:00:00Z", Reason: "确认开船"}, op); err == nil {
				t.Fatal("unpassed goods sailed")
			}
			if _, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{RouteNodeID: origin.ID, RouteVersion: schedule.RouteVersion, Action: "UPDATE_TIMES", LatestETDAt: "2026-08-12T00:00:00Z", ActualDepartureAt: "2026-08-12T10:00:00Z", Reason: "录入实际时间"}, op); err == nil {
				t.Fatal("actual-time edit bypassed inspection")
			}
			if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "SAILED", "旧状态入口", op); err == nil {
				t.Fatal("status bypassed inspection")
			}
			if _, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{RouteNodeID: destination.ID, RouteVersion: schedule.RouteVersion, Action: "ARRIVE", ActualTime: "2026-09-07T10:00:00Z", Reason: "跳过离港直接到港"}, op); err == nil {
				t.Fatal("arrival bypassed inspection")
			}
			got, _, err := svc.GetSchedule(ctx, tenant, schedule.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "PLANNED" || got.Atd.Valid || got.Ata.Valid {
				t.Fatalf("blocked dispatch changed schedule: %+v", got)
			}
			var history int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM shipping_schedule_changes WHERE tenant_id=$1 AND schedule_id=$2`, tenant, schedule.ID).Scan(&history); err != nil || history != baseHistory {
				t.Fatalf("blocked dispatch added history: %d/%d %v", history, baseHistory, err)
			}
		})
	}
	svc.UseDeliveryQualityGuard(nil)
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "SAILED", "缺少质检核验", op); errorCode(err) != "QUALITY_CHECK_UNAVAILABLE" {
		t.Fatalf("missing guard: %v", err)
	}
	guard := &deliveryQualityStub{}
	svc.UseDeliveryQualityGuard(guard)
	got, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{RouteNodeID: origin.ID, RouteVersion: schedule.RouteVersion, Action: "DEPART", ActualTime: "2026-08-12T10:00:00Z", Reason: "质检全部合格后开船"}, op)
	if err != nil || got.Status != "SAILED" {
		t.Fatalf("passed dispatch: %+v %v", got, err)
	}
	if guard.contractID != 41 || len(guard.lines) != 1 || guard.lines[0].ContractItemID != 101 || guard.lines[0].Qty != "10.0000" {
		t.Fatalf("wrong quality target: %+v", guard)
	}
	// Arrival and completion maintain an already dispatched voyage. They must
	// remain available even when the quality service is temporarily offline.
	svc.UseDeliveryQualityGuard(nil)
	arrived, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{RouteNodeID: destination.ID, RouteVersion: schedule.RouteVersion, Action: "ARRIVE", ActualTime: "2026-09-07T10:00:00Z", Reason: "已开船货物到港"}, op)
	if err != nil || arrived.Status != "ARRIVED" {
		t.Fatalf("existing voyage arrival blocked: %v", err)
	}
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "COMPLETED", "已到港后结案", op); err != nil {
		t.Fatalf("existing voyage completion blocked: %v", err)
	}
}

func TestQualityGateDoesNotTreatOriginOrSkippedPortsAsDispatch(t *testing.T) {
	for _, portAction := range []struct{ port, action string }{
		{"ORIGIN", "APPROACH"}, {"ORIGIN", "ARRIVE"}, {"ORIGIN", "SKIP"},
		{"TRANSIT", "SKIP"}, {"DESTINATION", "SKIP"},
	} {
		for _, correction := range []string{"UPDATE_TIMES", "REMOVE_PORT"} {
			t.Run(portAction.port+"_"+portAction.action+"_"+correction, func(t *testing.T) {
				ctx, pool, tenant := deliveryTestDB(t)
				svc := New(pool)
				schedule := deliverySchedules(t, ctx, pool, tenant, svc, 1)[0]
				svc.UseDeliveryQualityGuard(&deliveryQualityStub{err: apierr.Conflict("QUALITY_DELIVERY_BLOCKED", "未质检，不能交货")})
				op := Operator{ID: 1, Name: "物流"}
				details, err := svc.GetScheduleDetails(ctx, tenant, schedule.ID)
				if err != nil {
					t.Fatal(err)
				}
				origin, destination := details.Route[0], details.Route[1]
				nodes, version, err := svc.AddRouteNode(ctx, tenant, schedule.ID, RouteNodeInput{
					NodeType: "TRANSIT", PortName: "Busan", InsertAfterNodeID: origin.ID,
					Reason: "提前规划中转", RouteVersion: schedule.RouteVersion,
				}, op)
				if err != nil {
					t.Fatal(err)
				}
				target := map[string]int64{"ORIGIN": origin.ID, "TRANSIT": nodes[1].ID, "DESTINATION": destination.ID}[portAction.port]
				nodes, version, err = svc.AddRouteNode(ctx, tenant, schedule.ID, RouteNodeInput{
					NodeType: "TEMPORARY", PortName: "Unused Port", InsertAfterNodeID: origin.ID,
					Reason: "提前规划临时港", RouteVersion: version,
				}, op)
				if err != nil {
					t.Fatal(err)
				}
				unused := nodes[1].ID
				got, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{
					RouteNodeID: target, RouteVersion: version, Action: portAction.action,
					ActualTime: "2026-08-10T10:00:00Z", Reason: "装货前维护港口计划",
				}, op)
				if err != nil || got.Status != "PLANNED" {
					t.Fatalf("pre-dispatch port action: status=%s err=%v", got.Status, err)
				}
				if correction == "UPDATE_TIMES" {
					got, _, _, err = svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{
						RouteNodeID: destination.ID, RouteVersion: version, Action: "UPDATE_TIMES",
						LatestETAAt: "2026-09-08T00:00:00Z", Reason: "只调整预计到港时间",
					}, op)
				} else {
					_, version, err = svc.RemoveRouteNode(ctx, tenant, schedule.ID, unused, "取消未使用临时港", version, op)
					if err == nil {
						got, _, err = svc.GetSchedule(ctx, tenant, schedule.ID)
					}
				}
				if err != nil || got.Status != "PLANNED" || got.Atd.Valid || got.Ata.Valid {
					t.Fatalf("planning manufactured dispatch: status=%s ATD=%v ATA=%v err=%v", got.Status, got.Atd.Valid, got.Ata.Valid, err)
				}
				var before int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM shipping_schedule_changes WHERE tenant_id=$1 AND schedule_id=$2`, tenant, schedule.ID).Scan(&before); err != nil {
					t.Fatal(err)
				}
				for _, actual := range []ProgressInput{
					{RouteNodeID: origin.ID, Action: "DEPART", ActualTime: "2026-08-12T10:00:00Z"},
					{RouteNodeID: destination.ID, Action: "ARRIVE", ActualTime: "2026-09-08T10:00:00Z"},
					{RouteNodeID: destination.ID, Action: "UPDATE_TIMES", LatestETAAt: "2026-09-08T00:00:00Z", ActualArrivalAt: "2026-09-08T10:00:00Z"},
				} {
					actual.RouteVersion, actual.Reason = version, "尝试实际交货"
					if _, _, _, err := svc.UpdateProgress(ctx, tenant, schedule.ID, actual, op); errorCode(err) != "QUALITY_DELIVERY_BLOCKED" {
						t.Fatalf("actual %s bypassed inspection: %v", actual.Action, err)
					}
				}
				var after int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM shipping_schedule_changes WHERE tenant_id=$1 AND schedule_id=$2`, tenant, schedule.ID).Scan(&after); err != nil || after != before {
					t.Fatalf("blocked actions changed history: before=%d after=%d err=%v", before, after, err)
				}
				svc.UseDeliveryQualityGuard(&deliveryQualityStub{})
				if got, _, _, err = svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{
					RouteNodeID: origin.ID, RouteVersion: version, Action: "DEPART",
					ActualTime: "2026-08-12T10:00:00Z", Reason: "补检合格后实际离港",
				}, op); err != nil || got.Status != "SAILED" {
					t.Fatalf("passed departure blocked: status=%s err=%v", got.Status, err)
				}
				svc.UseDeliveryQualityGuard(nil)
				if got, _, _, err = svc.UpdateProgress(ctx, tenant, schedule.ID, ProgressInput{
					RouteNodeID: destination.ID, RouteVersion: version, Action: "ARRIVE",
					ActualTime: "2026-09-08T10:00:00Z", Reason: "已发运货物正常到港",
				}, op); err != nil || got.Status != "ARRIVED" {
					t.Fatalf("dispatched arrival blocked: status=%s err=%v", got.Status, err)
				}
			})
		}
	}
}

func TestQualityGateSerializesBatchQuantities(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	schedules := deliverySchedules(t, ctx, pool, tenant, svc, 2)
	guard := &deliveryQualityStub{limit: "10"}
	svc.UseDeliveryQualityGuard(guard)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, schedule := range schedules {
		go func(id int64) {
			<-start
			_, err := svc.UpdateScheduleStatus(ctx, tenant, id, "SAILED", "并发两批交货", Operator{ID: 1, Name: "物流"})
			results <- err
		}(schedule.ID)
	}
	close(start)
	passed, blocked := 0, 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			passed++
		} else if errorCode(err) == "QUALITY_QTY_NOT_PASSED" {
			blocked++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || blocked != 1 {
		t.Fatalf("one passed quantity reused across batches: passed=%d blocked=%d", passed, blocked)
	}
	guard.limit = "20"
	for _, schedule := range schedules {
		got, _, err := svc.GetSchedule(ctx, tenant, schedule.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "PLANNED" {
			if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedule.ID, "SAILED", "补检合格后交货", Operator{ID: 1, Name: "物流"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The legacy status entry records SAILED without ATD. Its audit evidence
	// still proves this is post-dispatch maintenance, not a new shipment.
	svc.UseDeliveryQualityGuard(nil)
	for _, next := range []string{"IN_TRANSIT", "ARRIVED", "COMPLETED"} {
		if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedules[0].ID, next, "已发运后的正常维护", Operator{ID: 1, Name: "物流"}); err != nil {
			t.Fatalf("legacy sailed maintenance blocked at %s: %v", next, err)
		}
	}
}

func TestQualityGateCarriesShippedQuantityAcrossContractVersions(t *testing.T) {
	ctx, pool, tenant := deliveryTestDB(t)
	svc := New(pool)
	schedules := deliverySchedules(t, ctx, pool, tenant, svc, 2)
	guard := &deliveryQualityStub{limit: "10"}
	svc.UseDeliveryQualityGuard(guard)
	op := Operator{ID: 1, Name: "物流"}
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedules[0].ID, "SAILED", "旧版本已发十吨", op); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contract_shipping_handoff_cargo SET contract_item_id=0 WHERE tenant_id=$1 AND handoff_id=$2`, tenant, *schedules[0].ContractHandoffID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedules[1].ID, "SAILED", "历史货物缺少关联", op); errorCode(err) != "QUALITY_DELIVERY_HISTORY_REQUIRED" {
		t.Fatalf("unverifiable old cargo ignored: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contract_shipping_handoff_cargo SET contract_item_id=101 WHERE tenant_id=$1 AND handoff_id=$2`, tenant, *schedules[0].ContractHandoffID); err != nil {
		t.Fatal(err)
	}
	secondHandoff := *schedules[1].ContractHandoffID
	if _, err := pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET contract_version_id=43,version_no=2 WHERE tenant_id=$1 AND id=$2`, tenant, secondHandoff); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE contract_shipping_handoff_cargo SET contract_item_id=202 WHERE tenant_id=$1 AND handoff_id=$2`, tenant, secondHandoff); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedules[1].ID, "SAILED", "新版本尝试复用十吨合格量", op); errorCode(err) != "QUALITY_QTY_NOT_PASSED" {
		t.Fatalf("old version quantity lost: %v", err)
	}
	if len(guard.lines) != 1 || guard.lines[0].ContractItemID != 202 || guard.lines[0].Qty != "20.0000" {
		t.Fatalf("wrong cross-version quantity: %+v", guard.lines)
	}
	// Two identical new lines cannot both inherit the same historical amount.
	if _, err := pool.Exec(ctx, `INSERT INTO contract_shipping_handoff_cargo (tenant_id,handoff_id,contract_item_id,line_no,product_code,product_name,specification,quantity,uom_code,remark)
	 SELECT tenant_id,handoff_id,303,2,product_code,product_name,specification,5,uom_code,remark FROM contract_shipping_handoff_cargo WHERE tenant_id=$1 AND handoff_id=$2 AND contract_item_id=202`, tenant, secondHandoff); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedules[1].ID, "SAILED", "新版本同规格多行", op); errorCode(err) != "QUALITY_VERSION_ALLOCATION_REQUIRED" {
		t.Fatalf("ambiguous old quantity silently mapped: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM contract_shipping_handoff_cargo WHERE tenant_id=$1 AND handoff_id=$2 AND contract_item_id=303`, tenant, secondHandoff); err != nil {
		t.Fatal(err)
	}
	guard.limit = "20"
	if _, err := svc.UpdateScheduleStatus(ctx, tenant, schedules[1].ID, "SAILED", "新增货物复检合格", op); err != nil {
		t.Fatal(err)
	}
}
