package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/pkg/xlsx"
)

type importCatalogStub struct{}

func (importCatalogStub) Resolve(_ context.Context, productCode, skuCode string) (CatalogItem, error) {
	if productCode != "P-WH4" || skuCode != "SKU-WH4" {
		return CatalogItem{}, os.ErrNotExist
	}
	return CatalogItem{ProductID: 401, ProductCode: productCode, ProductName: "WH4测试产品", SKUID: 402, SKUCode: skuCode, UomID: 1, UomCode: "件"}, nil
}

func TestInitialStockTemplateCarriesVersionAndHeaders(t *testing.T) {
	_, data, version, err := BuildInitialStockTemplate()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := xlsx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if version != initialStockTemplateVersion || rows[0][1] != initialStockTemplateCode || rows[0][3] != initialStockTemplateVersion {
		t.Fatalf("unexpected template identity: version=%q row=%v", version, rows[0])
	}
	if err := validateInitialStockTemplate(rows); err != nil {
		t.Fatal(err)
	}
	rows[1][0] = "仓库名称"
	if err := validateInitialStockTemplate(rows); err == nil {
		t.Fatal("modified header was accepted")
	}
}

// TestInitialStockImportIsAtomicAndIdempotent 验证预检不入账、确认只入账一次，并阻止相同文件重复导入。
func TestInitialStockImportIsAtomicAndIdempotent(t *testing.T) {
	dsn := os.Getenv("INVENTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTORY_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgdb.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenantID := time.Now().UnixNano()
	var warehouseID int64
	code := "WH4-" + time.Now().Format("150405.000000000")
	if err := pool.QueryRow(ctx, `INSERT INTO warehouses (tenant_id,code,name,wh_type) VALUES ($1,$2,'WH4期初仓','NORMAL') RETURNING id`, tenantID, code).Scan(&warehouseID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM stock_import_rows WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_imports WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM stock_ledger WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM stocks WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM warehouses WHERE tenant_id=$1`, tenantID)
	}()
	book, err := xlsx.Build("Initial Stock", [][]string{
		{"模板编码", initialStockTemplateCode, "模板版本", initialStockTemplateVersion},
		initialStockHeaders,
		{code, "P-WH4", "SKU-WH4", "12.5", "件", "8", "CNY", "期初盘点"},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, nil, importCatalogStub{})
	preview, err := svc.PreviewInitialStockImport(ctx, tenantID, book, "initial.xlsx", "EXT-WH4-1", Operator{ID: 9, Name: "测试管理员"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Batch.Status != "PREVIEW" || preview.Batch.ValidCount != 1 || preview.Batch.ErrorCount != 0 {
		t.Fatalf("unexpected preview: %+v rows=%+v", preview.Batch, preview.Rows)
	}
	history, total, err := svc.ListStockImports(ctx, tenantID, 1, 20)
	if err != nil || total != 1 || len(history) != 1 || history[0].ImportToken != preview.Batch.ImportToken {
		t.Fatalf("preview history missing: total=%d history=%+v err=%v", total, history, err)
	}
	var ledgerBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM stock_ledger WHERE tenant_id=$1`, tenantID).Scan(&ledgerBefore); err != nil || ledgerBefore != 0 {
		t.Fatalf("preview changed stock: count=%d err=%v", ledgerBefore, err)
	}
	confirmed, already, err := svc.ConfirmInitialStockImport(ctx, tenantID, preview.Batch.ImportToken, Operator{ID: 10, Name: "确认管理员"})
	if err != nil || already || confirmed.Status != "CONFIRMED" {
		t.Fatalf("confirm failed: batch=%+v already=%v err=%v", confirmed, already, err)
	}
	var qty, refType, refNo string
	if err := pool.QueryRow(ctx, `SELECT s.on_hand_qty::text,l.ref_type,l.ref_no FROM stocks s JOIN stock_ledger l ON l.stock_id=s.id WHERE s.tenant_id=$1 AND s.warehouse_id=$2`, tenantID, warehouseID).Scan(&qty, &refType, &refNo); err != nil {
		t.Fatal(err)
	}
	if qty != "12.5000" || refType != "INITIAL_IMPORT" || refNo != preview.Batch.BatchNo {
		t.Fatalf("unexpected booking: qty=%s ref=%s/%s", qty, refType, refNo)
	}
	_, already, err = svc.ConfirmInitialStockImport(ctx, tenantID, preview.Batch.ImportToken, Operator{ID: 10, Name: "确认管理员"})
	if err != nil || !already {
		t.Fatalf("idempotent retry failed: already=%v err=%v", already, err)
	}
	if _, err := svc.PreviewInitialStockImport(ctx, tenantID, book, "initial.xlsx", "EXT-WH4-2", Operator{ID: 9, Name: "测试管理员"}); err == nil || !strings.Contains(err.Error(), "已经入账") {
		t.Fatalf("duplicate file was not blocked: %v", err)
	}
}
