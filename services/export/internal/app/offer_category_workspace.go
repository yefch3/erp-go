package app

import (
	"encoding/json"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
	"strings"
	"time"
)

func prepareCategoryWorkspace(b OfferBody, source OfferInquiry, calculate bool, target string) (OfferBody, error) {
	if strings.TrimSpace(b.Customer) == "" {
		return b, apierr.Invalid("OFFER_HEADER", "请填写客户")
	}
	for _, date := range []string{b.ValidUntil} {
		if err := validBusinessDate(date, "OFFER_DATE", "报价日期"); err != nil {
			return b, err
		}
	}
	valid := map[string]string{"FOB_USD": "USD", "FOB_CNY": "CNY", "ALL_IN_PORT_CNY": "CNY", "EX_FACTORY_CNY": "CNY", "REPROCESSING_CNY": "CNY"}
	if strings.TrimSpace(b.DocumentLanguage) == "" {
		b.DocumentLanguage = "ZH"
	}
	if !map[string]bool{"ZH": true, "EN": true, "ES": true}[b.DocumentLanguage] {
		return b, apierr.Invalid("OFFER_DOCUMENT_LANGUAGE", "报价单语种无效")
	}
	for category, calculation := range b.CategoryCalculations {
		if valid[category] == "" {
			return b, apierr.Invalid("OFFER_CATEGORY_CALCULATION", "报价分类核算设置无效")
		}
		if strings.TrimSpace(calculation.QuoteFX) != "" {
			fx, err := decimal.NewFromString(strings.TrimSpace(calculation.QuoteFX))
			if err != nil || !fx.IsPositive() {
				return b, apierr.Invalid("OFFER_CATEGORY_FX", "分类参考汇率必须大于零")
			}
		}
		for _, input := range []struct{ value, code, label string }{{calculation.PortCharge, "OFFER_CATEGORY_PORT_CHARGE", "港区港杂费"}, {calculation.Loss, "OFFER_CATEGORY_LOSS", "损耗"}} {
			if strings.TrimSpace(input.value) == "" {
				continue
			}
			value, err := decimal.NewFromString(strings.TrimSpace(input.value))
			if err != nil || value.IsNegative() {
				return b, apierr.Invalid(input.code, input.label+"必须是有效的非负数字")
			}
		}
		if len([]rune(calculation.Note)) > 500 {
			return b, apierr.Invalid("OFFER_CATEGORY_NOTE", "分类核算说明不能超过 500 个字")
		}
	}
	products := map[string]bool{}
	for _, p := range source.Body.Products {
		products[p.ID] = true
	}
	seen := map[string]bool{}
	for _, selection := range b.CategorySelections {
		key := selection.ProductID + ":" + selection.QuoteID
		if !products[selection.ProductID] || valid[selection.Category] == "" || seen[key] {
			return b, apierr.Invalid("OFFER_CATEGORY_SELECTION", "产品分类报价选择无效")
		}
		seen[key] = true
		found := false
		for _, q := range source.Quotes {
			if q.ID != selection.QuoteID || q.Kind != "PROCUREMENT" || q.SubmittedAt == "" || q.Historical {
				continue
			}
			var data struct {
				QuoteCategory string `json:"quoteCategory"`
				Currency      string `json:"currency"`
				Prices        []struct {
					ProductID    string `json:"productId"`
					Price        string `json:"price"`
					FactoryPrice string `json:"factoryPrice"`
					FOBPrice     string `json:"fobPrice"`
				} `json:"prices"`
			}
			if err := json.Unmarshal(q.Body, &data); err != nil {
				return b, err
			}
			if data.QuoteCategory == selection.Category && data.Currency == valid[selection.Category] {
				for _, price := range data.Prices {
					if price.ProductID == selection.ProductID {
						found = true
					}
				}
			}
		}
		if !found {
			return b, apierr.Invalid("OFFER_CATEGORY_SELECTION", "所选供应商报价已失效或分类已变化，请重新选择")
		}
	}
	if calculate {
		var err error
		b, err = calculateCategorySelections(b, source, target)
		if err != nil {
			return b, err
		}
	}
	seen = map[string]bool{}
	for _, transport := range b.Transports {
		found := false
		for _, q := range source.Quotes {
			if q.ID == transport.QuoteID && q.Kind == "LOGISTICS" && q.SubmittedAt != "" && !q.Historical {
				found = true
			}
		}
		if !found || seen[transport.QuoteID] {
			return b, apierr.Invalid("OFFER_LOGISTICS_SOURCE", "所选物流报价已失效，请重新选择")
		}
		seen[transport.QuoteID] = true
	}
	staged := map[string]bool{}
	for _, selection := range b.CustomerSelections {
		staged[selection.ProductID+":"+selection.Category+":"+selection.QuoteID] = true
	}
	negotiationStatuses := map[string]bool{"DRAFT": true, "QUOTED": true, "CUSTOMER_COUNTERED": true, "ADJUSTING": true, "AGREED": true}
	seen = map[string]bool{}
	for _, negotiation := range b.Negotiations {
		key := negotiation.ProductID + ":" + negotiation.Category + ":" + negotiation.QuoteID
		if !staged[key] || seen[key] || !negotiationStatuses[negotiation.Status] {
			return b, apierr.Invalid("OFFER_NEGOTIATION", "客户议价记录与已选报价不一致")
		}
		seen[key] = true
		for _, value := range []string{negotiation.InitialPrice, negotiation.CustomerCounterPrice, negotiation.ProposedPrice} {
			if strings.TrimSpace(value) == "" {
				continue
			}
			price, err := decimal.NewFromString(strings.TrimSpace(value))
			if err != nil || price.IsNegative() {
				return b, apierr.Invalid("OFFER_NEGOTIATION_PRICE", "议价金额必须是有效的非负数字")
			}
		}
		if len([]rune(negotiation.Note)) > 500 {
			return b, apierr.Invalid("OFFER_NEGOTIATION_NOTE", "议价说明不能超过 500 个字")
		}
	}
	// The category workflow has no approved formulas yet. Never retain legacy computed prices.
	b.PricingSnapshot = ""
	b.Total = ""
	b.LogisticsQuoteID = ""
	for i := range b.Lines {
		b.Lines[i].CalculatedPrice = ""
		b.Lines[i].UnitPrice = ""
		b.Lines[i].Amount = ""
	}
	return b, nil
}

