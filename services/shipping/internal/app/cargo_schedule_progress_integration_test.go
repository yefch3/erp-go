package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCargoScheduleProgressUsesExactTenantContractAndGoods(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	var scheduleID, handoffID int64
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM shipping_schedules WHERE tenant_id=$1`, tenant)
	}()
	err = pool.QueryRow(ctx, `INSERT INTO shipping_schedules(tenant_id,schedule_no,contract_id,vessel_name,voyage_no,port_of_loading,port_of_discharge,etd,eta,original_eta,atd,status,responsible_employee_id,created_by,updated_by) VALUES($1,'CARGO-PROGRESS',44,'Test vessel','Test voyage','Origin','Destination',current_date,current_date+1,current_date+1,current_date,'SAILED',7,7,7) RETURNING id`, tenant).Scan(&scheduleID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status,schedule_id) VALUES($1,44,'CT-PROGRESS',100,1,1,'CNY','SCHEDULED',$2) RETURNING id`, tenant, scheduleID).Scan(&handoffID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO contract_shipping_handoff_cargo(tenant_id,handoff_id,contract_item_id,line_no,product_name,quantity,uom_code) VALUES($1,$2,71,1,'Test goods',10,'MT')`, tenant, handoffID); err != nil {
		t.Fatal(err)
	}
	svc := New(pool)
	row, err := svc.GetCargoScheduleProgress(ctx, tenant, scheduleID, 44, []int64{71})
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "SAILED" || row.ATD == "" || row.ATA != "" {
		t.Fatalf("wrong progress: %+v", row)
	}
	for _, tc := range []struct{ tenant, contract, line int64 }{{tenant + 1, 44, 71}, {tenant, 45, 71}, {tenant, 44, 72}} {
		if _, err := svc.GetCargoScheduleProgress(ctx, tc.tenant, scheduleID, tc.contract, []int64{tc.line}); errorCode(err) != "CARGO_SCHEDULE_NOT_FOUND" {
			t.Fatalf("unrelated reference was visible: %v", err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE shipping_schedules SET status='ARRIVED',ata=current_date+1 WHERE tenant_id=$1 AND id=$2`, tenant, scheduleID); err != nil {
		t.Fatal(err)
	}
	row, err = svc.GetCargoScheduleProgress(ctx, tenant, scheduleID, 44, []int64{71})
	if err != nil || row.Status != "ARRIVED" || row.ATA == "" {
		t.Fatalf("actual progress not refreshed: %+v %v", row, err)
	}
}
