package app

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

type manualRejectContract struct{}

func (manualRejectContract) Check(context.Context, int64) error {
	return errors.New("contract is unavailable")
}

func TestManualOrderValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*ContractHandoff)
	}{
		{"negative price", func(v *ContractHandoff) { v.FinalFreightAmount = "-1" }},
		{"price precision", func(v *ContractHandoff) { v.FinalFreightAmount = "0.001" }},
		{"date order", func(v *ContractHandoff) { v.FinalETD = "2026-09-22"; v.FinalETA = "2026-09-01" }},
		{"invalid cargo", func(v *ContractHandoff) {
			v.CargoItems = []ContractShippingCargoItem{{ProductName: "steel", Quantity: "0", UomCode: "MT"}}
		}},
		{"long reference", func(v *ContractHandoff) { v.ContractNo = strings.Repeat("x", 51); v.CustomerName = v.ContractNo }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := ContractHandoff{ContractNo: "NOT-YET-IMPORTED", FinalCurrency: "USD"}
			tc.edit(&v)
			if validateManualOrder(&v) == nil {
				t.Fatal("accepted invalid data")
			}
		})
	}
}

func TestManualOrderOptionalContractReference(t *testing.T) {
	v := ContractHandoff{ContractNo: " ", FinalCurrency: "USD"}
	if err := validateManualOrder(&v); err != nil || v.ContractNo != "" {
		t.Fatalf("blank historical reference: %v", err)
	}
}

func TestManualShippingOrderLifecycle(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("set SHIPPING_TEST_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano() / 1000
	defer func() {
		_, _ = pool.Exec(ctx, `UPDATE shipping_schedules SET contract_handoff_id=NULL WHERE tenant_id IN ($1,$2)`, tenant, tenant+1)
		_, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id IN ($1,$2)`, tenant, tenant+1)
		_, _ = pool.Exec(ctx, `DELETE FROM shipping_schedules WHERE tenant_id IN ($1,$2)`, tenant, tenant+1)
	}()
	svc := New(pool)
	op := Operator{ID: 10, Name: "historical test"}
	input := ContractHandoff{ManualOrderNo: "HISTORY-1", FinalCurrency: "USD", CustomerName: "独立历史客户", PortOfLoading: "上海", PortOfDischarge: "洛杉矶", FinalETD: "2027-02-01", FinalETA: "2027-02-15", PaymentTerms: "历史约定", Remark: "独立历史物流测试", CargoItems: []ContractShippingCargoItem{{ProductName: "钢板", Quantity: "12.5", UomCode: "MT"}}}
	first, err := svc.SaveManualShippingOrder(ctx, tenant, input, op)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContractNo != "" || first.LinkedContractID != 0 || first.ContractID != 0 || first.Status != "SCHEDULED" || first.ScheduleID == 0 || first.ApprovalInstanceID != 0 || first.ContractVerifiedAt != "" || first.PaymentRequestedAt != "" || len(first.CargoItems) != 1 {
		t.Fatalf("unexpected history flow: %+v", first)
	}
	schedule, _, err := svc.GetSchedule(ctx, tenant, first.ScheduleID)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.CustomerName != input.CustomerName || schedule.PortOfLoading != input.PortOfLoading || schedule.ContractID != nil || schedule.ContractNo != "" || schedule.ResponsibleEmployeeID != op.ID || schedule.Status != "PLANNED" || schedule.Remark != input.Remark {
		t.Fatalf("schedule input lost: %+v", schedule)
	}
	details, err := svc.GetScheduleDetails(ctx, tenant, first.ScheduleID)
	if err != nil || len(details.Route) != 2 {
		t.Fatalf("initial route: %v", err)
	}
	identities, err := svc.ScheduleIdentities(ctx, tenant, []int64{first.ScheduleID})
	if err != nil || identities[first.ScheduleID].ManualOrderNo != first.ManualOrderNo {
		t.Fatalf("history identity: %v", err)
	}
	if _, err = svc.SaveManualShippingOrder(ctx, tenant, input, op); err == nil {
		t.Fatal("duplicate historical number accepted")
	}
	if _, err = svc.SaveManualShippingOrder(ctx, tenant, first, op); err == nil {
		t.Fatal("scheduled history edited through import instead of schedule")
	}
	if _, err = svc.GetContractHandoff(ctx, tenant+1, first.ID); err == nil {
		t.Fatal("cross tenant history read")
	}
	if _, _, err = svc.GetSchedule(ctx, tenant+1, first.ScheduleID); err == nil {
		t.Fatal("cross tenant schedule read")
	}
	input.ManualOrderNo = ""
	input.ContractNo = "HISTORICAL-REFERENCE"
	input.FinalFreightAmount = "125.50"
	second, err := svc.SaveManualShippingOrder(ctx, tenant, input, op)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.ManualOrderNo == "" || second.ScheduleID == first.ScheduleID || second.LinkedContractID != 0 || second.FinalFreightAmount != "125.50" {
		t.Fatal("independent history failed")
	}
	var countBefore, countAfter int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant).Scan(&countBefore)
	input.PortOfLoading = ""
	if _, err = svc.SaveManualShippingOrder(ctx, tenant, input, op); err == nil {
		t.Fatal("missing schedule information accepted")
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant).Scan(&countAfter)
	if countAfter != countBefore {
		t.Fatal("failed history left a draft behind")
	}
	svc.UseContractGuard(manualRejectContract{})
	if err = svc.checkHandoffExecution(ctx, tenant, first.ID); err != nil {
		t.Fatal("unlinked history requires normal contract", err)
	}
}