func calculateCategorySelections(b OfferBody, source OfferInquiry, target string) (OfferBody, error) {
	products := map[string]OfferProduct{}
	for _, product := range source.Body.Products {
		products[product.ID] = product
	}
	for i := range b.CategorySelections {
		selection := &b.CategorySelections[i]
		if target != "*" && selection.Category != target {
			continue
		}
		product, ok := products[selection.ProductID]
		if !ok {
			return b, apierr.Invalid("OFFER_CATEGORY_SELECTION", "核算产品已失效，请刷新后重试")
		}
		quantity, err := decimal.NewFromString(strings.TrimSpace(product.Quantity))
		if err != nil || !quantity.IsPositive() {
			return b, apierr.Invalid("OFFER_CFR_QUANTITY", "产品数量必须大于零")
		}
		supplierPrice, factoryPrice, supplierCurrency := "", "", ""
		for _, quote := range source.Quotes {
			if quote.ID != selection.QuoteID || quote.Kind != "PROCUREMENT" || quote.SubmittedAt == "" || quote.Historical {
				continue
			}
			var body struct {
				Currency string `json:"currency"`
				Prices   []struct {
					ProductID    string `json:"productId"`
					Price        string `json:"price"`
					FactoryPrice string `json:"factoryPrice"`
					FOBPrice     string `json:"fobPrice"`
				} `json:"prices"`
			}
			if err := json.Unmarshal(quote.Body, &body); err != nil {
				return b, err
			}
			supplierCurrency = strings.ToUpper(strings.TrimSpace(body.Currency))
			for _, price := range body.Prices {
				if price.ProductID != selection.ProductID {
					continue
				}
				supplierPrice = strings.TrimSpace(price.Price)
				if supplierPrice == "" {
					supplierPrice = strings.TrimSpace(price.FOBPrice)
				}
				if supplierPrice == "" {
					supplierPrice = strings.TrimSpace(price.FactoryPrice)
				}
				factoryPrice = strings.TrimSpace(price.FactoryPrice)
			}
		}
		unitPrice, err := decimal.NewFromString(supplierPrice)
		if err != nil || unitPrice.IsNegative() {
			return b, apierr.Invalid("OFFER_CFR_SUPPLIER_PRICE", "供应商报价单价无效")
		}
		expectedCurrency := "CNY"
		if selection.Category == "FOB_USD" {
			expectedCurrency = "USD"
		}
		if supplierCurrency != expectedCurrency {
			return b, apierr.Invalid("OFFER_CFR_CURRENCY", "供应商报价币种与报价分类不一致")
		}

		freightTotal, inlandTotal := decimal.Zero, decimal.Zero
		freightQuoteID, latestSubmitted := "", ""
		var latestVersion int64
		for _, quote := range source.Quotes {
			if quote.Kind != "LOGISTICS" || quote.SubmittedAt == "" || quote.Historical {
				continue
			}
			var body struct {
				FreightRates []struct {
					ProductID string `json:"productId"`
					USDTotal  string `json:"usdTotal"`
				} `json:"freightRates"`
				Charges []struct {
					Name           string `json:"name"`
					ProductID      string `json:"productId"`
					AllocationType string `json:"allocationType"`
					USDSubtotal    string `json:"usdSubtotal"`
				} `json:"charges"`
			}
			if err := json.Unmarshal(quote.Body, &body); err != nil {
				return b, err
			}
			for _, rate := range body.FreightRates {
				if rate.ProductID != selection.ProductID || strings.TrimSpace(rate.USDTotal) == "" {
					continue
				}
				value, err := decimal.NewFromString(strings.TrimSpace(rate.USDTotal))
				if err != nil || value.IsNegative() {
					continue
				}
				if freightQuoteID == "" || quote.SubmittedAt > latestSubmitted || (quote.SubmittedAt == latestSubmitted && quote.Version > latestVersion) {
					freightTotal, inlandTotal, freightQuoteID, latestSubmitted, latestVersion = value, inlandFreightForProduct(body.Charges, selection.ProductID, len(body.FreightRates)), quote.ID, quote.SubmittedAt, quote.Version
				}
			}
		}
		if freightQuoteID == "" {
			return b, apierr.Invalid("OFFER_CFR_FREIGHT", "所选产品尚无有效的产品海运总价")
		}
		calculation := b.CategoryCalculations[selection.Category]
		fx := decimal.NewFromInt(1)
		if selection.Category != "FOB_USD" {
			fx, err = decimal.NewFromString(strings.TrimSpace(calculation.QuoteFX))
			if err != nil || !fx.IsPositive() {
				return b, apierr.Invalid("OFFER_CATEGORY_FX", "请填写有效汇率（1 USD 可兑换多少 CNY）")
			}
		}
		portCharge, err := categoryInput(calculation.PortCharge, selection.Category == "ALL_IN_PORT_CNY" || selection.Category == "EX_FACTORY_CNY" || selection.Category == "REPROCESSING_CNY", "请填写港区港杂费")
		if err != nil {
			return b, err
		}
		loss, err := categoryInput(calculation.Loss, selection.Category == "REPROCESSING_CNY", "请填写损耗")
		if err != nil {
			return b, err
		}
		cfrTotal := decimal.Zero
		switch selection.Category {
		case "FOB_USD":
			cfrTotal = unitPrice.Mul(quantity).Add(freightTotal)
		case "FOB_CNY":
			cfrTotal = unitPrice.Mul(quantity).Div(fx).Add(freightTotal)
		case "ALL_IN_PORT_CNY":
			cfrTotal = unitPrice.Mul(quantity).Add(portCharge).Div(fx).Add(freightTotal)
		case "EX_FACTORY_CNY":
			cfrTotal = unitPrice.Mul(quantity).Add(portCharge).Div(fx).Add(freightTotal).Add(inlandTotal)
		case "REPROCESSING_CNY":
			factory, parseErr := decimal.NewFromString(factoryPrice)
			if parseErr != nil || factory.IsNegative() {
				return b, apierr.Invalid("OFFER_CFR_FACTORY_PRICE", "再加工报价缺少有效的出厂单价")
			}
			cfrTotal = factory.Mul(quantity).Add(unitPrice).Add(loss).Add(portCharge).Div(fx).Add(freightTotal).Add(inlandTotal)
		default:
			continue
		}
		selection.SupplierFOBUnitPrice = unitPrice.StringFixed(4)
		selection.ProductFreightTotal = freightTotal.StringFixed(2)
		selection.InlandFreightTotal = inlandTotal.StringFixed(2)
		selection.CFRTotal = cfrTotal.StringFixed(2)
		selection.CFRUnitPrice = cfrTotal.Div(quantity).StringFixed(4)
		selection.FreightQuoteID = freightQuoteID
	}
	return b, nil
}

