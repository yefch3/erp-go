package app

import (
	"context"
	"encoding/json"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestContractHandoffRemarksAndReference(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN required")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := time.Now().UnixNano()
	svc := New(pool, Deps{})
	reference := json.RawMessage(`{"id":"frozen-7","kind":"PROCUREMENT","version":3,"body":{"company":"Reference factory","currency":"CNY","prices":[{"price":"4000"}]}}`)
	event := ContractEffective{ContractID: 11, VersionID: 12, VersionNo: 1, ContractNo: "CT-HANDOFF", QuotationID: 9, QuotationNo: "QT-FROZEN", SalesEmployeeID: 77, SalesEmployee: "Sales", SourceSalesRemark: "sales note", Items: []ContractLine{{ItemID: 101, ProductName: "HRC", Spec: "3mm", Qty: "60", UomCode: "MT", Remark: "dry packing", PresaleReference: reference}, {ItemID: 102, ProductName: "HRC", Spec: "4mm", Qty: "40", UomCode: "MT", Remark: "no oil"}}}
	for i := 0; i < 2; i++ {
		if err := svc.RequirementsFromContract(ctx, tenant, event, slog.Default(), noopClaim); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var remark, ref, quote, status string
	if err := pool.QueryRow(ctx, `SELECT count(*),max(contract_item_remark),max(presale_reference_json),max(quotation_no),max(status) FROM purchase_requirements WHERE tenant_id=$1 AND contract_item_id=101`, tenant).Scan(&count, &remark, &ref, &quote, &status); err != nil {
		t.Fatal(err)
	}
	if count != 1 || remark != "dry packing" || ref != string(reference) || quote != "QT-FROZEN" || status != "WAITING_REQUOTE" {
		t.Fatal(count, remark, ref, quote, status)
	}
	// The reference must not silently become the final execution supplier.
	var supplier int64
	if err := pool.QueryRow(ctx, `SELECT supplier_id FROM purchase_requirements WHERE tenant_id=$1 AND contract_item_id=101`, tenant).Scan(&supplier); err != nil || supplier != 0 {
		t.Fatal(supplier, err)
	}
}
