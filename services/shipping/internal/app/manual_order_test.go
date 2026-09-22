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
		{"missing reference", func(v *ContractHandoff) { v.ContractNo = " " }},
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
		_, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id IN ($1,$2)`, tenant, tenant+1)
	}()
	svc := New(pool)
	op := Operator{ID: 10, Name: "manual test"}
	input := ContractHandoff{ManualOrderNo: "HISTORY-1", ContractNo: "CONTRACT-NOT-IN-SYSTEM", FinalCurrency: "USD", CustomerName: "历史客户", CargoItems: []ContractShippingCargoItem{{ProductName: "钢卷", Quantity: "12.5", UomCode: "MT"}, {ProductName: "钢管", Quantity: "30", UomCode: "PCS"}}}
	first, err := svc.SaveManualShippingOrder(ctx, tenant, input, op)
	if err != nil {
		t.Fatal(err)
	}
	svc.UseContractGuard(manualRejectContract{})
	if err = svc.checkHandoffExecution(ctx, tenant, first.ID); err != nil {
		t.Fatal("unlinked manual order incorrectly requires a contract", err)
	}
	if err = svc.checkHandoffExecution(ctx, tenant+1, first.ID); err == nil {
		t.Fatal("cross tenant guard bypass")
	}
	if first.ID == 0 || first.ContractID != 0 || first.Status != "DRAFT" || !first.AmountMissing || len(first.CargoItems) != 2 || first.ScheduleID != 0 || first.PaymentRequestedAt != "" {
		t.Fatalf("unexpected state: %+v", first)
	}
	if _, err = svc.SaveManualShippingOrder(ctx, tenant, input, op); err == nil {
		t.Fatal("duplicate order accepted")
	}
	second, err := svc.SaveManualShippingOrder(ctx, tenant+1, input, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GetContractHandoff(ctx, tenant, second.ID); err == nil {
		t.Fatal("cross tenant read")
	}
	first.FinalFreightAmount = "125.50"
	if _, err = svc.SaveManualShippingOrder(ctx, tenant+1, first, op); err == nil {
		t.Fatal("cross tenant edit")
	}
	first, err = svc.SaveManualShippingOrder(ctx, tenant, first, op)
	if err != nil {
		t.Fatal(err)
	}
	if first.AmountMissing || first.FinalFreightAmount != "125.50" || len(first.CargoItems) != 2 {
		t.Fatal("edit did not persist")
	}
	input.ManualOrderNo = ""
	extra, err := svc.SaveManualShippingOrder(ctx, tenant, input, op)
	if err != nil {
		t.Fatal(err)
	}
	if extra.ManualOrderNo == "" || extra.ID == first.ID {
		t.Fatal("independent order not created")
	}
	linked, linkErr := svc.LinkManualShippingContract(ctx, tenant, first.ID, 501, "CT-501", op)
	if linkErr != nil {
		t.Fatal(linkErr)
	}
	if linked.LinkedContractID != 501 || linked.ContractNo != "CT-501" || linked.ContractID != 0 || linked.Status != first.Status {
		t.Fatal("link altered workflow or failed", linked)
	}
	var original string
	if err = pool.QueryRow(ctx, `SELECT original_contract_no FROM manual_shipping_orders WHERE tenant_id=$1 AND handoff_id=$2`, tenant, first.ID).Scan(&original); err != nil || original != "CONTRACT-NOT-IN-SYSTEM" {
		t.Fatal("original reference lost", original, err)
	}
	if _, err = svc.LinkManualShippingContract(ctx, tenant+1, first.ID, 501, "CT-501", op); err == nil {
		t.Fatal("cross tenant link")
	}
	if _, err = svc.LinkManualShippingContract(ctx, tenant, first.ID, 502, "CT-502", op); err == nil {
		t.Fatal("silent relink")
	}
	linked.ContractNo = "changed"
	if _, err = svc.SaveManualShippingOrder(ctx, tenant, linked, op); err == nil {
		t.Fatal("linked number changed")
	}
	first, err = svc.LinkManualShippingContract(ctx, tenant, first.ID, 501, "CT-501", op)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListD4ContractHandoffs(ctx, tenant, "")
	if err != nil || len(rows) != 2 {
		t.Fatalf("list: %v %v", len(rows), err)
	}
	_, err = pool.Exec(ctx, `UPDATE contract_shipping_handoffs SET status='PENDING_APPROVAL' WHERE tenant_id=$1 AND id=$2`, tenant, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SaveManualShippingOrder(ctx, tenant, first, op); err == nil {
		t.Fatal("editing approved flow accepted")
	}
	input.CargoItems[0].Quantity = "invalid"
	if _, err = svc.SaveManualShippingOrder(ctx, tenant, input, op); err == nil {
		t.Fatal("invalid row accepted")
	}
}
