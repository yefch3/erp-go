package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

// 一页合同的船期状态（D2）：只给最新一班，作废的不算，班次数照实说。
func TestContractShippingSnapshot(t *testing.T) {
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
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM shipping_schedules WHERE tenant_id=$1", tenantID)
	}()

	leg := func(scheduleNo, contractNo, vessel, status, etd, eta string) {
		if _, err := pool.Exec(ctx, `INSERT INTO shipping_schedules
			(tenant_id, schedule_no, contract_no, customer_name, vessel_name, voyage_no,
			 port_of_loading, port_of_discharge, etd, eta, original_eta,
			 responsible_employee_id, status, created_by, updated_by)
			VALUES ($1,$2,$3,'客户',$4,'V1','宁波','汉堡',$5::date,$6::date,$6::date,88,$7,1,1)`,
			tenantID, scheduleNo, contractNo, vessel, etd, eta, status); err != nil {
			t.Fatal(err)
		}
	}

	// 合同 A 分两批走：一览表上只该出最新那一班（按 ETD 排），但要说共 2 班。
	leg("SC-A1", "CT-SHIP-A", "第一班船", "ARRIVED", "2026-03-01", "2026-03-20")
	leg("SC-A2", "CT-SHIP-A", "第二班船", "IN_TRANSIT", "2026-05-01", "2026-05-25")
	// 作废那一班不算数——它既不该被当成最新，也不该计进班次数。
	leg("SC-A3", "CT-SHIP-A", "取消的船", "CANCELLED", "2026-07-01", "2026-07-20")
	// 合同 B 只有一班。
	leg("SC-B1", "CT-SHIP-B", "单班船", "PLANNED", "2026-06-01", "2026-06-20")

	svc := New(pool)
	got, err := svc.ContractShippingSnapshot(ctx, tenantID,
		[]string{"CT-SHIP-A", "CT-SHIP-B", "CT-SHIP-NONE"})
	if err != nil {
		t.Fatal(err)
	}

	a, ok := got["CT-SHIP-A"]
	if !ok {
		t.Fatal("合同 A 该有船期")
	}
	if a.ScheduleNo != "SC-A2" || a.VesselName != "第二班船" {
		t.Fatalf("该出最新一班 SC-A2，实际 %s / %s", a.ScheduleNo, a.VesselName)
	}
	if a.LegCount != 2 {
		t.Fatalf("作废那班不该计进班次数，该是 2，实际 %d", a.LegCount)
	}
	if a.ETA != "2026-05-25" || a.ATA != "" {
		t.Fatalf("在途的船有 ETA、没有 ATA，实际 ETA=%q ATA=%q", a.ETA, a.ATA)
	}

	if b := got["CT-SHIP-B"]; b.ScheduleNo != "SC-B1" || b.LegCount != 1 {
		t.Fatalf("合同 B 该是一班 SC-B1，实际 %+v", b)
	}
	// 还没订舱的合同不出现——网关据此把那一格留空，而不是显示成「零班船」。
	if _, present := got["CT-SHIP-NONE"]; present {
		t.Fatal("没订舱的合同不该出现在结果里")
	}

	empty, err := svc.ContractShippingSnapshot(ctx, tenantID, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("空输入该直接返回空，实际 %+v / %v", empty, err)
	}
}
