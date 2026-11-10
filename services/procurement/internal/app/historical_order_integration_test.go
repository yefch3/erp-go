package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

func TestHistoricalOrderSupplierCanBeFilledAfterConfirmation(t *testing.T) {
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
	t.Cleanup(func() {
		for _, table := range []string{"historical_purchase_orders", "purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	})
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{
		9:  {ID: 9, Code: "SUP9", Name: "Supplier", Status: "ACTIVE"},
		10: {ID: 10, Code: "SUP10", Name: "Other", Status: "ACTIVE"},
		11: {ID: 11, Status: "INACTIVE"},
	}, Scopes: &scopeStub{views: map[int64]Visibility{77: {EmployeeIDs: []int64{77}}, 99: {EmployeeIDs: []int64{99}}}}})
	op := Operator{ID: 77, Name: "Buyer"}
	in := HistoricalOrderInput{OriginalDate: "2026-11-01", Currency: "USD", Lines: []HistoricalOrderLine{{ProductName: "Steel", UOM: "MT", Qty: "10", UnitPrice: "12"}}}
	draft, err := svc.SaveHistoricalOrder(ctx, tenant, in, op)
	if err != nil {
		t.Fatal("save without supplier", err)
	}
	in.ID = draft.ID
	in.Confirm = true
	ordered, err := svc.SaveHistoricalOrder(ctx, tenant, in, op)
	if err != nil || ordered.Status != "ORDERED" {
		t.Fatal("confirm without supplier", ordered, err)
	}
	head, err := svc.GetOrder(ctx, tenant, ordered.ID)
	if err != nil || head.SupplierID != 0 || head.SupplierName != "" {
		t.Fatal("missing supplier snapshot", head, err)
	}
	items, err := svc.OrderItems(ctx, tenant, ordered.ID)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	in.Lines[0].ID = items[0].ID
	if _, err = svc.SaveHistoricalOrder(ctx, tenant+1, in, op); err == nil {
		t.Fatal("cross tenant fill allowed")
	}
	if _, err = svc.SaveHistoricalOrder(ctx, tenant, in, Operator{ID: 99}); err == nil {
		t.Fatal("out of scope fill allowed")
	}
	if _, err = svc.ReceiveOrder(ctx, tenant, ordered.ID, 0, []ReceiptLine{{POItemID: items[0].ID, Qty: "1"}}, "", op); apierr.CodeFromError(err) != "PO_SUPPLIER_MISSING" {
		t.Fatal("receipt without supplier", err)
	}
	if _, err = svc.RequestOrderPayment(ctx, tenant, ordered.ID, op); apierr.CodeFromError(err) != "PO_SUPPLIER_MISSING" {
		t.Fatal("payment without supplier", err)
	}
	if _, err = svc.VerifyOrderContract(ctx, tenant, ordered.ID, op); apierr.CodeFromError(err) != "PO_SUPPLIER_MISSING" {
		t.Fatal("contract payment shortcut without supplier", err)
	}
	for _, id := range []int64{-1, 11, 999} {
		in.SupplierID = id
		if _, err = svc.SaveHistoricalOrder(ctx, tenant, in, op); err == nil {
			t.Fatalf("invalid supplier %d accepted", id)
		}
	}
	in.SupplierID = 9
	if _, err = svc.SaveHistoricalOrder(ctx, tenant, in, op); err != nil {
		t.Fatal("fill supplier", err)
	}
	head, err = svc.GetOrder(ctx, tenant, ordered.ID)
	if err != nil || head.SupplierID != 9 || head.SupplierName != "Supplier" || head.SupplierCode != "SUP9" || head.TotalAmount != "120.00" {
		t.Fatal("supplier not persisted", head, err)
	}
	if err = svc.RequireHistoricalSupplier(ctx, tenant, ordered.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{0, 10} {
		in.SupplierID = id
		if _, err = svc.SaveHistoricalOrder(ctx, tenant, in, op); apierr.CodeFromError(err) != "PO_HISTORY_LOCKED" {
			t.Fatalf("confirmed supplier changed to %d: %v", id, err)
		}
	}
	in.SupplierID = 9
	in.Remark = "supplier filled"
	if _, err = svc.SaveHistoricalOrder(ctx, tenant, in, op); err != nil {
		t.Fatal("edit after fill", err)
	}
}

func TestHistoricalOrderLifecycle(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, e := pgdb.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"quality_inspection_tasks", "purchase_order_draft_files", "historical_purchase_orders", "purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1 OR tenant_id=$2", tenant, tenant+1)
		}
	}()
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{9: {ID: 9, Code: "SUP9", Name: "Supplier", Status: "ACTIVE"}}, Scopes: &scopeStub{views: map[int64]Visibility{77: {EmployeeIDs: []int64{77}}, 88: {All: true}, 99: {EmployeeIDs: []int64{99}}}}})
	op := Operator{ID: 77, Name: "Buyer"}
	in := HistoricalOrderInput{SupplierID: 9, PONo: "HIST-1", OriginalDate: "2025-01-01", Currency: "USD", PayableDueDate: "2025-02-01", Lines: []HistoricalOrderLine{{ProductID: 11, ProductCode: "STEEL", UomID: 7, ProductName: "Steel", UOM: "MT", Qty: "10"}}}
	draft, e := svc.SaveHistoricalOrder(ctx, tenant, in, op)
	if e != nil {
		t.Fatal(e)
	}
	if draft.Status != "DRAFT" {
		t.Fatal(draft)
	}
	var owner int64
	if e = pool.QueryRow(ctx, `SELECT buyer_id FROM purchase_orders WHERE tenant_id=$1 AND id=$2`, tenant, draft.ID).Scan(&owner); e != nil || owner != 77 {
		t.Fatal(owner, e)
	}
	if _, e = svc.SaveHistoricalOrder(ctx, tenant, in, op); e == nil {
		t.Fatal("duplicate accepted")
	}
	if _, e = svc.SaveHistoricalOrder(ctx, tenant+1, in, op); e != nil {
		t.Fatal("same number in other tenant", e)
	}
	in.ID = draft.ID
	if _, e = svc.SaveHistoricalOrder(ctx, tenant+1, in, op); e == nil {
		t.Fatal("cross tenant accepted")
	}
	if _, e = svc.SaveHistoricalOrder(ctx, tenant, in, Operator{ID: 99}); e == nil {
		t.Fatal("out of scope accepted")
	}
	if _, _, e = svc.SubmitOrder(ctx, tenant, draft.ID, op); e == nil {
		t.Fatal("historical draft submitted to approval")
	}
	in.Confirm = true
	ordered, e := svc.SaveHistoricalOrder(ctx, tenant, in, op)
	if e != nil {
		t.Fatal(e)
	}
	if ordered.Status != "ORDERED" {
		t.Fatal(ordered)
	}
	m, e := svc.HistoricalOrderMeta(ctx, tenant, ordered.ID)
	if e != nil || !m.Historical || m.PricesComplete || m.RecordedBy != "Buyer" {
		t.Fatal(m, e)
	}
	if e = svc.RequireHistoricalPrices(ctx, tenant, ordered.ID); e == nil {
		t.Fatal("missing prices not guarded")
	}
	var count int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM purchase_requirements WHERE tenant_id=$1 AND status IN ('PENDING','PARTIALLY_ORDERED')`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("unexpected demand", count, e)
	}
	items, e := svc.OrderItems(ctx, tenant, ordered.ID)
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	in.Lines[0].ID = items[0].ID
	in.Lines[0].Qty = "11"
	if _, e = svc.SaveHistoricalOrder(ctx, tenant, in, op); e == nil {
		t.Fatal("confirmed qty changed")
	}
	in.Lines[0].Qty = "10"
	in.Lines[0].UnitPrice = "12.5"
	if _, e = svc.SaveHistoricalOrder(ctx, tenant, in, op); e != nil {
		t.Fatal(e)
	}
	if e = svc.RequireHistoricalPrices(ctx, tenant, ordered.ID); e != nil {
		t.Fatal(e)
	}
	head, e := svc.GetOrder(ctx, tenant, ordered.ID)
	if e != nil || head.TotalAmount != "125.00" || head.PayableDueDate != "2025-02-01" {
		t.Fatal(head.TotalAmount, e)
	}
	if _, e = svc.UploadOrderDraftFile(ctx, tenant, ordered.ID, "original.txt", "text/plain", []byte("original purchase"), op); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateQualityInspection(ctx, tenant, ApplyQualityInput{POID: ordered.ID}, op); e != nil {
		t.Fatal("quality followup", e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM purchase_receipts WHERE tenant_id=$1`, tenant).Scan(&count); e != nil || count != 0 {
		t.Fatal("unexpected receipt", count, e)
	}
}
