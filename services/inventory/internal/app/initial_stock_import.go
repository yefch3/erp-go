package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/pkg/xlsx"
	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

const (
	initialStockTemplateCode    = "ERP_INITIAL_STOCK"
	initialStockTemplateVersion = "WH4-INITIAL-STOCK-V1"
	initialStockImportType      = "INITIAL_STOCK"
)

var initialStockHeaders = []string{"仓库编码", "产品编码", "SKU编码（可空）", "数量", "单位", "单位成本", "币种", "备注"}

type StockImportIssue struct {
	Field   string
	Value   string
	Message string
}

type StockImportRow struct {
	RowNumber     int32
	WarehouseID   int64
	WarehouseCode string
	WarehouseName string
	ProductID     int64
	ProductCode   string
	ProductName   string
	SKUID         int64
	SKUCode       string
	UomID         int64
	UomCode       string
	Qty           string
	UnitCost      string
	Currency      string
	Remark        string
	Verdict       string
	Issues        []StockImportIssue
}

type StockImportBatch struct {
	ID              int64
	ImportToken     string
	BatchNo         string
	ImportType      string
	SourceFileName  string
	ExternalBatchNo string
	TemplateVersion string
	Status          string
	TotalCount      int32
	ValidCount      int32
	WarningCount    int32
	ErrorCount      int32
	DuplicateCount  int32
	OperatorName    string
	CreatedAt       time.Time
	ConfirmedAt     *time.Time
}

type StockImportPreview struct {
	Batch         StockImportBatch
	Rows          []StockImportRow
	ErrorFileName string
	ErrorFileData []byte
}

type initialImportWarehouse struct {
	ID     int64
	Code   string
	Name   string
	Status string
	Type   string
}

// BuildInitialStockTemplate 生成带固定模板编码和版本的期初库存工作簿。
func BuildInitialStockTemplate() (string, []byte, string, error) {
	rows := [][]string{{"模板编码", initialStockTemplateCode, "模板版本", initialStockTemplateVersion}, initialStockHeaders}
	data, err := xlsx.Build("Initial Stock", rows)
	return "initial-stock-import-v1.xlsx", data, initialStockTemplateVersion, err
}

