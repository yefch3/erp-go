package httpapi

import (
	"net/http"
	"strings"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

type shippingHistoricalPurchase struct {
	PONo             string `json:"poNo"`
	SupplierName     string `json:"supplierName"`
	ExportContractNo string `json:"exportContractNo"`
	DestinationPort  string `json:"destinationPort"`
	Remark           string `json:"remark"`
}

func (s *Server) getHandoffHistoricalPurchases(w http.ResponseWriter, r *http.Request) {
	// Authorize the handoff first; ignore caller-supplied contract parameters.
	handoff, err := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	h := handoff.GetHandoff()
	items := []shippingHistoricalPurchase{}
	if h.GetContractId() > 0 && strings.TrimSpace(h.GetContractNo()) != "" {
		result, e := s.Orders.ListShippingHistoricalPurchases(r.Context(), &prv1.ListShippingHistoricalPurchasesRequest{ContractNo: h.GetContractNo()})
		if e != nil {
			s.writeGRPCError(w, e)
			return
		}
		for _, row := range result.GetItems() {
			items = append(items, shippingHistoricalPurchase{PONo: row.GetPoNo(), SupplierName: row.GetSupplierName(), ExportContractNo: row.GetExportContractNo(), DestinationPort: row.GetDestinationPort(), Remark: row.GetRemark()})
		}
	}
	s.writeJSON(w, map[string]any{"items": items})
}
