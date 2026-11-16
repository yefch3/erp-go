package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBackfillOnlyMetadataAndRetrySync(t *testing.T) {
	sd, fd := os.Getenv("SHIPPING_BACKFILL_DSN"), os.Getenv("PROCUREMENT_BACKFILL_DSN")
	if sd == "" {
		sd = os.Getenv("SHIPPING_TEST_DSN")
	}
	if fd == "" {
		fd = os.Getenv("PROCUREMENT_TEST_DSN")
	}
	if sd == "" || fd == "" {
		t.Skip("both backfill test databases required")
	}
	ctx := context.Background()
	s, err := pgxpool.New(ctx, sd)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f, err := pgxpool.New(ctx, fd)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		_, _ = f.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenant)
		_, _ = s.Exec(ctx, `DELETE FROM contract_shipping_handoffs WHERE tenant_id=$1`, tenant)
	}()
	var id, manualID int64
	err = s.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status,signed_contract_key,signed_contract_name,signed_contract_uploaded_at,contract_verified_at,payment_requested_at) VALUES($1,991,'BACKFILL-TEST',1,1,1,'USD','PAYMENT_REQUESTED','test.pdf','test.pdf',now(),now(),now()) RETURNING id`, tenant).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	err = s.QueryRow(ctx, `INSERT INTO contract_shipping_handoffs(tenant_id,contract_id,contract_no,contract_version_id,version_no,batch_no,currency,status,signed_contract_key,signed_contract_name,signed_contract_uploaded_at,forwarder_contract_no) VALUES($1,991,'BACKFILL-TEST',1,1,2,'USD','CONTRACT_UPLOADED','manual.pdf','manual.pdf',now(),'REAL-CONTRACT') RETURNING id`, tenant).Scan(&manualID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Exec(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,supplier_name,currency,total_amount,status,buyer_id,buyer_name,business_type,source_business_id,export_contract_no,signed_contract_key,signed_contract_name,signed_contract_uploaded_at,contract_verified_at,payment_requested_at) VALUES($1,'BACKFILL-PAYABLE',1,'Test','USD',50,'ORDERED',1,'Finance','LOGISTICS',$2,'BACKFILL-TEST','test.pdf','test.pdf',now(),now(),now())`, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	dry, err := run(ctx, s, f, tenant, 0, false)
	if err != nil || dry.ShippingChanged != 1 || dry.FinanceChanged != 1 {
		t.Fatalf("dry run %+v: %v", dry, err)
	}
	var number string
	if err = s.QueryRow(ctx, `SELECT forwarder_contract_no FROM contract_shipping_handoffs WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&number); err != nil || number != "" {
		t.Fatalf("dry run mutated source: %s %v", number, err)
	}
	applied, err := run(ctx, s, f, tenant, 0, true)
	if err != nil || len(applied.Rows) != 1 || !strings.HasPrefix(applied.Rows[0].Number, "LC-") {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	number = applied.Rows[0].Number
	if err = onlyMetadataChanged(applied.Rows[0].ShippingBefore, applied.Rows[0].ShippingAfter, "forwarder_contract_no", "forwarder_contract_no_generated"); err != nil {
		t.Fatal(err)
	}
	if err = onlyMetadataChanged(applied.Rows[0].FinanceBefore, applied.Rows[0].FinanceAfter, "business_document_no", "business_document_no_generated"); err != nil {
		t.Fatal(err)
	}
	again, err := run(ctx, s, f, tenant, 0, true)
	if err != nil || len(again.Rows) != 0 {
		t.Fatalf("repeat run changed data: %+v %v", again, err)
	}
	// Simulate a lost finance commit: the source number must be reused.
	_, err = f.Exec(ctx, `UPDATE purchase_orders SET business_document_no='',business_document_no_generated=false WHERE tenant_id=$1 AND source_business_id=$2`, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := run(ctx, s, f, tenant, 0, true)
	if err != nil || retry.ShippingChanged != 0 || retry.FinanceChanged != 1 || retry.Rows[0].Number != number {
		t.Fatalf("retry sync: %+v %v", retry, err)
	}
	var real string
	if err = s.QueryRow(ctx, `SELECT forwarder_contract_no FROM contract_shipping_handoffs WHERE tenant_id=$1 AND id=$2`, tenant, manualID).Scan(&real); err != nil || real != "REAL-CONTRACT" {
		t.Fatalf("real number changed: %q %v", real, err)
	}
}
