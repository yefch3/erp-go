package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScheduleIdentityReminderPreservesEventAndSecondaryNumber(t *testing.T) {
	identity := ScheduleIdentity{ContractNo: "CT-IDENTITY", BatchNo: 2, ScheduleNo: "SCH-INTERNAL"}
	content := identity.ReminderContent("船期编号：SCH-INTERNAL；合同编号：CT-IDENTITY；港口：上海；预计离港（ETD）：2026-11-20")
	if !strings.HasPrefix(content, "CT-IDENTITY · 第 2 批；港口：上海") || !strings.HasSuffix(content, "船期编号：SCH-INTERNAL") || !strings.Contains(content, "2026-11-20") {
		t.Fatal(content)
	}
	if (ScheduleIdentity{ContractNo: "CT-LEGACY"}).Label() != "CT-LEGACY · 批次未关联" {
		t.Fatal("legacy batch was invented")
	}
	if (ScheduleIdentity{}).Label() != "未关联合同 · 批次未关联" {
		t.Fatal("manual schedule must not invent contract")
	}
}

func TestScheduleIdentitiesReadActualBatchesAndRespectTenant(t *testing.T) {
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
	defer func() {
		_, _ = pool.Exec(ctx, `UPDATE shipping_schedules SET contract_handoff_id=NULL WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM shipping_schedules WHERE tenant_id=$1`, tenant)
	}()
	ids := []int64{}
	for _, batch := range []int32{2, 3, 0} {
		var id int64
		err = pool.QueryRow(ctx, `INSERT INTO shipping_schedules(tenant_id,schedule_no,contract_id,contract_no,vessel_name,voyage_no,port_of_loading,port_of_discharge,etd,eta,original_eta,responsible_employee_id,created_by,updated_by) VALUES($1,$2,44,'CT-IDENTITY','Vessel','Voyage','Origin','Destination',current_date,current_date+2,current_date+2,7,7,7) RETURNING id`, tenant, "IDENTITY-"+string(rune('0'+batch))).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if batch > 0 {
			var handoffID int64
			err = pool.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status,schedule_id) VALUES($1,44,'CT-IDENTITY',100,1,$2,'USD','SCHEDULED',$3) RETURNING id`, tenant, batch, id).Scan(&handoffID)
			if err != nil {
				t.Fatal(err)
			}
			// Cover both explicit current links and legacy reverse links.
			if batch == 2 {
				_, err = pool.Exec(ctx, `UPDATE shipping_schedules SET contract_handoff_id=$1 WHERE tenant_id=$2 AND id=$3`, handoffID, tenant, id)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	svc := New(pool)
	rows, err := svc.ScheduleIdentities(ctx, tenant, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[ids[0]].BatchNo != 2 || rows[ids[1]].BatchNo != 3 || rows[ids[2]].BatchNo != 0 {
		t.Fatalf("wrong batch mapping: %+v", rows)
	}
	foreign, err := svc.ScheduleIdentities(ctx, tenant+1, ids)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("tenant isolation: %+v %v", foreign, err)
	}
}