// PreviewInitialStockImport 解析并持久化预检结果；此阶段绝不修改库存和流水。
func (s *Service) PreviewInitialStockImport(ctx context.Context, tenantID int64, data []byte, sourceFileName, externalBatchNo string, op Operator) (StockImportPreview, error) {
	if s.catalog == nil {
		return StockImportPreview{}, apierr.Internal("IV_IMPORT_CATALOG_UNAVAILABLE", "产品服务暂不可用")
	}
	rows, err := xlsx.Parse(data)
	if err != nil {
		return StockImportPreview{}, apierr.Invalid("IV_IMPORT_FILE_INVALID", "无法读取 Excel，请使用系统下载的模板")
	}
	if err := validateInitialStockTemplate(rows); err != nil {
		return StockImportPreview{}, err
	}
	hash := sha256.Sum256(data)
	fileHash := hex.EncodeToString(hash[:])
	externalBatchNo = strings.TrimSpace(externalBatchNo)
	if prior, err := s.findConfirmedImport(ctx, tenantID, fileHash, externalBatchNo); err != nil {
		return StockImportPreview{}, err
	} else if prior != "" {
		return StockImportPreview{}, apierr.Conflict("IV_IMPORT_DUPLICATE", "该文件或外部批次已经入账："+prior)
	}

	warehouseCache := map[string]initialImportWarehouse{}
	catalogCache := map[string]CatalogItem{}
	seen := map[string]bool{}
	parsed := make([]StockImportRow, 0, len(rows)-2)
	for index, cells := range rows[2:] {
		if blankImportRow(cells) {
			continue
		}
		row := parseInitialStockRow(int32(index+3), cells)
		warehouseKey := strings.ToUpper(row.WarehouseCode)
		warehouse, ok := warehouseCache[warehouseKey]
		if !ok {
			warehouse, err = s.resolveImportWarehouse(ctx, tenantID, row.WarehouseCode)
			if err == nil {
				warehouseCache[warehouseKey] = warehouse
			}
		}
		if err != nil {
			row.addIssue("仓库编码", row.WarehouseCode, "仓库不存在、已停用或不能记库存")
		} else {
			row.WarehouseID, row.WarehouseName = warehouse.ID, warehouse.Name
			if live, checkErr := s.warehouseHasLedger(ctx, tenantID, warehouse.ID); checkErr != nil {
				return StockImportPreview{}, checkErr
			} else if live {
				row.addIssue("仓库编码", row.WarehouseCode, "该仓库已经产生库存流水，不能再导入期初库存")
			}
		}

		catalogKey := strings.ToUpper(row.ProductCode + "\x00" + row.SKUCode)
		item, ok := catalogCache[catalogKey]
		if !ok {
			item, err = s.catalog.Resolve(ctx, row.ProductCode, row.SKUCode)
			if err == nil {
				catalogCache[catalogKey] = item
			}
		}
		if err != nil {
			row.addIssue("产品/SKU", strings.TrimSpace(row.ProductCode+" "+row.SKUCode), "产品或 SKU 不存在或未启用")
		} else {
			row.ProductID, row.ProductCode, row.ProductName = item.ProductID, item.ProductCode, item.ProductName
			row.SKUID, row.SKUCode, row.UomID = item.SKUID, item.SKUCode, item.UomID
			if !strings.EqualFold(row.UomCode, item.UomCode) {
				row.addIssue("单位", row.UomCode, "单位必须与产品基础单位 "+item.UomCode+" 一致")
			} else {
				row.UomCode = item.UomCode
			}
		}
		qty, qtyErr := decimal.NewFromString(row.Qty)
		if qtyErr != nil || qty.LessThanOrEqual(decimal.Zero) {
			row.addIssue("数量", row.Qty, "数量必须是大于 0 的数字")
		} else {
			row.Qty = qty.String()
		}
		cost, costErr := decimal.NewFromString(row.UnitCost)
		if costErr != nil || cost.IsNegative() {
			row.addIssue("单位成本", row.UnitCost, "单位成本必须是大于或等于 0 的数字")
		} else {
			row.UnitCost = cost.String()
		}
		if !strings.EqualFold(row.Currency, baseCurrency) {
			row.addIssue("币种", row.Currency, "期初库存成本币种当前只支持 "+baseCurrency)
		} else {
			row.Currency = baseCurrency
		}
		identity := fmt.Sprintf("%d/%d/%d", row.WarehouseID, row.ProductID, row.SKUID)
		if row.WarehouseID != 0 && row.ProductID != 0 && seen[identity] {
			row.addDuplicate("产品/SKU", row.ProductCode+" "+row.SKUCode, "同一仓库中的同一产品/SKU 在文件中重复")
		}
		seen[identity] = row.WarehouseID != 0 && row.ProductID != 0
		row.finishVerdict()
		parsed = append(parsed, row)
	}
	if len(parsed) == 0 {
		return StockImportPreview{}, apierr.Invalid("IV_IMPORT_ROWS_REQUIRED", "模板中没有期初库存明细")
	}
	return s.saveStockImportPreview(ctx, tenantID, sourceFileName, externalBatchNo, fileHash, parsed, op)
}

func validateInitialStockTemplate(rows [][]string) error {
	if len(rows) < 2 || len(rows[0]) < 4 || rows[0][0] != "模板编码" || rows[0][1] != initialStockTemplateCode || rows[0][2] != "模板版本" || rows[0][3] != initialStockTemplateVersion {
		return apierr.Invalid("IV_IMPORT_TEMPLATE_VERSION", "模板版本不受支持，请重新下载当前模板")
	}
	if len(rows[1]) != len(initialStockHeaders) {
		return apierr.Invalid("IV_IMPORT_HEADER_INVALID", "模板列名或顺序已被修改")
	}
	for i, header := range initialStockHeaders {
		if rows[1][i] != header {
			return apierr.Invalid("IV_IMPORT_HEADER_INVALID", "模板列名或顺序已被修改")
		}
	}
	return nil
}