func categoryInput(raw string, required bool, message string) (decimal.Decimal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return decimal.Zero, apierr.Invalid("OFFER_CATEGORY_INPUT", message)
		}
		return decimal.Zero, nil
	}
	value, err := decimal.NewFromString(raw)
	if err != nil || value.IsNegative() {
		return decimal.Zero, apierr.Invalid("OFFER_CATEGORY_INPUT", message)
	}
	return value, nil
}

func inlandFreightForProduct(charges []struct {
	Name           string `json:"name"`
	ProductID      string `json:"productId"`
	AllocationType string `json:"allocationType"`
	USDSubtotal    string `json:"usdSubtotal"`
}, productID string, freightRateCount int) decimal.Decimal {
	total := decimal.Zero
	for _, charge := range charges {
		name := strings.ToLower(strings.TrimSpace(charge.Name))
		if !strings.Contains(name, "内陆") && !strings.Contains(name, "短驳") && !strings.Contains(name, "inland") && !strings.Contains(name, "drayage") {
			continue
		}
		if charge.ProductID != productID && !(charge.ProductID == "" && freightRateCount == 1) {
			continue
		}
		value, err := decimal.NewFromString(strings.TrimSpace(charge.USDSubtotal))
		if err == nil && !value.IsNegative() {
			total = total.Add(value)
		}
	}
	return total
}

