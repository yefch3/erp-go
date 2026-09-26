package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"os"
	"testing"
	"time"
)

func TestExecutionQuoteCurrency(t *testing.T) {
	for _, currency := range []string{"CNY", "USD", "EUR", "JPY"} {
		if !validExecutionQuoteCurrency(currency) {
			t.Fatalf("rejected %s", currency)
		}
	}
	for _, currency := range []string{"", "US", "USDD", "123", "C N", "usd"} {
		if validExecutionQuoteCurrency(currency) {
			t.Fatalf("accepted %q", currency)
		}
	}
}

func TestExecutionFactoryQuoteUsesOriginalPrice(t *testing.T) {
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
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"purchase_order_items", "purchase_orders", "purchase_execution_supplier_quotes", "purchase_requirements"} {
			_, _ = pool.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", tenantID)
		}
	}()
	var requirementID int64
	err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements
  (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty,status)
  VALUES ($1,99002,'CT-PLAIN-FACTORY',1,99002,11,'P-11','Steel',7,'MT',5,'WAITING_REQUOTE') RETURNING id`, tenantID).Scan(&requirementID)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{
		6: {ID: 6, Code: "S6", Name: "Factory", Currency: "CNY", Status: "ACTIVE"},
	}})
	op := Operator{ID: 5, Name: "Buyer"}
	var quote ExecutionSupplierQuote
	for _, currency := range []string{"CNY", "USD", "EUR"} {
		quote, err = svc.SaveExecutionSupplierQuote(ctx, tenantID, SaveExecutionSupplierQuoteInput{
			RequirementID: requirementID, SupplierID: 6, Currency: currency, UnitPrice: "50", PaymentTerms: "T/T",
		}, op)
		if err != nil {
			t.Fatalf("save %s: %v", currency, err)
		}
		if quote.Currency != currency || quote.QuoteCategory != "" || quote.CalculatedUnitPrice != "" {
			t.Fatalf("unexpected quote: %#v", quote)
		}
	}
	if _, err = svc.SelectExecutionSupplierQuote(ctx, tenantID, requirementID, quote.ID, op); err != nil {
		t.Fatal(err)
	}
	input := CreateOrderInput{SupplierID: 6, Currency: "EUR", FulfillmentMode: "DIRECT_SHIP", DeliveryLocationType: "CUSTOM", DeliveryAddress: "Contract address",
		Lines: []OrderLine{{RequirementID: requirementID, Qty: "5", UnitPrice: "50", ExecutionQuoteID: quote.ID}},
	}
	if _, err = svc.CreateOrder(ctx, tenantID, input, Operator{ID: 7, Name: "Other buyer"}); err == nil {
		t.Fatal("another buyer must not use this selection")
	}
	// Old calculation results must neither gate a new order nor replace its original price.
	if _, err = pool.Exec(ctx, `UPDATE purchase_execution_supplier_quotes SET calculated_unit_price=999,quote_category='FOB_CNY' WHERE tenant_id=$1 AND id=$2`, tenantID, quote.ID); err != nil {
		t.Fatal(err)
	}
	input.Lines[0].UnitPrice = "999"
	if _, err = svc.CreateOrder(ctx, tenantID, input, op); err == nil {
		t.Fatal("must reject stale calculated price")
	}
	input.Lines[0].UnitPrice = "50"
	input.Currency = "USD"
	if _, err = svc.CreateOrder(ctx, tenantID, input, op); err == nil {
		t.Fatal("must reject wrong currency")
	}
	input.Currency = "EUR"
	if _, err = pool.Exec(ctx, `UPDATE purchase_execution_supplier_quotes SET calculated_unit_price=NULL WHERE tenant_id=$1 AND id=$2`, tenantID, quote.ID); err != nil {
		t.Fatal(err)
	}
	order, err := svc.CreateOrder(ctx, tenantID, input, op)
	if err != nil {
		t.Fatal(err)
	}
	var storedPrice, storedCurrency string
	err = pool.QueryRow(ctx, `SELECT i.unit_price::text,o.currency FROM purchase_order_items i JOIN purchase_orders o ON o.id=i.po_id AND o.tenant_id=i.tenant_id WHERE i.tenant_id=$1 AND i.po_id=$2`, tenantID, order.ID).Scan(&storedPrice, &storedCurrency)
	if err != nil || storedPrice != "50.0000" || storedCurrency != "EUR" {
		t.Fatalf("price=%s currency=%s err=%v", storedPrice, storedCurrency, err)
	}
}
