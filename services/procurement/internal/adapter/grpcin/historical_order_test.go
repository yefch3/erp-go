package grpcin

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestHistoricalShippingContextListAndDetailWire(t *testing.T) {
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
	ctx = grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenant, EmployeeID: 77, Name: "Buyer"})
	defer func() {
		for _, table := range []string{"historical_purchase_orders", "purchase_order_items", "purchase_orders", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		}
	}()
	h := NewOrders(app.New(pool, app.Deps{}))
	saved, err := h.SaveHistoricalOrder(ctx, &prv1.SaveHistoricalOrderRequest{PoNo: "WIRE-609", OriginalDate: "2026-11-01", ExportContractNo: "CT-609", DestinationPort: "Callao, Peru", Lines: []*prv1.HistoricalOrderLine{{ProductName: "Steel", Qty: "10", Uom: "MT"}}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := h.GetOrder(ctx, &prv1.GetOrderRequest{Id: saved.Id})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := h.ListOrders(ctx, &prv1.ListOrdersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Orders) != 1 {
		t.Fatal(listed)
	}
	for _, o := range []*prv1.PurchaseOrder{detail.Order, listed.Orders[0]} {
		if !o.Historical || o.ExportContractNo != "CT-609" || o.DestinationPort != "Callao, Peru" {
			t.Fatalf("fields missing: %+v", o)
		}
		raw, e := protojson.Marshal(o)
		if e != nil {
			t.Fatal(e)
		}
		var data map[string]any
		if e = json.Unmarshal(raw, &data); e != nil {
			t.Fatal(e)
		}
		if data["exportContractNo"] != "CT-609" || data["destinationPort"] != "Callao, Peru" {
			t.Fatalf("wrong JSON: %s", raw)
		}
	}
	// Ordinary orders retain the existing contract snapshot; adding historical
	// metadata must not replace it with empty historical defaults.
	var normalID int64
	if err = pool.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,po_no,supplier_id,buyer_id,buyer_name,export_contract_no) VALUES($1,'NORMAL-WIRE-609',9,77,'Buyer','NORMAL-CONTRACT') RETURNING id`, tenant).Scan(&normalID); err != nil {
		t.Fatal(err)
	}
	normal, err := h.GetOrder(ctx, &prv1.GetOrderRequest{Id: normalID})
	if err != nil {
		t.Fatal(err)
	}
	if normal.Order.Historical || normal.Order.ExportContractNo != "NORMAL-CONTRACT" || normal.Order.DestinationPort != "" {
		t.Fatalf("ordinary order changed: %+v", normal.Order)
	}
	if _, err = h.UpdateHistoricalShippingContext(ctx, &prv1.UpdateHistoricalShippingContextRequest{Id: normalID, ExportContractNo: "REPLACED"}); err == nil {
		t.Fatal("ordinary order accepted historical metadata write")
	}
}
