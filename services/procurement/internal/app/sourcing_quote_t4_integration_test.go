package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
)

func TestProductGroupQuotesKeepBuyerOwnershipAndVersions(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed T4 quote test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	defer func() {
		for _, table := range []string{"supplier_quote_lines", "supplier_quotes", "factory_rfq_lines", "factory_rfqs", "sourcing_case_changes", "sourcing_procurement_participants", "sourcing_lines", "sourcing_cases"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1`, tenantID)
		}
	}()

	svc := New(pool, Deps{})
	sales := Operator{ID: 10, Name: "销售"}
	buyerA := Operator{ID: 20, Name: "采购甲"}
	buyerB := Operator{ID: 21, Name: "采购乙"}
	created, err := svc.CreateSourcingCase(ctx, tenantID, NewSourcingCase{
		Title: "T4 产品组报价",
		Lines: []SourcingLineInput{
			{Product: "钢卷", Quantity: "20", QuantityUnit: "TON"},
			{Product: "钢板", Quantity: "10", QuantityUnit: "TON"},
		},
	}, sales)
	if err != nil {
		t.Fatal(err)
	}
	caseID := created.Head.ID
	lineA, lineB := created.Lines[0].ID, created.Lines[1].ID
	if _, err = svc.ConfirmSourcingLines(ctx, tenantID, caseID, []int64{lineA, lineB}, "", sales); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.JoinSourcingCase(ctx, tenantID, caseID, buyerA); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.JoinSourcingCase(ctx, tenantID, caseID, buyerB); err != nil {
		t.Fatal(err)
	}

	seedRFQ := func(no, supplier string, supplierID, ownerID int64, ownerName string) int64 {
		var rfqID int64
		if err := pool.QueryRow(ctx, `INSERT INTO factory_rfqs
(tenant_id,case_id,rfq_no,supplier_id,supplier_name,factory_id,factory_name,currency,status,created_by,created_by_name)
VALUES ($1,$2,$3,$4,$5,9001,'共同工厂','USD','DRAFT',$6,$7) RETURNING id`,
			tenantID, caseID, no, supplierID, supplier, ownerID, ownerName).Scan(&rfqID); err != nil {
			t.Fatal(err)
		}
		for index, lineID := range []int64{lineA, lineB} {
			if _, err := pool.Exec(ctx, `INSERT INTO factory_rfq_lines
(tenant_id,factory_rfq_id,sourcing_line_id,line_no,qty,uom_code)
VALUES ($1,$2,$3,$4,10,'TON')`, tenantID, rfqID, lineID, index+1); err != nil {
				t.Fatal(err)
			}
		}
		return rfqID
	}
	rfqA := seedRFQ("RFQ-T4-A", "供应商甲", 101, buyerA.ID, buyerA.Name)
	rfqB := seedRFQ("RFQ-T4-B", "供应商乙", 102, buyerB.ID, buyerB.Name)

	quote := func(rfqID, lineID int64, price string, op Operator) storeQuoteResult {
		row, err := svc.CreateSupplierQuote(ctx, tenantID, NewSupplierQuote{
			FactoryRFQID: rfqID, Currency: "USD", Incoterm: "fob", Source: "MANUAL",
			Lines: []SupplierQuoteLineInput{{SourcingLineID: lineID, Qty: "10", UnitPrice: price, MOQ: "5", LeadTime: "15"}},
		}, op)
		if err != nil {
			t.Fatal(err)
		}
		return storeQuoteResult{Version: row.VersionNo}
	}
	if got := quote(rfqA, lineA, "620", buyerA).Version; got != 1 {
		t.Fatalf("buyer A first version = %d, want 1", got)
	}
	if got := quote(rfqA, lineA, "605", buyerA).Version; got != 2 {
		t.Fatalf("buyer A second version = %d, want 2", got)
	}
	if got := quote(rfqB, lineB, "590", buyerB).Version; got != 1 {
		t.Fatalf("buyer B first version = %d, want 1", got)
	}
	if _, err := svc.CreateSupplierQuote(ctx, tenantID, NewSupplierQuote{
		FactoryRFQID: rfqB, Currency: "USD", Source: "MANUAL",
		Lines: []SupplierQuoteLineInput{{SourcingLineID: lineB, Qty: "10", UnitPrice: "580"}},
	}, buyerA); err == nil || !strings.Contains(err.Error(), "SC_RFQ_OWNER_REQUIRED") {
		t.Fatalf("buyer A must not write buyer B quote, got %v", err)
	}

	rows, err := svc.ListSupplierQuoteComparison(ctx, tenantID, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("quote comparison rows = %d, want 3 historical rows", len(rows))
	}
	versions := map[int64][]int32{}
	for _, item := range rows {
		if item.Row.Incoterm != "FOB" {
			t.Fatalf("quote incoterm = %q, want FOB", item.Row.Incoterm)
		}
		versions[item.Row.CreatedBy] = append(versions[item.Row.CreatedBy], item.Row.VersionNo)
	}
	if len(versions[buyerA.ID]) != 2 || len(versions[buyerB.ID]) != 1 {
		t.Fatalf("quote ownership/version split = %#v", versions)
	}
}

type storeQuoteResult struct {
	Version int32
}
