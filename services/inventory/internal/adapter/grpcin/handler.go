// Package grpcin exposes inventory over gRPC. Quantities travel as strings:
// they are decimals, and a float would round them.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	ivv1 "github.com/sgao19/erp-go/gen/go/erp/inventory/v1"
	"github.com/sgao19/erp-go/pkg/deadletter"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/inventory/internal/app"
	"github.com/sgao19/erp-go/services/inventory/internal/store"
)

type Handler struct {
	ivv1.UnimplementedStockServiceServer
	svc  *app.Service
	dead *deadletter.Console
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListWarehouses(ctx context.Context, req *ivv1.ListWarehousesRequest) (*ivv1.ListWarehousesResponse, error) {
	rows, err := h.svc.ListWarehouses(ctx, grpcx.TenantID(ctx), req.GetIncludeInactive())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.Warehouse, 0, len(rows))
	for _, r := range rows {
		profile, err := h.svc.GetWarehouseProfile(ctx, grpcx.TenantID(ctx), r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, warehouseProfile(profile))
	}
	return &ivv1.ListWarehousesResponse{Warehouses: out}, nil
}

// CreateWarehouse 创建完整仓库档案，并由应用层统一保存联系人和审计记录。
func (h *Handler) CreateWarehouse(ctx context.Context, req *ivv1.CreateWarehouseRequest) (*ivv1.CreateWarehouseResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.CreateWarehouse(ctx, grpcx.TenantID(ctx), warehouseInput(req.GetWarehouse()), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &ivv1.CreateWarehouseResponse{Warehouse: warehouseProfile(row)}, nil
}

// UpdateWarehouse 更新仓库档案；停用、联系人和核算方式等变化均写入历史。
func (h *Handler) UpdateWarehouse(ctx context.Context, req *ivv1.UpdateWarehouseRequest) (*ivv1.UpdateWarehouseResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.UpdateWarehouse(ctx, grpcx.TenantID(ctx), req.GetId(), warehouseInput(req.GetWarehouse()), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &ivv1.UpdateWarehouseResponse{Warehouse: warehouseProfile(row)}, nil
}

func (h *Handler) GetWarehouseSettings(ctx context.Context, _ *ivv1.GetWarehouseSettingsRequest) (*ivv1.GetWarehouseSettingsResponse, error) {
	row, err := h.svc.GetWarehouseSettings(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	return &ivv1.GetWarehouseSettingsResponse{Settings: warehouseSettings(row)}, nil
}

// UpdateWarehouseSettings 保存公司级仓库模式，供采购、库存和收货流程共同判断。
func (h *Handler) UpdateWarehouseSettings(ctx context.Context, req *ivv1.UpdateWarehouseSettingsRequest) (*ivv1.UpdateWarehouseSettingsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.UpdateWarehouseSettings(ctx, grpcx.TenantID(ctx), app.WarehouseSettingsInput{
		UsageMode: req.GetUsageMode(), AllowDirectDelivery: req.GetAllowDirectDelivery(),
		AllowInventory: req.GetAllowInventory(), DefaultWarehouseID: req.GetDefaultWarehouseId(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &ivv1.UpdateWarehouseSettingsResponse{Settings: warehouseSettings(row)}, nil
}

func warehouseInput(in *ivv1.WarehouseInput) app.WarehouseInput {
	if in == nil {
		return app.WarehouseInput{}
	}
	contacts := make([]app.WarehouseContactInput, 0, len(in.GetContacts()))
	for _, c := range in.GetContacts() {
		contacts = append(contacts, app.WarehouseContactInput{ContactType: c.GetContactType(), EmployeeID: c.GetEmployeeId(), Name: c.GetName(), Phone: c.GetPhone(), Email: c.GetEmail(), IsPrimary: c.GetIsPrimary(), Status: c.GetStatus()})
	}
	return app.WarehouseInput{Code: in.GetCode(), Name: in.GetName(), ProfileType: in.GetProfileType(), Address: in.GetAddress(), CountryCode: in.GetCountryCode(), City: in.GetCity(), Timezone: in.GetTimezone(), AccountingMode: in.GetAccountingMode(), Status: in.GetStatus(), Reason: in.GetChangeReason(), Contacts: contacts}
}

func warehouseProfile(row app.WarehouseProfile) *ivv1.Warehouse {
	out := &ivv1.Warehouse{Id: row.ID, Code: row.Code, Name: row.Name, WhType: row.WhType, Address: row.Address, Status: row.Status, ProfileType: row.ProfileType, CountryCode: row.CountryCode, City: row.City, Timezone: row.Timezone, AccountingMode: row.AccountingMode}
	for _, c := range row.Contacts {
		employeeID := int64(0)
		if c.EmployeeID != nil {
			employeeID = *c.EmployeeID
		}
		out.Contacts = append(out.Contacts, &ivv1.WarehouseContact{Id: c.ID, ContactType: c.ContactType, EmployeeId: employeeID, Name: c.Name, Phone: c.Phone, Email: c.Email, IsPrimary: c.IsPrimary, Status: c.Status})
	}
	return out
}

func warehouseSettings(row store.GetWarehouseSettingsRow) *ivv1.WarehouseSettings {
	defaultID := int64(0)
	if row.DefaultWarehouseID != nil {
		defaultID = *row.DefaultWarehouseID
	}
	return &ivv1.WarehouseSettings{UsageMode: row.UsageMode, AllowDirectDelivery: row.AllowDirectDelivery, AllowInventory: row.AllowInventory, DefaultWarehouseId: defaultID, UpdatedAt: ts(row.UpdatedAt)}
}

func (h *Handler) ListStocks(ctx context.Context, req *ivv1.ListStocksRequest) (*ivv1.ListStocksResponse, error) {
	rows, total, err := h.svc.ListStocks(ctx, grpcx.TenantID(ctx), app.StockFilter{
		WarehouseID: req.GetWarehouseId(), Keyword: req.GetKeyword(),
		InStockOnly: req.GetInStockOnly(), ProductID: req.GetProductId(),
		SkuID: req.GetSkuId(), StockState: req.GetStockState(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.Stock, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ivv1.Stock{
			Id: r.ID, WarehouseId: r.WarehouseID, WarehouseCode: r.WarehouseCode,
			WarehouseName: r.WarehouseName, ProductId: r.ProductID, SkuId: r.SkuID,
			ProductCode: r.ProductCode, ProductName: r.ProductName, UomCode: r.UomCode,
			OnHandQty: r.OnHandQty, ReservedQty: r.ReservedQty, LockedQty: r.LockedQty,
			FrozenQty: r.FrozenQty, AvailableQty: r.AvailableQty, UpdatedAt: ts(r.UpdatedAt),
			AvgCost: r.AvgCost, TotalCost: r.TotalCost, CostCurrency: r.CostCurrency,
			InTransitQty: r.InTransitQty,
		})
	}
	return &ivv1.ListStocksResponse{Stocks: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

// GetStock 返回一条租户隔离的库存快照。
func (h *Handler) GetStock(ctx context.Context, req *ivv1.GetStockRequest) (*ivv1.GetStockResponse, error) {
	r, err := h.svc.GetStock(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &ivv1.GetStockResponse{Stock: &ivv1.Stock{
		Id: r.ID, WarehouseId: r.WarehouseID, WarehouseCode: r.WarehouseCode,
		WarehouseName: r.WarehouseName, ProductId: r.ProductID, SkuId: r.SkuID,
		ProductCode: r.ProductCode, ProductName: r.ProductName, UomCode: r.UomCode,
		OnHandQty: r.OnHandQty, ReservedQty: r.ReservedQty, LockedQty: r.LockedQty,
		FrozenQty: r.FrozenQty, InTransitQty: r.InTransitQty, AvailableQty: r.AvailableQty,
		UpdatedAt: ts(r.UpdatedAt), AvgCost: r.AvgCost, TotalCost: r.TotalCost,
		CostCurrency: r.CostCurrency,
	}}, nil
}

func (h *Handler) ListLedger(ctx context.Context, req *ivv1.ListLedgerRequest) (*ivv1.ListLedgerResponse, error) {
	rows, total, err := h.svc.ListLedger(ctx, grpcx.TenantID(ctx), app.LedgerFilter{
		StockID: req.GetStockId(), WarehouseID: req.GetWarehouseId(),
		SkuID: req.GetSkuId(), Movement: req.GetMovement(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.LedgerEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ivv1.LedgerEntry{
			Id: r.ID, WarehouseId: r.WarehouseID, SkuId: r.SkuID,
			ProductCode: r.ProductCode, ProductName: r.ProductName,
			Movement: r.Movement, Qty: r.Qty, RefType: r.RefType, RefId: r.RefID,
			RefNo: r.RefNo, OnHandAfter: r.OnHandAfter, AvailableAfter: r.AvailableAfter,
			Remark: r.Remark, OccurredAt: ts(r.OccurredAt),
			UnitCost: r.UnitCost, Amount: r.Amount, AvgCostAfter: r.AvgCostAfter,
			CostCurrency: r.CostCurrency,
		})
	}
	return &ivv1.ListLedgerResponse{Entries: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

// FreezeStock 冻结仍可分配的库存。
func (h *Handler) FreezeStock(ctx context.Context, req *ivv1.FreezeStockRequest) (*ivv1.FreezeStockResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.FreezeStock(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetQty(), req.GetReason(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	resp, err := h.GetStock(ctx, &ivv1.GetStockRequest{Id: req.GetId()})
	if err != nil {
		return nil, err
	}
	return &ivv1.FreezeStockResponse{Stock: resp.GetStock()}, nil
}

// UnfreezeStock 释放已冻结库存。
func (h *Handler) UnfreezeStock(ctx context.Context, req *ivv1.UnfreezeStockRequest) (*ivv1.UnfreezeStockResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.UnfreezeStock(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetQty(), req.GetReason(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	resp, err := h.GetStock(ctx, &ivv1.GetStockRequest{Id: req.GetId()})
	if err != nil {
		return nil, err
	}
	return &ivv1.UnfreezeStockResponse{Stock: resp.GetStock()}, nil
}

// DownloadStockImportTemplate 返回当前受支持的期初库存模板。
func (h *Handler) DownloadStockImportTemplate(context.Context, *ivv1.DownloadStockImportTemplateRequest) (*ivv1.DownloadStockImportTemplateResponse, error) {
	name, data, version, err := app.BuildInitialStockTemplate()
	if err != nil {
		return nil, err
	}
	return &ivv1.DownloadStockImportTemplateResponse{FileName: name, FileData: data, TemplateVersion: version}, nil
}

// PreviewInitialStockImport 只做解析、业务校验和预检留档，不改库存。
func (h *Handler) PreviewInitialStockImport(ctx context.Context, req *ivv1.PreviewInitialStockImportRequest) (*ivv1.PreviewInitialStockImportResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	preview, err := h.svc.PreviewInitialStockImport(ctx, grpcx.TenantID(ctx), req.GetFileData(), req.GetSourceFileName(), req.GetExternalBatchNo(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	rows := make([]*ivv1.StockImportRow, 0, len(preview.Rows))
	for _, row := range preview.Rows {
		rows = append(rows, stockImportRowProto(row))
	}
	return &ivv1.PreviewInitialStockImportResponse{Batch: stockImportBatchProto(preview.Batch), Rows: rows, ErrorFileName: preview.ErrorFileName, ErrorFileData: preview.ErrorFileData}, nil
}

// ConfirmInitialStockImport 将已通过的预检批次原子写入库存和流水。
func (h *Handler) ConfirmInitialStockImport(ctx context.Context, req *ivv1.ConfirmInitialStockImportRequest) (*ivv1.ConfirmInitialStockImportResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	batch, already, err := h.svc.ConfirmInitialStockImport(ctx, grpcx.TenantID(ctx), req.GetImportToken(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &ivv1.ConfirmInitialStockImportResponse{Batch: stockImportBatchProto(batch), AlreadyConfirmed: already}, nil
}

func (h *Handler) ListStockImports(ctx context.Context, req *ivv1.ListStockImportsRequest) (*ivv1.ListStockImportsResponse, error) {
	batches, total, err := h.svc.ListStockImports(ctx, grpcx.TenantID(ctx), req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.StockImportBatch, 0, len(batches))
	for _, batch := range batches {
		out = append(out, stockImportBatchProto(batch))
	}
	return &ivv1.ListStockImportsResponse{Batches: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *Handler) DownloadStockImportReport(ctx context.Context, req *ivv1.DownloadStockImportReportRequest) (*ivv1.DownloadStockImportReportResponse, error) {
	name, data, err := h.svc.StockImportReport(ctx, grpcx.TenantID(ctx), req.GetImportToken())
	if err != nil {
		return nil, err
	}
	return &ivv1.DownloadStockImportReportResponse{FileName: name, FileData: data}, nil
}

func (h *Handler) CancelStockImport(ctx context.Context, req *ivv1.CancelStockImportRequest) (*ivv1.CancelStockImportResponse, error) {
	batch, err := h.svc.CancelStockImport(ctx, grpcx.TenantID(ctx), req.GetImportToken())
	if err != nil {
		return nil, err
	}
	return &ivv1.CancelStockImportResponse{Batch: stockImportBatchProto(batch)}, nil
}

func stockImportBatchProto(batch app.StockImportBatch) *ivv1.StockImportBatch {
	confirmed := ""
	if batch.ConfirmedAt != nil {
		confirmed = batch.ConfirmedAt.Format(time.RFC3339)
	}
	return &ivv1.StockImportBatch{Id: batch.ID, ImportToken: batch.ImportToken, BatchNo: batch.BatchNo, ImportType: batch.ImportType, SourceFileName: batch.SourceFileName, ExternalBatchNo: batch.ExternalBatchNo, TemplateVersion: batch.TemplateVersion, Status: batch.Status, TotalCount: batch.TotalCount, ValidCount: batch.ValidCount, WarningCount: batch.WarningCount, ErrorCount: batch.ErrorCount, DuplicateCount: batch.DuplicateCount, OperatorName: batch.OperatorName, CreatedAt: batch.CreatedAt.Format(time.RFC3339), ConfirmedAt: confirmed}
}

func stockImportRowProto(row app.StockImportRow) *ivv1.StockImportRow {
	issues := make([]*ivv1.StockImportIssue, 0, len(row.Issues))
	for _, issue := range row.Issues {
		issues = append(issues, &ivv1.StockImportIssue{Field: issue.Field, Value: issue.Value, Message: issue.Message})
	}
	return &ivv1.StockImportRow{RowNumber: row.RowNumber, WarehouseCode: row.WarehouseCode, WarehouseName: row.WarehouseName, ProductCode: row.ProductCode, ProductName: row.ProductName, SkuCode: row.SKUCode, Qty: row.Qty, UomCode: row.UomCode, UnitCost: row.UnitCost, Currency: row.Currency, Remark: row.Remark, Verdict: row.Verdict, Issues: issues}
}

func (h *Handler) ReceiveStock(ctx context.Context, req *ivv1.ReceiveStockRequest) (*ivv1.ReceiveStockResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.ReceiveLine, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		lines = append(lines, app.ReceiveLine{
			ProductID: l.GetProductId(), SkuID: l.GetSkuId(),
			ProductCode: l.GetProductCode(), ProductName: l.GetProductName(),
			UomID: l.GetUomId(), UomCode: l.GetUomCode(), Qty: l.GetQty(),
			UnitCost: l.GetUnitCost(),
		})
	}
	if err := h.svc.ReceiveStock(ctx, grpcx.TenantID(ctx), req.GetWarehouseId(), lines,
		req.GetRemark(), app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &ivv1.ReceiveStockResponse{}, nil
}

func ts(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func (h *Handler) ListShippable(ctx context.Context, req *ivv1.ListShippableRequest) (*ivv1.ListShippableResponse, error) {
	rows, total, err := h.svc.ListShippableContracts(ctx, grpcx.TenantID(ctx), req.GetKeyword(),
		req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.ShippableContract, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ivv1.ShippableContract{
			ContractId: r.ContractID, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, DeliveryDate: r.DeliveryDate,
			LineCount: r.LineCount, DemandQty: r.DemandQty, ReadyQty: r.ReadyQty,
			ShortageQty: r.ShortageQty, LockedQty: r.LockedQty, ShippedQty: r.ShippedQty,
		})
	}
	return &ivv1.ListShippableResponse{Contracts: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *Handler) ListShippableLines(ctx context.Context, req *ivv1.ListShippableLinesRequest) (*ivv1.ListShippableLinesResponse, error) {
	rows, err := h.svc.ShippableLines(ctx, grpcx.TenantID(ctx), req.GetContractId())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.ShippableLine, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ivv1.ShippableLine{
			ReservationId: r.ID, ContractItemId: r.RefLineID, ProductId: r.ProductID,
			SkuId: r.SkuID, ProductCode: r.ProductCode, ProductName: r.ProductName,
			UomCode: r.UomCode, DemandQty: r.DemandQty, ReservedQty: r.ReservedQty,
			LockedQty: r.LockedQty, ShippedQty: r.ShippedQty, ShortageQty: r.ShortageQty,
			ReadyQty: r.ReadyQty, AvailableQty: r.AvailableQty, Status: r.Status,
		})
	}
	return &ivv1.ListShippableLinesResponse{Lines: out}, nil
}

func (h *Handler) CreateOutbound(ctx context.Context, req *ivv1.CreateOutboundRequest) (*ivv1.CreateOutboundResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.OutboundLine, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		lines = append(lines, app.OutboundLine{RefLineID: l.GetContractItemId(), Qty: l.GetQty()})
	}
	row, err := h.svc.CreateOutbound(ctx, grpcx.TenantID(ctx), app.CreateOutboundInput{
		ContractID: req.GetContractId(), OutboundType: req.GetOutboundType(),
		Remark: req.GetRemark(), Lines: lines,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &ivv1.CreateOutboundResponse{
		Id: row.ID, OutboundNo: row.OutboundNo, Status: row.Status,
	}, nil
}

func (h *Handler) ConfirmOutbound(ctx context.Context, req *ivv1.ConfirmOutboundRequest) (*ivv1.ConfirmOutboundResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.ConfirmOutbound(ctx, grpcx.TenantID(ctx), req.GetId(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &ivv1.ConfirmOutboundResponse{}, nil
}

func (h *Handler) CancelOutbound(ctx context.Context, req *ivv1.CancelOutboundRequest) (*ivv1.CancelOutboundResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.CancelOutbound(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetReason(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &ivv1.CancelOutboundResponse{}, nil
}

func (h *Handler) ListOutbounds(ctx context.Context, req *ivv1.ListOutboundsRequest) (*ivv1.ListOutboundsResponse, error) {
	rows, total, err := h.svc.ListOutbounds(ctx, grpcx.TenantID(ctx), app.OutboundFilter{
		Status: req.GetStatus(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.Outbound, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ivv1.Outbound{
			Id: r.ID, OutboundNo: r.OutboundNo, OutboundType: r.OutboundType,
			ContractId: r.RefID, ContractNo: r.RefNo, CustomerName: r.CustomerName,
			Status: r.Status, OperatorName: r.OperatorName, Remark: r.Remark,
			CancelledReason: r.CancelledReason, ItemCount: r.ItemCount, TotalQty: r.TotalQty,
			ConfirmedAt: ts(r.ConfirmedAt), CreatedAt: ts(r.CreatedAt),
		})
	}
	return &ivv1.ListOutboundsResponse{Outbounds: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *Handler) ListOutboundItems(ctx context.Context, req *ivv1.ListOutboundItemsRequest) (*ivv1.ListOutboundItemsResponse, error) {
	rows, err := h.svc.OutboundItems(ctx, grpcx.TenantID(ctx), req.GetOutboundId())
	if err != nil {
		return nil, err
	}
	out := make([]*ivv1.OutboundItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, &ivv1.OutboundItem{
			Id: r.ID, ContractItemId: r.RefLineID, ProductId: r.ProductID, SkuId: r.SkuID,
			ProductCode: r.ProductCode, ProductName: r.ProductName,
			UomCode: r.UomCode, Qty: r.Qty,
		})
	}
	return &ivv1.ListOutboundItemsResponse{Items: out}, nil
}