func blankImportRow(cells []string) bool {
	for _, cell := range cells {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func cell(cells []string, index int) string {
	if index >= len(cells) {
		return ""
	}
	return strings.TrimSpace(cells[index])
}

func parseInitialStockRow(rowNumber int32, cells []string) StockImportRow {
	return StockImportRow{RowNumber: rowNumber, WarehouseCode: cell(cells, 0), ProductCode: cell(cells, 1), SKUCode: cell(cells, 2), Qty: cell(cells, 3), UomCode: cell(cells, 4), UnitCost: cell(cells, 5), Currency: strings.ToUpper(cell(cells, 6)), Remark: cell(cells, 7)}
}

func (r *StockImportRow) addIssue(field, value, message string) {
	r.Issues = append(r.Issues, StockImportIssue{Field: field, Value: value, Message: message})
}

func (r *StockImportRow) addDuplicate(field, value, message string) {
	r.addIssue(field, value, message)
	r.Verdict = "DUPLICATE"
}

func (r *StockImportRow) finishVerdict() {
	if r.Verdict == "DUPLICATE" {
		return
	}
	if len(r.Issues) > 0 {
		r.Verdict = "ERROR"
	} else {
		r.Verdict = "VALID"
	}
}

func (s *Service) resolveImportWarehouse(ctx context.Context, tenantID int64, code string) (initialImportWarehouse, error) {
	var row initialImportWarehouse
	err := s.pool.QueryRow(ctx, `SELECT id,code,name,status,wh_type FROM warehouses WHERE tenant_id=$1 AND upper(code)=upper($2)`, tenantID, strings.TrimSpace(code)).Scan(&row.ID, &row.Code, &row.Name, &row.Status, &row.Type)
	if err != nil || row.Status != "ACTIVE" || row.Type == "VIRTUAL" {
		return initialImportWarehouse{}, errors.New("warehouse unavailable")
	}
	return row, nil
}

func (s *Service) warehouseHasLedger(ctx context.Context, tenantID, warehouseID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stock_ledger WHERE tenant_id=$1 AND warehouse_id=$2)`, tenantID, warehouseID).Scan(&exists)
	return exists, err
}

func newImportToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Service) findConfirmedImport(ctx context.Context, tenantID int64, fileHash, externalBatchNo string) (string, error) {
	var batchNo string
	err := s.pool.QueryRow(ctx, `SELECT batch_no FROM stock_imports WHERE tenant_id=$1 AND status='CONFIRMED' AND (file_sha256=$2 OR ($3<>'' AND external_batch_no=$3)) ORDER BY id DESC LIMIT 1`, tenantID, fileHash, externalBatchNo).Scan(&batchNo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return batchNo, err
}

func (s *Service) saveStockImportPreview(ctx context.Context, tenantID int64, sourceFileName, externalBatchNo, fileHash string, rows []StockImportRow, op Operator) (StockImportPreview, error) {
	token, err := newImportToken()
	if err != nil {
		return StockImportPreview{}, err
	}
	batch := StockImportBatch{ImportToken: token, ImportType: initialStockImportType, SourceFileName: strings.TrimSpace(sourceFileName), ExternalBatchNo: externalBatchNo, TemplateVersion: initialStockTemplateVersion, OperatorName: op.Name, TotalCount: int32(len(rows))}
	for _, row := range rows {
		switch row.Verdict {
		case "VALID":
			batch.ValidCount++
		case "WARNING":
			batch.WarningCount++
		case "DUPLICATE":
			batch.DuplicateCount++
		default:
			batch.ErrorCount++
		}
	}
	batch.Status = "PREVIEW"
	if batch.ErrorCount+batch.DuplicateCount > 0 {
		batch.Status = "INVALID"
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO stock_imports (tenant_id,import_token,import_type,source_file_name,file_sha256,external_batch_no,template_version,status,total_count,valid_count,warning_count,error_count,duplicate_count,operator_id,operator_name) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id,created_at`, tenantID, batch.ImportToken, batch.ImportType, batch.SourceFileName, fileHash, batch.ExternalBatchNo, batch.TemplateVersion, batch.Status, batch.TotalCount, batch.ValidCount, batch.WarningCount, batch.ErrorCount, batch.DuplicateCount, op.ID, op.Name).Scan(&batch.ID, &batch.CreatedAt); err != nil {
			return err
		}
		batch.BatchNo = fmt.Sprintf("INIT-%06d", batch.ID)
		if _, err := tx.Exec(ctx, `UPDATE stock_imports SET batch_no=$1 WHERE tenant_id=$2 AND id=$3`, batch.BatchNo, tenantID, batch.ID); err != nil {
			return err
		}
		for _, row := range rows {
			fields, values, messages := issueTexts(row.Issues)
			if _, err := tx.Exec(ctx, `INSERT INTO stock_import_rows (tenant_id,import_id,row_number,warehouse_id,warehouse_code,warehouse_name,product_id,product_code,product_name,sku_id,sku_code,uom_id,uom_code,qty,unit_cost,currency,remark,verdict,issue_field,issue_value,issue_message) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`, tenantID, batch.ID, row.RowNumber, row.WarehouseID, row.WarehouseCode, row.WarehouseName, row.ProductID, row.ProductCode, row.ProductName, row.SKUID, row.SKUCode, row.UomID, row.UomCode, row.Qty, row.UnitCost, row.Currency, row.Remark, row.Verdict, fields, values, messages); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return StockImportPreview{}, err
	}
	preview := StockImportPreview{Batch: batch, Rows: rows}
	if batch.Status == "INVALID" {
		preview.ErrorFileName = batch.BatchNo + "-errors.xlsx"
		preview.ErrorFileData, err = buildStockImportReport(rows)
	}
	return preview, err
}

