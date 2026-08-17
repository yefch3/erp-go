package app

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/pkg/xlsx"
)

type sequenceNumbering struct {
	mu  sync.Mutex
	seq int
}

type supplierMapDirectoryStub map[int64]Supplier

func (s supplierMapDirectoryStub) Get(_ context.Context, id int64) (Supplier, error) {
	if supplier, ok := s[id]; ok {
		return supplier, nil
	}
	return Supplier{}, fmt.Errorf("supplier %d not found", id)
}

func TestPurchaseTemplateExportPreviewAndConfirm(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed template test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	var requirementID, secondRequirementID int64
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty) VALUES ($1,1,'CT-TEMPLATE',1,$1,11,'P-11','Template Coil',7,'TON',10) RETURNING id`, tenantID).Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO purchase_requirements (tenant_id,contract_id,contract_no,contract_version_id,contract_item_id,product_id,product_code,product_name,uom_id,uom_code,required_qty) VALUES ($1::bigint,2,'CT-TEMPLATE-2',1,$1::bigint+1,12,'P-12','Second Coil',7,'TON',6) RETURNING id`, tenantID).Scan(&secondRequirementID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_imports WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()
	svc := New(pool, Deps{Numbering: &sequenceNumbering{}, Suppliers: supplierMapDirectoryStub{
		9:  {ID: 9, Code: "SUP-9", Name: "Mill", Currency: "USD", Status: "ACTIVE"},
		10: {ID: 10, Code: "SUP-10", Name: "Second Mill", Currency: "EUR", Status: "ACTIVE"},
	}})
	book, err := svc.ExportPurchaseTemplate(ctx, tenantID, []int64{requirementID, secondRequirementID})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.Parse(book.Data)
	if err != nil {
		t.Fatal(err)
	}
	for len(rows[1]) < len(purchaseTemplateHeaders) {
		rows[1] = append(rows[1], "")
	}
	rows[1][12], rows[1][13], rows[1][14], rows[1][15], rows[1][17], rows[1][18] = "9", "SUP-9", "USD", "520", "5", "30 days"
	for len(rows[2]) < len(purchaseTemplateHeaders) {
		rows[2] = append(rows[2], "")
	}
	rows[2][12], rows[2][13], rows[2][14], rows[2][15], rows[2][17], rows[2][18] = "10", "SUP-10", "EUR", "480", "3", "advance"
	filled, err := xlsx.Build("Purchase Import", rows)
	if err != nil {
		t.Fatal(err)
	}
	op := Operator{ID: 77, Name: "Buyer"}
	preview, err := svc.PreviewPurchaseTemplateImport(ctx, tenantID, filled, "template.xlsx", op)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Groups) != 2 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	group := preview.Groups[0]
	if group.Supplier.ID != 9 {
		group = preview.Groups[1]
	}
	if group.Supplier.ID != 9 || len(group.Lines) != 1 {
		t.Fatalf("supplier split failed: %#v", preview.Groups)
	}
	created, err := svc.ConfirmOrderImport(ctx, tenantID, ConfirmOrderImportInput{ImportToken: group.ImportToken, SupplierID: group.Supplier.ID, Currency: group.Currency, Lines: []ConfirmOrderImportLine{{RowNo: group.Lines[0].RowNo, RequirementID: group.Lines[0].RequirementID, Qty: group.Lines[0].Qty, UnitPrice: group.Lines[0].UnitPrice}}}, op)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "DRAFT" || created.ID == 0 {
		t.Fatalf("unexpected order: %#v", created)
	}
}

func (n *sequenceNumbering) Next(context.Context, string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.seq++
	return fmt.Sprintf("PO-TEST-%04d", n.seq), nil
}

