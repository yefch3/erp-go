package app

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
)

type allocationCharge struct{ Amount, Quantity, Currency, AllocationType, ProductID string }

// allocateOfferLogistics keeps currencies separate. Product-specific charges go
// directly to one line, per-ton charges use contract weight, and shipment-fixed
// charges use each product's weight share. The final line receives the cent tail.
func allocateOfferLogistics(b OfferBody, source OfferInquiry) ([]OfferLogisticsAllocation, error) {
	var accepted *OfferTransport
	for i := range b.Transports {
		if b.Transports[i].Accepted {
			if accepted != nil {
				return nil, apierr.Invalid("OFFER_ONE_SHIPMENT", "一份合同当前只允许选择一个运输方案")
			}
			accepted = &b.Transports[i]
		}
	}
	if accepted == nil && b.LogisticsQuoteID != "" {
		accepted = &OfferTransport{QuoteID: b.LogisticsQuoteID}
	}
	if accepted == nil {
		return []OfferLogisticsAllocation{}, nil
	}
	var charges []allocationCharge
	for _, q := range source.Quotes {
		if q.ID == accepted.QuoteID && q.Kind == "LOGISTICS" {
			var body struct {
				Charges []allocationCharge `json:"charges"`
			}
			if err := json.Unmarshal(q.Body, &body); err != nil {
				return nil, err
			}
			charges = body.Charges
			break
		}
	}
	if len(charges) == 0 {
		return []OfferLogisticsAllocation{}, nil
	}
	weights := make([]decimal.Decimal, len(b.Lines))
	totalWeight := decimal.Zero
	for i, l := range b.Lines {
		qty, e1 := decimal.NewFromString(l.Quantity)
		mt, e2 := decimal.NewFromString(l.Calculation.MTPerUnit)
		if e1 != nil || e2 != nil || !qty.IsPositive() || !mt.IsPositive() {
			return nil, apierr.Invalid("OFFER_ALLOCATION_WEIGHT", "物流费用分摊需要每项产品的销售单位吨数")
		}
		weights[i] = qty.Mul(mt)
		totalWeight = totalWeight.Add(weights[i])
	}
	values := make([]map[string]decimal.Decimal, len(b.Lines))
	for i := range values {
		values[i] = map[string]decimal.Decimal{}
	}
	for _, c := range charges {
		amount, e1 := decimal.NewFromString(c.Amount)
		qty, e2 := decimal.NewFromString(c.Quantity)
		currency := strings.ToUpper(strings.TrimSpace(c.Currency))
		if e1 != nil || e2 != nil || amount.IsNegative() || !qty.IsPositive() || len(currency) != 3 {
			return nil, apierr.Invalid("OFFER_ALLOCATION_CHARGE", "物流费用金额、数量或币种无效")
		}
		kind := strings.ToUpper(c.AllocationType)
		if kind == "" {
			kind = "FIXED"
		}
		switch kind {
		case "DIRECT":
			found := false
			for i, l := range b.Lines {
				if l.ID == c.ProductID {
					values[i][currency] = values[i][currency].Add(amount.Mul(qty).Round(2))
					found = true
					break
				}
			}
			if !found {
				return nil, apierr.Invalid("OFFER_ALLOCATION_PRODUCT", "产品专属物流费用缺少有效产品")
			}
		case "PER_TON":
			for i := range b.Lines {
				values[i][currency] = values[i][currency].Add(amount.Mul(weights[i]).Round(2))
			}
		case "FIXED":
			total := amount.Mul(qty).Round(2)
			used := decimal.Zero
			for i := range b.Lines {
				part := total.Sub(used)
				if i < len(b.Lines)-1 {
					part = total.Mul(weights[i]).Div(totalWeight).Round(2)
				}
				values[i][currency] = values[i][currency].Add(part)
				used = used.Add(part)
			}
		default:
			return nil, apierr.Invalid("OFFER_ALLOCATION_TYPE", "物流费用分摊方式无效")
		}
	}
	out := []OfferLogisticsAllocation{}
	for i, l := range b.Lines {
		currencies := make([]string, 0, len(values[i]))
		for currency := range values[i] {
			currencies = append(currencies, currency)
		}
		sort.Strings(currencies)
		for _, currency := range currencies {
			amount := values[i][currency]
			out = append(out, OfferLogisticsAllocation{ProductID: l.ID, Product: l.Product, Currency: currency, Amount: amount.StringFixed(2)})
		}
	}
	return out, nil
}
