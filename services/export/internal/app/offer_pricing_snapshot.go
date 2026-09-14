package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/sgao19/erp-go/pkg/apierr"
)

// Negotiated prices are excluded so sales may adjust the final customer price.
func offerPricingSnapshot(b OfferBody, source OfferInquiry) string {
	type input struct {
		ID, Quantity, Unit, Factory string
		Calculation                 OfferCalculation
	}
	lines := make([]input, 0, len(b.Lines))
	selected := map[string]bool{b.LogisticsQuoteID: true}
	for _, l := range b.Lines {
		lines = append(lines, input{l.ID, l.Quantity, l.Unit, l.FactoryQuoteID, l.Calculation})
		selected[l.FactoryQuoteID] = true
	}
	quotes := make([]OfferSourceQuote, 0)
	for _, q := range source.Quotes {
		if selected[q.ID] {
			quotes = append(quotes, q)
		}
	}
	accepted := ""
	if b.Incoterm == "CFR" {
		for _, t := range b.Transports {
			if t.Accepted {
				accepted += t.QuoteID + ";"
			}
		}
	}
	raw, _ := json.Marshal(struct {
		Currency, FX, Term, Logistics, Accepted string
		Confirmed                               bool
		Lines                                   []input
		Quotes                                  []OfferSourceQuote
		Products                                []OfferProduct
	}{b.Currency, b.QuoteFX, b.Incoterm, b.LogisticsQuoteID, accepted, b.QuoteFXConfirmed, lines, quotes, source.Body.Products})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func offerPricingStale(b OfferBody, source OfferInquiry) bool {
	for _, l := range b.Lines {
		if l.CalculatedPrice != "" && (b.LogisticsQuoteID != "" || b.PricingSnapshot != "" || b.Incoterm == "CFR" || b.Incoterm == "FOB") {
			return b.PricingSnapshot == "" || b.PricingSnapshot != offerPricingSnapshot(b, source)
		}
	}
	return false
}
func validateOfferPricing(b OfferBody, source OfferInquiry) error {
	if offerPricingStale(b, source) {
		return apierr.Invalid("OFFER_PRICING_STALE", "核价依据已变化或旧报价尚未核验，请重新计算后再保存、导出或确认成交")
	}
	return nil
}