func issueTexts(issues []StockImportIssue) (string, string, string) {
	fields, values, messages := make([]string, 0, len(issues)), make([]string, 0, len(issues)), make([]string, 0, len(issues))
	for _, issue := range issues {
		fields, values, messages = append(fields, issue.Field), append(values, issue.Value), append(messages, issue.Message)
	}
	return strings.Join(fields, "；"), strings.Join(values, "；"), strings.Join(messages, "；")
}

func buildStockImportReport(rows []StockImportRow) ([]byte, error) {
	out := [][]string{{"Excel行号", "仓库编码", "产品编码", "SKU编码", "数量", "单位", "单位成本", "币种", "备注", "结果", "错误字段", "原值", "原因"}}
	for _, row := range rows {
		fields, values, messages := issueTexts(row.Issues)
		out = append(out, []string{fmt.Sprint(row.RowNumber), row.WarehouseCode, row.ProductCode, row.SKUCode, row.Qty, row.UomCode, row.UnitCost, row.Currency, row.Remark, row.Verdict, fields, values, messages})
	}
	return xlsx.Build("Import Report", out)
}

// ConfirmInitialStockImport 在单一事务内锁定预检批次、再次校验仓库边界并写入库存和流水。
func (s *Service) ConfirmInitialStockImport(ctx context.Context, tenantID int64, token string, op Operator) (StockImportBatch, bool, error) {
	var result StockImportBatch
	already := false
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		batch, err := scanStockImport(tx.QueryRow(ctx, stockImportSelect+` WHERE tenant_id=$1 AND import_token=$2 FOR UPDATE`, tenantID, strings.TrimSpace(token)))
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("IV_IMPORT_NOT_FOUND", "导入预检不存在")
		}
		if err != nil {
			return err
		}
		result = batch
		if batch.Status == "CONFIRMED" {
			already = true
			return nil
		}
		if batch.Status != "PREVIEW" {
			return apierr.Conflict("IV_IMPORT_NOT_READY", "该批次未通过预检，不能确认入账")
		}
		rows, err := loadStockImportRows(ctx, tx, tenantID, batch.ID)
		if err != nil {
			return err
		}
		if len(rows) == 0 || int32(len(rows)) != batch.ValidCount {
			return apierr.Conflict("IV_IMPORT_ROWS_CHANGED", "预检明细不完整，请重新上传")
		}
		warehouses := map[int64]bool{}
		for _, row := range rows {
			if row.Verdict != "VALID" {
				return apierr.Conflict("IV_IMPORT_NOT_READY", "该批次仍有错误行")
			}
			warehouses[row.WarehouseID] = true
		}
		for warehouseID := range warehouses {
			var live bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stock_ledger WHERE tenant_id=$1 AND warehouse_id=$2)`, tenantID, warehouseID).Scan(&live); err != nil {
				return err
			}
			if live {
				return apierr.Conflict("IV_IMPORT_WAREHOUSE_LIVE", "预检后仓库已经产生库存流水，请重新检查")
			}
		}
		q := store.New(tx)
		for _, row := range rows {
			qty, _ := decimal.NewFromString(row.Qty)
			line := ReceiveLine{ProductID: row.ProductID, SkuID: row.SKUID, ProductCode: row.ProductCode, ProductName: row.ProductName, UomID: row.UomID, UomCode: row.UomCode, Qty: row.Qty, UnitCost: row.UnitCost}
			unit, amount, err := s.priceLine(ctx, q, tenantID, row.WarehouseID, line, qty)
			if err != nil {
				return err
			}
			stock, err := q.UpsertStockOnInbound(ctx, store.UpsertStockOnInboundParams{TenantID: tenantID, WarehouseID: row.WarehouseID, ProductID: row.ProductID, SkuID: row.SKUID, UomID: row.UomID, UomCode: row.UomCode, ProductCode: row.ProductCode, ProductName: row.ProductName, Qty: row.Qty, Amount: amount.String(), CostCurrency: baseCurrency})
			if err != nil {
				return err
			}
			remark := "期初库存导入"
			if row.Remark != "" {
				remark += "：" + row.Remark
			}
			if err := q.AppendLedger(ctx, store.AppendLedgerParams{TenantID: tenantID, StockID: stock.ID, WarehouseID: row.WarehouseID, SkuID: row.SKUID, Movement: "INBOUND", Qty: row.Qty, RefType: "INITIAL_IMPORT", RefID: batch.ID, RefNo: batch.BatchNo, OnHandAfter: stock.OnHandQty, AvailableAfter: stock.AvailableQty, OperatorID: op.ID, Remark: remark, UnitCost: unit.String(), Amount: amount.String(), AvgCostAfter: stock.AvgCost}); err != nil {
				return err
			}
			if err := s.fillWaiting(ctx, tx, q, tenantID, row.ProductID, row.SKUID, 0); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE stock_imports SET status='CONFIRMED',operator_id=$1,operator_name=$2,confirmed_at=now() WHERE tenant_id=$3 AND id=$4`, op.ID, op.Name, tenantID, batch.ID); err != nil {
			return err
		}
		result.Status, result.OperatorName = "CONFIRMED", op.Name
		now := time.Now()
		result.ConfirmedAt = &now
		return nil
	})
	return result, already, err
}