func stageOfferSelections(b *OfferBody, source OfferInquiry) {
	existingNegotiations := map[string]OfferNegotiation{}
	for _, negotiation := range b.Negotiations {
		key := negotiation.ProductID + ":" + negotiation.Category + ":" + negotiation.QuoteID
		existingNegotiations[key] = negotiation
	}
	b.CustomerSelections = []OfferSelectionSnapshot{}
	b.CustomerLogistics = []OfferSourceQuote{}
	b.Negotiations = []OfferNegotiation{}
	for _, selection := range b.CategorySelections {
		for _, product := range source.Body.Products {
			if product.ID != selection.ProductID {
				continue
			}
			for _, quote := range source.Quotes {
				if quote.ID == selection.QuoteID {
					b.CustomerSelections = append(b.CustomerSelections, OfferSelectionSnapshot{OfferCategorySelection: selection, Product: product, Quote: quote})
					key := selection.ProductID + ":" + selection.Category + ":" + selection.QuoteID
					negotiation, ok := existingNegotiations[key]
					if !ok {
						negotiation = OfferNegotiation{OfferCategorySelection: selection, Status: "DRAFT"}
					}
					negotiation.InitialPrice = selection.CFRUnitPrice
					b.Negotiations = append(b.Negotiations, negotiation)
				}
			}
		}
	}
	freightIDs := map[string]bool{}
	for _, selection := range b.CategorySelections {
		if selection.FreightQuoteID != "" {
			freightIDs[selection.FreightQuoteID] = true
		}
	}
	for _, transport := range b.Transports {
		freightIDs[transport.QuoteID] = true
	}
	for quoteID := range freightIDs {
		for _, q := range source.Quotes {
			if q.ID == quoteID {
				b.CustomerLogistics = append(b.CustomerLogistics, q)
			}
		}
	}
	b.SelectionSavedAt = time.Now().UTC().Format(time.RFC3339Nano)
}
