package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/shopspring/decimal"
)

func TestExistingContractCreatesOrderedPOAndOpeningReceipt(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed existing-contract test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_receipt_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_receipts WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{
		9: {ID: 9, Code: "SUP-9", Name: "Original Mill", Currency: "USD", Status: "ACTIVE"},
	}})
	event := ContractEffective{
		ContractID: 901, ContractNo: "CT-EXISTING-1", VersionID: 9011, VersionNo: 1,
		CustomerName: "Customer", Currency: "USD", DeliveryDate: "2026-09-30",
		ExistingContract: true, ProcurementEmployeeID: 77, ProcurementEmployee: "Original Buyer", SupplierID: 9,
		Items: []ContractLine{{LineNo: 1, ItemID: 90101, ProductName: "Coil", ProductCode: "P-1",
			Qty: "100", UomCode: "TON", PurchaseUnitPrice: "5", OpeningArrivedQty: "40"}},
	}
	if err := svc.RequirementsFromContract(ctx, tenantID, event, slog.Default(), noopClaim); err != nil {
		t.Fatal(err)
	}
	var status, buyer string
	var ordered, received string
	if err := pool.QueryRow(ctx, `SELECT o.status,o.buyer_name,i.qty::text,i.received_qty::text
		FROM purchase_orders o JOIN purchase_order_items i ON i.po_id=o.id WHERE o.tenant_id=$1`, tenantID).
		Scan(&status, &buyer, &ordered, &received); err != nil {
		t.Fatal(err)
	}
	if status != poPartial || buyer != "Original Buyer" || !decimal.RequireFromString(ordered).Equal(decimal.NewFromInt(100)) || !decimal.RequireFromString(received).Equal(decimal.NewFromInt(40)) {
		t.Fatalf("unexpected imported order: status=%s buyer=%s ordered=%s received=%s", status, buyer, ordered, received)
	}
	var receipts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_receipts WHERE tenant_id=$1`, tenantID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("opening receipts = %d, want 1", receipts)
	}
}