const stockImportColumns = `id,import_token,batch_no,import_type,source_file_name,external_batch_no,template_version,status,total_count,valid_count,warning_count,error_count,duplicate_count,operator_name,created_at,confirmed_at`
const stockImportSelect = `SELECT ` + stockImportColumns + ` FROM stock_imports`

type rowScanner interface{ Scan(...any) error }

func scanStockImport(row rowScanner) (StockImportBatch, error) {
	var out StockImportBatch
	var confirmed *time.Time
	err := row.Scan(&out.ID, &out.ImportToken, &out.BatchNo, &out.ImportType, &out.SourceFileName, &out.ExternalBatchNo, &out.TemplateVersion, &out.Status, &out.TotalCount, &out.ValidCount, &out.WarningCount, &out.ErrorCount, &out.DuplicateCount, &out.OperatorName, &out.CreatedAt, &confirmed)
	out.ConfirmedAt = confirmed
	return out, err
}

func loadStockImportRows(ctx context.Context, queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, tenantID, importID int64) ([]StockImportRow, error) {
	rows, err := queryer.Query(ctx, `SELECT row_number,warehouse_id,warehouse_code,warehouse_name,product_id,product_code,product_name,sku_id,sku_code,uom_id,uom_code,qty,unit_cost,currency,remark,verdict,issue_field,issue_value,issue_message FROM stock_import_rows WHERE tenant_id=$1 AND import_id=$2 ORDER BY row_number`, tenantID, importID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StockImportRow{}
	for rows.Next() {
		var row StockImportRow
		var field, value, message string
		if err := rows.Scan(&row.RowNumber, &row.WarehouseID, &row.WarehouseCode, &row.WarehouseName, &row.ProductID, &row.ProductCode, &row.ProductName, &row.SKUID, &row.SKUCode, &row.UomID, &row.UomCode, &row.Qty, &row.UnitCost, &row.Currency, &row.Remark, &row.Verdict, &field, &value, &message); err != nil {
			return nil, err
		}
		if message != "" {
			row.Issues = []StockImportIssue{{Field: field, Value: value, Message: message}}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListStockImports 返回可审计的导入批次历史。
func (s *Service) ListStockImports(ctx context.Context, tenantID int64, page, size int32) ([]StockImportBatch, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.pool.Query(ctx, `SELECT `+stockImportColumns+`,count(*) OVER() FROM stock_imports WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, tenantID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []StockImportBatch{}
	var total int64
	for rows.Next() {
		var batch StockImportBatch
		var confirmed *time.Time
		if err := rows.Scan(&batch.ID, &batch.ImportToken, &batch.BatchNo, &batch.ImportType, &batch.SourceFileName, &batch.ExternalBatchNo, &batch.TemplateVersion, &batch.Status, &batch.TotalCount, &batch.ValidCount, &batch.WarningCount, &batch.ErrorCount, &batch.DuplicateCount, &batch.OperatorName, &batch.CreatedAt, &confirmed, &total); err != nil {
			return nil, 0, err
		}
		batch.ConfirmedAt = confirmed
		out = append(out, batch)
	}
	return out, total, rows.Err()
}

// StockImportReport 重新生成某个租户内导入批次的逐行结果报告。
func (s *Service) StockImportReport(ctx context.Context, tenantID int64, token string) (string, []byte, error) {
	batch, err := scanStockImport(s.pool.QueryRow(ctx, stockImportSelect+` WHERE tenant_id=$1 AND import_token=$2`, tenantID, strings.TrimSpace(token)))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, apierr.NotFound("IV_IMPORT_NOT_FOUND", "导入批次不存在")
	}
	if err != nil {
		return "", nil, err
	}
	rows, err := loadStockImportRows(ctx, s.pool, tenantID, batch.ID)
	if err != nil {
		return "", nil, err
	}
	data, err := buildStockImportReport(rows)
	return batch.BatchNo + "-report.xlsx", data, err
}

// CancelStockImport 只允许放弃尚未入账的预检批次；已确认批次必须走调整流程纠正。
func (s *Service) CancelStockImport(ctx context.Context, tenantID int64, token string) (StockImportBatch, error) {
	batch, err := scanStockImport(s.pool.QueryRow(ctx, stockImportSelect+` WHERE tenant_id=$1 AND import_token=$2`, tenantID, strings.TrimSpace(token)))
	if errors.Is(err, pgx.ErrNoRows) {
		return StockImportBatch{}, apierr.NotFound("IV_IMPORT_NOT_FOUND", "导入批次不存在")
	}
	if err != nil {
		return StockImportBatch{}, err
	}
	if batch.Status == "CONFIRMED" {
		return StockImportBatch{}, apierr.Conflict("IV_IMPORT_CONFIRMED", "已确认批次不能撤销，请通过库存调整纠正")
	}
	if batch.Status != "CANCELLED" {
		if _, err := s.pool.Exec(ctx, `UPDATE stock_imports SET status='CANCELLED',cancelled_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, batch.ID); err != nil {
			return StockImportBatch{}, err
		}
		batch.Status = "CANCELLED"
	}
	return batch, nil
}
