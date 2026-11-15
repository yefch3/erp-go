package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestNormalizeHistoricalShippingContext(t *testing.T) {
	for _, tc := range []struct {
		contract, port string
		valid          bool
	}{
		{"", "", true}, {"  CT-1  ", " 鹿特丹 ", true}, {strings.Repeat("合", 50), strings.Repeat("港", 200), true},
		{strings.Repeat("合", 51), "", false}, {"", strings.Repeat("港", 201), false},
	} {
		contract, port, err := normalizeHistoricalShippingContext(tc.contract, tc.port)
		if (err == nil) != tc.valid {
			t.Fatalf("validation=%v", err)
		}
		if tc.valid && (contract != strings.TrimSpace(tc.contract) || port != strings.TrimSpace(tc.port)) {
			t.Fatal("not normalized")
		}
	}
}

func TestHistoricalShippingContextLifecycleAndIsolation(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"historical_purchase_orders", "purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1 OR tenant_id=$2", tenant, tenant+1)
		}
	}()
	buyer := Operator{ID: 201, Name: "Buyer"}
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Scopes: &listAccess{}})
	in := HistoricalOrderInput{PONo: "HISTORY-CONTEXT", OriginalDate: "2026-11-01", Currency: "USD", Lines: []HistoricalOrderLine{{ProductName: "Steel", Spec: "same spec", Qty: "10", UOM: "MT", UnitPrice: "123"}}}
	draft, err := svc.SaveHistoricalOrder(ctx, tenant, in, buyer)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := svc.HistoricalOrderMeta(ctx, tenant, draft.ID)
	if err != nil || meta.ExportContractNo != "" || meta.DestinationPort != "" {
		t.Fatalf("old/empty compatibility: %+v %v", meta, err)
	}
	in.ID = draft.ID
	in.Confirm = true
	confirmed, err := svc.SaveHistoricalOrder(ctx, tenant, in, buyer)
	if err != nil || confirmed.Status != "ORDERED" {
		t.Fatalf("blank confirmation failed: %+v %v", confirmed, err)
	}
	items, err := svc.OrderItems(ctx, tenant, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	in.Lines[0].ID = items[0].ID
	in.ExportContractNo = " CT-OWN "
	in.DestinationPort = " Rotterdam, Netherlands "
	in.Remark = "A factory\n<b>packing</b>"
	if _, err = svc.SaveHistoricalOrder(ctx, tenant, in, buyer); err != nil {
		t.Fatal(err)
	}
	meta, err = svc.HistoricalOrderMeta(ctx, tenant, draft.ID)
	if err != nil || meta.ExportContractNo != "CT-OWN" || meta.DestinationPort != "Rotterdam, Netherlands" {
		t.Fatalf("confirmed fill not retained: %+v %v", meta, err)
	}
	// Preserve financial/execution records exactly while editing informational
	// snapshots after receiving and requesting payment.
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET status='PARTIALLY_RECEIVED',payment_requested_at=now(),business_document_no='FINANCIAL-UNCHANGED',export_contract_no='FINANCIAL-SNAPSHOT' WHERE tenant_id=$1 AND id=$2`, tenant, draft.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var v string
		e := pool.QueryRow(ctx, `SELECT jsonb_build_object('order',to_jsonb(o),'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM purchase_order_items i WHERE i.tenant_id=o.tenant_id AND i.po_id=o.id))::text FROM purchase_orders o WHERE o.tenant_id=$1 AND o.id=$2`, tenant, draft.ID).Scan(&v)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := snapshot()
	if err = svc.UpdateHistoricalShippingContext(ctx, tenant, draft.ID, " CT-OWN ", " Callao, Peru ", buyer); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("metadata edit changed purchase/financial/execution data")
	}
	meta, err = svc.HistoricalOrderMeta(ctx, tenant, draft.ID)
	if err != nil || meta.DestinationPort != "Callao, Peru" || meta.RecordedBy != "Buyer" {
		t.Fatalf("metadata update lost: %+v %v", meta, err)
	}
	for _, tc := range []struct{ tenant, id int64 }{{tenant + 1, 201}, {tenant, 999}, {tenant, 0}} {
		if e := svc.UpdateHistoricalShippingContext(ctx, tc.tenant, draft.ID, "CT-OTHER", "Other", Operator{ID: tc.id}); e == nil {
			t.Fatalf("scope bypass: %+v", tc)
		}
	}
	// Additional confirmed, draft, foreign-tenant and near-match contract records.
	seed := func(tid int64, number, contract string, confirm bool) int64 {
		v := in
		v.ID = 0
		v.PONo = number
		v.ExportContractNo = contract
		v.Confirm = confirm
		v.DestinationPort = "Port B"
		v.Remark = "B instructions"
		v.Lines = append([]HistoricalOrderLine(nil), in.Lines...)
		v.Lines[0].ID = 0
		r, e := svc.SaveHistoricalOrder(ctx, tid, v, buyer)
		if e != nil {
			t.Fatal(e)
		}
		return r.ID
	}
	seed(tenant, "HISTORY-B", "CT-OWN", true)
	seed(tenant, "HISTORY-DRAFT", "CT-OWN", false)
	seed(tenant, "HISTORY-OTHER", "CT-OWN-OTHER", true)
	seed(tenant+1, "HISTORY-FOREIGN", "CT-OWN", true)
	cancelled := seed(tenant, "HISTORY-CANCELLED", "CT-OWN", true)
	if _, err = pool.Exec(ctx, `UPDATE purchase_orders SET status='CANCELLED' WHERE tenant_id=$1 AND id=$2`, tenant, cancelled); err != nil {
		t.Fatal(err)
	}
	shipper := Operator{ID: 301}
	rows, err := svc.ListShippingHistoricalPurchases(ctx, tenant, "CT-OWN", shipper)
	if err != nil || len(rows) != 2 || rows[0].PONo != "HISTORY-CONTEXT" || rows[0].DestinationPort != "Callao, Peru" || rows[1].PONo != "HISTORY-B" {
		t.Fatalf("wrong contract projection: %+v %v", rows, err)
	}
	if err = svc.AuthorizeOrder(ctx, tenant, draft.ID, shipper); err == nil {
		t.Fatal("shipper gained full purchase order access")
	}
	if _, err = svc.ListShippingHistoricalPurchases(ctx, tenant, "CT-OWN", buyer); err == nil {
		t.Fatal("buyer without shipping permission can query shipping projection")
	}
	if _, err = New(pool, Deps{}).ListShippingHistoricalPurchases(ctx, tenant, "CT-OWN", shipper); err == nil {
		t.Fatal("missing IAM should fail closed")
	}
	if err = svc.UpdateHistoricalShippingContext(ctx, tenant, draft.ID, "", "", buyer); err != nil {
		t.Fatal(err)
	}
	meta, err = svc.HistoricalOrderMeta(ctx, tenant, draft.ID)
	if err != nil || meta.ExportContractNo != "" || meta.DestinationPort != "" {
		t.Fatal("clearing optional metadata failed", meta, err)
	}
	rows, err = svc.ListShippingHistoricalPurchases(ctx, tenant, "CT-OWN", shipper)
	if err != nil || len(rows) != 1 || rows[0].PONo != "HISTORY-B" {
		t.Fatal("old match retained after clearing", rows, err)
	}
	rows, err = svc.ListShippingHistoricalPurchases(ctx, tenant, " ", shipper)
	if err != nil || len(rows) != 0 {
		t.Fatal("blank contract leaked records", rows, err)
	}
}
