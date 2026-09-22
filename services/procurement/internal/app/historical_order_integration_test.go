package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

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
