package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// Each route has its own IAM permission at the gateway. The service still
// applies tenant and owner checks to writes; browser-supplied IDs are never
// treated as authority.
func (s *Server) dailyPriceCall(w http.ResponseWriter, r *http.Request, command map[string]any) {
	if s.DailyPrices == nil {
		s.writeError(w, 503, "DAILY_PRICE_UNAVAILABLE", "每日基价服务不可用")
		return
	}
	data, _ := json.Marshal(command)
	resp, err := s.DailyPrices.Execute(r.Context(), &prv1.DailyPriceServiceExecuteRequest{CommandJson: string(data)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: json.RawMessage(resp.GetResultJson())})
}

func (s *Server) dailyPriceBody(w http.ResponseWriter, r *http.Request, action string) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
	if err != nil {
		s.writeError(w, 400, "DAILY_PRICE_SIZE", "一次提交的数据过多")
		return
	}
	var command map[string]any
	if json.Unmarshal(data, &command) != nil || command == nil {
		s.writeError(w, 400, "DAILY_PRICE_INPUT", "每日基价输入无效")
		return
	}
	command["action"] = action
	s.dailyPriceCall(w, r, command)
}
func (s *Server) dailyPriceConfig(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceCall(w, r, map[string]any{"action": "config"})
}
func (s *Server) dailyPriceDay(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceCall(w, r, map[string]any{"action": "day", "date": r.URL.Query().Get("date")})
}
func (s *Server) dailyPriceTrend(w http.ResponseWriter, r *http.Request) {
	productID, _ := strconv.ParseInt(r.URL.Query().Get("productId"), 10, 64)
	spreadID, _ := strconv.ParseInt(r.URL.Query().Get("spreadProductId"), 10, 64)
	supplierIDs := []int64{}
	for _, value := range append(r.URL.Query()["supplierId"], r.URL.Query()["supplierId[]"]...) {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 {
			supplierIDs = append(supplierIDs, id)
		}
	}
	s.dailyPriceCall(w, r, map[string]any{"action": "trend", "from": r.URL.Query().Get("from"), "to": r.URL.Query().Get("to"), "productId": productID, "spreadProductId": spreadID, "supplierIds": supplierIDs})
}
func (s *Server) saveDailyPrices(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceBody(w, r, "savePrices")
}
func (s *Server) saveDailySpreads(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceBody(w, r, "saveSpreads")
}
func (s *Server) configureDailyPrice(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceBody(w, r, "configure")
}
func (s *Server) deleteDailyPriceDimension(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceDelete(w, r, "deleteDimension")
}
func (s *Server) deleteDailyPrice(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceDelete(w, r, "deletePrice")
}
func (s *Server) deleteDailySpread(w http.ResponseWriter, r *http.Request) {
	s.dailyPriceDelete(w, r, "deleteSpread")
}
func (s *Server) dailyPriceDelete(w http.ResponseWriter, r *http.Request, action string) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, 400, "DAILY_PRICE_ID", "记录 ID 无效")
		return
	}
	s.dailyPriceCall(w, r, map[string]any{"action": action, "id": id})
}
