package httpapi

import (
	"fmt"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/shopspring/decimal"
	"strings"
)

func contractProgressKey(item *exv1.ContractItem, standard bool) string {
	spec := ""
	if standard {
		spec = item.Spec
	}
	fallback := ""
	if item.ProductId == 0 {
		fallback = item.ProductName + "\x00" + item.UomCode
	}
	return fmt.Sprintf("%d/%d/%s/%s", item.ProductId, item.SkuId, spec, fallback)
}

// Warehouse dispatch and formal transport describe overlapping observations of
// the same contracted goods. Reconcile cumulative coverage, do not add them and
// double the quantity merely because both departments recorded the movement.
func mergeContractDispatchProgress(view *exv1.GetContractResponse, lines []*shippingv1.ContractDispatchLine) bool {
	standard := view.GetContract().GetEntrySource() == "STANDARD"
	byID := map[int64]string{}
	byLine := map[int32]string{}
	opening := map[string]decimal.Decimal{}
	groups := map[string][]*exv1.ContractItem{}
	for _, item := range view.Items {
		key := contractProgressKey(item, standard)
		byID[item.Id] = key
		byLine[item.LineNo] = key
		groups[key] = append(groups[key], item)
		q, err := decimal.NewFromString(item.OpeningShippedQty)
		if item.OpeningShippedQty != "" && err != nil {
			return false
		}
		opening[key] = opening[key].Add(q)
	}
	shipped := map[string]decimal.Decimal{}
	for _, line := range lines {
		key, ok := byID[line.ContractItemId]
		if !ok {
			matches := map[string]bool{}
			for candidate, items := range groups {
				for _, item := range items {
					if item.ProductName == line.ProductName && item.Spec == line.Specification && item.UomCode == line.UomCode && (line.ProductCode == "" || item.ProductCode == line.ProductCode) {
						matches[candidate] = true
					}
				}
			}
			if len(matches) != 1 {
				return false
			}
			for candidate := range matches {
				key = candidate
			}
		}
		qty, err := decimal.NewFromString(strings.TrimSpace(line.Quantity))
		if err != nil || !qty.IsPositive() {
			return false
		}
		shipped[key] = shipped[key].Add(qty)
	}
	// Validate everything before mutating the response, so an unavailable or
	// ambiguous projection cannot leave partially updated quantities in the UI.
	type next struct {
		row             *exv1.ShipmentProgress
		sent, remaining string
	}
	out := []next{}
	for _, row := range view.Shipments {
		key, ok := byLine[row.LineNo]
		if !ok {
			return false
		}
		sent, err := decimal.NewFromString(row.ShippedQty)
		if err != nil {
			return false
		}
		sold, err := decimal.NewFromString(row.Qty)
		if err != nil {
			return false
		}
		if actual, exists := shipped[key]; exists {
			actual = actual.Add(opening[key])
			if actual.GreaterThan(sent) {
				sent = actual
			}
		}
		out = append(out, next{row, sent.StringFixed(4), sold.Sub(sent).StringFixed(4)})
	}
	for _, n := range out {
		n.row.ShippedQty = n.sent
		n.row.RemainingQty = n.remaining
	}
	return true
}
