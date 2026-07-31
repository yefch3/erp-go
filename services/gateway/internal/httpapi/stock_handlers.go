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

func (s *Server) listStocks(w http.ResponseWriter, r *http.Request) {
	warehouseID, _ := strconv.ParseInt(r.URL.Query().Get("warehouse_id"), 10, 64)
	resp, err := s.Stocks.ListStocks(r.Context(), &ivv1.ListStocksRequest{
		Page: pageFromQuery(r), WarehouseId: warehouseID,
		Keyword:     r.URL.Query().Get("keyword"),
		InStockOnly: r.URL.Query().Get("in_stock_only") == "true",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listStockLedger(w http.ResponseWriter, r *http.Request) {
	skuID, _ := strconv.ParseInt(r.URL.Query().Get("sku_id"), 10, 64)
	resp, err := s.Stocks.ListLedger(r.Context(), &ivv1.ListLedgerRequest{
		Page: pageFromQuery(r), SkuId: skuID, Movement: r.URL.Query().Get("movement"),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
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