func TestOrderImportConfirmIsIdempotentAndSerializesDemand(t *testing.T) {
	dsn := os.Getenv("PROCUREMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("PROCUREMENT_TEST_DSN not set; skipping DB-backed import test")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantID := time.Now().UnixNano()
	var requirementID int64
	err = pool.QueryRow(ctx, `INSERT INTO purchase_requirements (
		tenant_id, contract_id, contract_no, contract_version_id, contract_item_id,
		product_id, product_code, product_name, uom_id, uom_code, required_qty
	) VALUES ($1,1,'CT-TEST',1,$1,1,'P-TEST','镀锌卷',1,'TON',10) RETURNING id`, tenantID).Scan(&requirementID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_imports WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_order_items WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_orders WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM purchase_requirements WHERE tenant_id=$1`, tenantID)
	}()

	svc := New(pool, Deps{
		Numbering: &sequenceNumbering{},
		Suppliers: supplierDirectoryStub{supplier: Supplier{ID: 9, Code: "SUP-9", Name: "Mill", Currency: "USD", Status: "ACTIVE"}},
	})
	op := Operator{ID: 77, Name: "Buyer"}
	preview := func() PreviewOrderImportResult {
		result, err := svc.PreviewOrderImport(ctx, tenantID, PreviewOrderImportInput{
			SourceType: "UPLOAD", SourceFileName: "test.xlsx",
			Rows: []OrderImportRow{{RowNo: 2, Product: "镀锌卷", Quantity: "10", QuantityUnit: "TON"}},
		}, op)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 1 || result.Rows[0].Result != "MATCHED" {
			t.Fatalf("unexpected preview: %#v", result.Rows)
		}
		return result
	}
	first, second := preview(), preview()

	// A token is tenant-scoped even when the caller and token are otherwise valid.
	if _, err := svc.ConfirmOrderImport(ctx, tenantID+1, ConfirmOrderImportInput{
		ImportToken: first.ImportToken, SupplierID: 9, Currency: "USD",
		Lines: []ConfirmOrderImportLine{{RowNo: 2, RequirementID: requirementID, Qty: "10", UnitPrice: "2"}},
	}, op); err == nil {
		t.Fatal("expected cross-tenant confirmation to be rejected")
	}

	// A validation failure must not leave a partial order or consume the token.
	failed := preview()
	if _, err := svc.ConfirmOrderImport(ctx, tenantID, ConfirmOrderImportInput{
		ImportToken: failed.ImportToken, SupplierID: 9, Currency: "USD",
		Lines: []ConfirmOrderImportLine{{RowNo: 2, RequirementID: requirementID, Qty: "11", UnitPrice: "2"}},
	}, op); err == nil {
		t.Fatal("expected over-quantity confirmation to fail")
	}
	var orderCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_orders WHERE tenant_id=$1`, tenantID).Scan(&orderCount); err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 {
		t.Fatalf("failed confirmation created %d purchase orders", orderCount)
	}
	var failedStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM purchase_order_imports WHERE tenant_id=$1 AND import_token=$2`, tenantID, failed.ImportToken).Scan(&failedStatus); err != nil {
		t.Fatal(err)
	}
	if failedStatus != "PREVIEWED" {
		t.Fatalf("failed confirmation consumed token: status=%s", failedStatus)
	}

	type outcome struct {
		token  string
		result ConfirmOrderImportResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	for _, token := range []string{first.ImportToken, second.ImportToken} {
		go func(token string) {
			result, err := svc.ConfirmOrderImport(ctx, tenantID, ConfirmOrderImportInput{
				ImportToken: token, SupplierID: 9, Currency: "USD",
				Lines: []ConfirmOrderImportLine{{RowNo: 2, RequirementID: requirementID, Qty: "10", UnitPrice: "2"}},
			}, op)
			outcomes <- outcome{token: token, result: result, err: err}
		}(token)
	}

	var winner outcome
	failures := 0
	for range 2 {
		got := <-outcomes
		if got.err != nil {
			failures++
			continue
		}
		winner = got
	}
	if winner.result.ID == 0 || failures != 1 {
		t.Fatalf("expected one created order and one demand conflict, winner=%#v failures=%d", winner, failures)
	}

	retry, err := svc.ConfirmOrderImport(ctx, tenantID, ConfirmOrderImportInput{ImportToken: winner.token}, op)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.AlreadyCreated || retry.ID != winner.result.ID || retry.PONo != winner.result.PONo {
		t.Fatalf("idempotent retry = %#v, first = %#v", retry, winner.result)
	}
}
