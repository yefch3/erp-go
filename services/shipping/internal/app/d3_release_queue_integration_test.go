package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestD3ReleasedContractStopsBeforeShippingSchedule(t *testing.T) {
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
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenantID) }()
	svc := New(pool)
	claim := func(context.Context, pgx.Tx) error { return nil }
	err = svc.HandoffsFromContract(ctx, tenantID, ContractEffective{ContractID: 32001, ContractNo: "CT-D3-SHIP", VersionID: 320011, VersionNo: 1, CustomerID: 1, CustomerName: "D3 Customer", Shipments: []ContractEffectiveShipment{{BatchNo: 1, Currency: "USD", FreightAmount: "800", CarrierForwarder: "Presales reference"}}, Items: []ContractEffectiveItem{{LineNo: 1, ContractItemID: 51001, ProductCode: "CRC", ProductName: "冷轧卷", Spec: "Q235 / 1.2×1250mm", Qty: "10", UomCode: "MT", Remark: "防潮包装"}}}, claim)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenantID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "WAITING_REQUOTE" {
		t.Fatalf("status=%s", status)
	}
	var cargoCount int
	var productName, specification, quantity, uom string
	if err = pool.QueryRow(ctx, `SELECT count(*),max(product_name),max(specification),max(quantity::text),max(uom_code) FROM contract_shipping_handoff_cargo WHERE tenant_id=$1`, tenantID).Scan(&cargoCount, &productName, &specification, &quantity, &uom); err != nil {
		t.Fatal(err)
	}
	if cargoCount != 1 || productName != "冷轧卷" || specification != "Q235 / 1.2×1250mm" || quantity != "10.0000" || uom != "MT" {
		t.Fatalf("cargo snapshot mismatch: count=%d product=%q spec=%q qty=%q uom=%q", cargoCount, productName, specification, quantity, uom)
	}
	var changed int
	if err = pool.QueryRow(ctx, `WITH changed AS (UPDATE contract_shipping_handoffs SET status='SCHEDULED' WHERE tenant_id=$1 AND status='PENDING' RETURNING 1) SELECT count(*) FROM changed`, tenantID).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatal("D3 task crossed the shipping-schedule fence")
	}
}

func TestBatchSchedulesAndRetries(t *testing.T) {
	if os.Getenv("SHIPPING_TEST_DSN") == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, os.Getenv("SHIPPING_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		cleanupShippingTenant(ctx, pool, tenant)
		_, _ = pool.Exec(ctx, "DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1", tenant)
	}()
	svc := New(pool)
	var ids []int64
	for _, batch := range []int{1, 2} {
		var id int64
		err = pool.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs (tenant_id,contract_id,contract_no,contract_version_id,version_no,customer_id,customer_name,batch_no,currency,freight_amount,port_of_loading,port_of_discharge,status,final_forwarder_id,final_forwarder_name,final_currency,final_freight_amount) VALUES ($1,101,'AUDIT-TWO-BATCH',1001,1,1,'Audit customer',$2,'USD',100,'Shanghai','Los Angeles','PAYMENT_REQUESTED',1,'Audit forwarder','USD',100) RETURNING id`, tenant, batch).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	var scheduleIDs []int64
	for i, id := range ids {
		in := validInput()
		in.ContractHandoffID = id
		type result struct {
			id  int64
			err error
		}
		results := make(chan result, 4)
		for range 4 {
			go func() {
				created, e := svc.CreateSchedule(ctx, tenant, in, Operator{ID: 1, Name: "Audit"}, false)
				results <- result{id: created.ID, err: e}
			}()
		}
		var firstID int64
		for range 4 {
			r := <-results
			if r.err != nil {
				t.Errorf("batch %d concurrent creation failed: %v", i+1, r.err)
				continue
			}
			if firstID != 0 && firstID != r.id {
				t.Errorf("batch %d created different schedules: %d and %d", i+1, firstID, r.id)
			}
			firstID = r.id
		}
		if t.Failed() {
			t.FailNow()
		}
		again, e := svc.CreateSchedule(ctx, tenant, in, Operator{ID: 1, Name: "Audit"}, false)
		if e != nil || again.ID != firstID {
			t.Fatalf("retry batch %d: %v", i+1, e)
		}
		scheduleIDs = append(scheduleIDs, firstID)
		edit := validInput()
		edit.ContractID = 0
		edit.BookingNo = "BOOK-RETRY"
		updated, e := svc.UpdateSchedule(ctx, tenant, firstID, edit, "补充订舱资料", Operator{ID: 1, Name: "Audit"}, true)
		if e != nil || updated.ContractID == nil || *updated.ContractID != 101 || updated.ContractNo != "AUDIT-TWO-BATCH" {
			t.Fatalf("basic edit lost handoff contract: %+v err=%v", updated, e)
		}
	}
	if scheduleIDs[0] == scheduleIDs[1] {
		t.Fatal("different batches share the same schedule")
	}
}

func TestShippingVerificationRetryPreservesAuditAndSchedule(t *testing.T) {
	dsn := os.Getenv("SHIPPING_TEST_DSN")
	if dsn == "" {
		t.Skip("SHIPPING_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1", tenant) }()
	var id int64
	err = pool.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs (tenant_id,contract_id,contract_no,contract_version_id,version_no,customer_id,customer_name,batch_no,currency,freight_amount,port_of_loading,port_of_discharge,status,signed_contract_key) VALUES ($1,101,'AUDIT-RETRY',1001,1,1,'Audit customer',1,'USD',100,'Shanghai','Los Angeles','CONTRACT_UPLOADED','test.pdf') RETURNING id`, tenant).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool)
	first, err := svc.VerifyShippingContract(ctx, tenant, id, Operator{ID: 1, Name: "First verifier"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.VerifyShippingContract(ctx, tenant, id, Operator{ID: 2, Name: "Retry verifier"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ContractVerifiedAt != again.ContractVerifiedAt || again.ContractVerifiedByName != "First verifier" {
		t.Fatal("retry overwrote audit")
	}
	if _, err = pool.Exec(ctx, "UPDATE contract_shipping_handoffs SET status='SCHEDULED' WHERE tenant_id=$1 AND id=$2", tenant, id); err != nil {
		t.Fatal(err)
	}
	again, err = svc.VerifyShippingContract(ctx, tenant, id, Operator{ID: 2})
	if err != nil || again.Status != "SCHEDULED" {
		t.Fatal("retry changed scheduled status", err)
	}
}
