package httpapi

import (
	"net/http"
	"strconv"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

type handoffPurchaseDelivery struct {
	OrderID      string `json:"orderId"`
	PONo         string `json:"poNo"`
	SupplierName string `json:"supplierName"`
	ProductName  string `json:"productName"`
	Spec         string `json:"spec"`
	Qty          string `json:"qty"`
	DeliveredQty string `json:"deliveredQty"`
	UOMCode      string `json:"uomCode"`
}

// Read-only, whole-PO progress for contract goods carried by this
// batch. These quantities are not allocations to a particular vessel/batch.
// Every lookup retains procurement's tenant and data-scope authorization.
func (s *Server) getHandoffPurchaseDeliveries(w http.ResponseWriter, r *http.Request) {
	handoff, err := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	h := handoff.GetHandoff()
	out := []handoffPurchaseDelivery{}
	if h.GetContractId() <= 0 {
		s.writeJSON(w, map[string]any{"items": out})
		return
	}
	// Purchase-order readers need not have access to the buyer's requirement
	// inbox. Resolve visible orders instead, retaining order data scope and
	// the contract provenance already returned on each authorized order line.
	for page := int32(1); ; page++ {
		orders, e := s.Orders.ListOrders(r.Context(), &prv1.ListOrdersRequest{Page: &commonv1.PageRequest{Page: page, PageSize: 100}})
		if e != nil {
			s.writeGRPCError(w, e)
			return
		}
		for _, brief := range orders.GetOrders() {
			if brief.GetFulfillmentMode() == "WAREHOUSE" || brief.GetBusinessType() == "LOGISTICS" {
				continue
			}
			switch brief.GetStatus() {
			case "ORDERED", "PARTIALLY_RECEIVED", "RECEIVED":
			default:
				continue
			}
			// Contract-created orders carry a stable source ID. Legacy/mixed orders
			// have no header source, so their individual line provenance is checked.
			if brief.GetSourceBusinessId() > 0 && brief.GetSourceBusinessId() != h.GetContractId() {
				continue
			}
			detail, e := s.Orders.GetOrder(r.Context(), &prv1.GetOrderRequest{Id: brief.GetId()})
			if e != nil {
				s.writeGRPCError(w, e)
				return
			}
			order := detail.GetOrder()
			for _, line := range detail.GetItems() {
				if line.GetContractNo() != h.GetContractNo() {
					continue
				}
				matched := false
				for _, cargo := range h.GetCargoItems() {
					if line.GetProductName() == cargo.GetProductName() && line.GetSpec() == cargo.GetSpecification() && line.GetUomCode() == cargo.GetUomCode() {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
				out = append(out, handoffPurchaseDelivery{OrderID: strconv.FormatInt(order.GetId(), 10), PONo: order.GetPoNo(), SupplierName: order.GetSupplierName(), ProductName: line.GetProductName(), Spec: line.GetSpec(), Qty: line.GetQty(), DeliveredQty: line.GetReceivedQty(), UOMCode: line.GetUomCode()})
			}
		}
		if int64(page)*100 >= orders.GetMeta().GetTotal() {
			break
		}
	}
	s.writeJSON(w, map[string]any{"items": out})
}
