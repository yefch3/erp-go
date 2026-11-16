package httpapi

import (
	"net/http"
	"strings"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

type shippingPurchaseRemark struct {
	SupplierName        string `json:"supplierName"`
	PurchaseOrderRemark string `json:"purchaseOrderRemark"`
}

func (s *Server) getHandoffPurchaseRemarks(w http.ResponseWriter, r *http.Request) {
	// Always resolve the contract through the authorized shipping resource.
	// Ignore query/body contract IDs; never call ListOrders/GetOrder here.
	handoff, err := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	h := handoff.GetHandoff()
	items := []shippingPurchaseRemark{}
	if h.GetContractId() <= 0 || strings.TrimSpace(h.GetContractNo()) == "" {
		s.writeJSON(w, map[string]any{"items": items})
		return
	}
	response, err := s.Orders.ListShippingPurchaseRemarks(r.Context(), &prv1.ListShippingPurchaseRemarksRequest{ContractId: h.GetContractId(), ContractNo: h.GetContractNo()})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	for _, row := range response.GetItems() {
		items = append(items, shippingPurchaseRemark{SupplierName: row.GetSupplierName(), PurchaseOrderRemark: row.GetPurchaseOrderRemark()})
	}
	s.writeJSON(w, map[string]any{"items": items})
}
