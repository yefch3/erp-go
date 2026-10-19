package app

import (
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
)

// Confirm the staged customer snapshot, never all alternative supplier quotes.
// One AGREED alternative is required for each staged inquiry product/specification.
func categoryContractBody(b OfferBody) (OfferBody, error) {
	snapshots := map[string]OfferSelectionSnapshot{}
	required := map[string]bool{}
	for _, s := range b.CustomerSelections {
		key := s.ProductID + ":" + s.Category + ":" + s.QuoteID
		if _, duplicate := snapshots[key]; duplicate {
			return b, apierr.Invalid("OFFER_SELECTION_DUPLICATE", "候选方案重复，请重新保存选择")
		}
		snapshots[key] = s
		required[s.ProductID] = true
	}
	if len(required) == 0 {
		return b, apierr.Invalid("OFFER_CATEGORY_EMPTY", "请先保存客户报价方案")
	}
	selected := map[string]bool{}
	lines := []OfferLine{}
	total := decimal.Zero
	for _, n := range b.Negotiations {
		if n.Status != "AGREED" {
			continue
		}
		s, ok := snapshots[n.ProductID+":"+n.Category+":"+n.QuoteID]
		if !ok || selected[n.ProductID] {
			return b, apierr.Invalid("OFFER_ACCEPTED_SELECTION", "每个产品规格只能确认一份有效成交方案")
		}
		price := strings.TrimSpace(n.ProposedPrice)
		if price == "" {
			price = strings.TrimSpace(n.InitialPrice)
		}
		if price == "" {
			price = strings.TrimSpace(s.CFRUnitPrice)
		}
		unitPrice, pe := decimal.NewFromString(price)
		qty, qe := decimal.NewFromString(s.Product.Quantity)
		if pe != nil || unitPrice.IsNegative() || qe != nil || !qty.IsPositive() || strings.TrimSpace(s.Product.Unit) == "" || strings.TrimSpace(s.Product.Product) == "" {
			return b, apierr.Invalid("OFFER_ACCEPTED_LINE", "请核对成交产品名称、单位、数量和最终对客单价")
		}
		if unitPrice.Exponent() < -4 {
			return b, apierr.Invalid("OFFER_ACCEPTED_PRICE_PRECISION", "成交单价最多保留四位小数")
		}
		amount := qty.Mul(unitPrice).Round(2)
		lines = append(lines, OfferLine{OfferProduct: s.Product, FactoryQuoteID: s.QuoteID, UnitPrice: unitPrice.StringFixed(4), Amount: amount.StringFixed(2)})
		total = total.Add(amount)
		selected[n.ProductID] = true
	}
	if len(selected) != len(required) {
		return b, apierr.Invalid("OFFER_ACCEPTED_MISSING", "请为每个产品规格选择客户最终接受的方案")
	}
	b.Lines = lines
	b.Total = total.StringFixed(2)
	b.Currency = "USD"
	b.Incoterm = "CFR"
	// Freight is already part of the selected CFR price, not an extra charge.
	b.Transports = nil
	return b, nil
}
