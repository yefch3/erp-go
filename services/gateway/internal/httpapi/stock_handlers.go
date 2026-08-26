package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	ivv1 "github.com/sgao19/erp-go/gen/go/erp/inventory/v1"
)

func (s *Server) listWarehouses(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.ListWarehouses(r.Context(), &ivv1.ListWarehousesRequest{
		IncludeInactive: r.URL.Query().Get("include_inactive") == "true",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createWarehouse(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.CreateWarehouseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Stocks.CreateWarehouse(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateWarehouse(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.UpdateWarehouseRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Stocks.UpdateWarehouse(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getWarehouseSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.GetWarehouseSettings(r.Context(), &ivv1.GetWarehouseSettingsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateWarehouseSettings(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.UpdateWarehouseSettingsRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Stocks.UpdateWarehouseSettings(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listStocks(w http.ResponseWriter, r *http.Request) {
	warehouseID, _ := strconv.ParseInt(r.URL.Query().Get("warehouse_id"), 10, 64)
	productID, _ := strconv.ParseInt(r.URL.Query().Get("product_id"), 10, 64)
	skuID, _ := strconv.ParseInt(r.URL.Query().Get("sku_id"), 10, 64)
	resp, err := s.Stocks.ListStocks(r.Context(), &ivv1.ListStocksRequest{
		Page: pageFromQuery(r), WarehouseId: warehouseID,
		Keyword:     r.URL.Query().Get("keyword"),
		InStockOnly: r.URL.Query().Get("in_stock_only") == "true",
		ProductId:   productID, SkuId: skuID, StockState: r.URL.Query().Get("stock_state"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "inventory:stock:cost")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		redactStockCosts(resp.GetStocks())
	}
	s.writeProto(w, resp)
}

func (s *Server) getStock(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.GetStock(r.Context(), &ivv1.GetStockRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "inventory:stock:cost")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		redactStockCosts([]*ivv1.Stock{resp.GetStock()})
	}
	s.writeProto(w, resp)
}

func (s *Server) listStockLedger(w http.ResponseWriter, r *http.Request) {
	skuID, _ := strconv.ParseInt(r.URL.Query().Get("sku_id"), 10, 64)
	stockID, _ := strconv.ParseInt(r.URL.Query().Get("stock_id"), 10, 64)
	warehouseID, _ := strconv.ParseInt(r.URL.Query().Get("warehouse_id"), 10, 64)
	resp, err := s.Stocks.ListLedger(r.Context(), &ivv1.ListLedgerRequest{
		Page: pageFromQuery(r), StockId: stockID, WarehouseId: warehouseID,
		SkuId: skuID, Movement: r.URL.Query().Get("movement"), Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "inventory:stock:cost")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		for _, entry := range resp.GetEntries() {
			entry.UnitCost, entry.Amount, entry.AvgCostAfter, entry.CostCurrency = "", "", "", ""
		}
	}
	s.writeProto(w, resp)
}

func (s *Server) freezeStock(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.FreezeStockRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// protojson 解析正文时会重置消息，路径参数必须在解析后写入。
	req.Id = idFromPath(r)
	resp, err := s.Stocks.FreezeStock(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "inventory:stock:cost")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		redactStockCosts([]*ivv1.Stock{resp.GetStock()})
	}
	s.writeProto(w, resp)
}

func (s *Server) unfreezeStock(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.UnfreezeStockRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// 与冻结接口保持一致，避免正文解析覆盖 URL 中的库存 ID。
	req.Id = idFromPath(r)
	resp, err := s.Stocks.UnfreezeStock(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	allowed, err := s.hasPermission(r, "inventory:stock:cost")
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if !allowed {
		redactStockCosts([]*ivv1.Stock{resp.GetStock()})
	}
	s.writeProto(w, resp)
}

func (s *Server) downloadStockImportTemplate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.DownloadStockImportTemplate(r.Context(), &ivv1.DownloadStockImportTemplateRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	writeXLSXDownload(w, resp.GetFileName(), resp.GetFileData())
}

func (s *Server) previewInitialStockImport(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.PreviewInitialStockImportRequest{}
	// 2 MiB Excel 经 Base64 编码后会膨胀，导入预检单独允许 3 MiB JSON 请求体。
	if !s.decodeBodyLimit(w, r, req, 3<<20) {
		return
	}
	resp, err := s.Stocks.PreviewInitialStockImport(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmInitialStockImport(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.ConfirmInitialStockImport(r.Context(), &ivv1.ConfirmInitialStockImportRequest{ImportToken: chi.URLParam(r, "importToken")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listStockImports(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.ListStockImports(r.Context(), &ivv1.ListStockImportsRequest{Page: pageFromQuery(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) downloadStockImportReport(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.DownloadStockImportReport(r.Context(), &ivv1.DownloadStockImportReportRequest{ImportToken: chi.URLParam(r, "importToken")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	writeXLSXDownload(w, resp.GetFileName(), resp.GetFileData())
}

func (s *Server) cancelStockImport(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.CancelStockImport(r.Context(), &ivv1.CancelStockImportRequest{ImportToken: chi.URLParam(r, "importToken")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func writeXLSXDownload(w http.ResponseWriter, name string, data []byte) {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", attachmentDisposition(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func redactStockCosts(stocks []*ivv1.Stock) {
	for _, stock := range stocks {
		if stock != nil {
			stock.AvgCost, stock.TotalCost, stock.CostCurrency = "", "", ""
		}
	}
}

func (s *Server) receiveStock(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.ReceiveStockRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Stocks.ReceiveStock(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippable(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.ListShippable(r.Context(), &ivv1.ListShippableRequest{
		Page: pageFromQuery(r), Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippableLines(w http.ResponseWriter, r *http.Request) {
	contractID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Stocks.ListShippableLines(r.Context(), &ivv1.ListShippableLinesRequest{
		ContractId: contractID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listOutbounds(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Stocks.ListOutbounds(r.Context(), &ivv1.ListOutboundsRequest{
		Page:   pageFromQuery(r),
		Status: r.URL.Query().Get("status"), Keyword: r.URL.Query().Get("keyword"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listOutboundItems(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Stocks.ListOutboundItems(r.Context(), &ivv1.ListOutboundItemsRequest{
		OutboundId: id,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createOutbound(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.CreateOutboundRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Stocks.CreateOutbound(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmOutbound(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Stocks.ConfirmOutbound(r.Context(), &ivv1.ConfirmOutboundRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelOutbound(w http.ResponseWriter, r *http.Request) {
	req := &ivv1.CancelOutboundRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	resp, err := s.Stocks.CancelOutbound(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
