package app

import (
	"encoding/json"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
	"strings"
)

// Read costs from the submitted quote; client-entered calculation costs are not authoritative.
func prepareLogisticsInputs(line *OfferLine, b OfferBody, source OfferInquiry, fx decimal.Decimal) error {
	for _, q := range source.Quotes {
		if q.ID != b.LogisticsQuoteID || q.Kind != "LOGISTICS" {
			continue
		}
		var body struct {
			TransitDays string
			Charges     []struct{ Name, Amount, Currency, Unit string }
		}
		if err := json.Unmarshal(q.Body, &body); err != nil {
			return err
		}
		sums := map[string]decimal.Decimal{}
		for _, c := range body.Charges {
			name := strings.ToLower(c.Name)
			field := ""
			target := "CNY"
			switch {
			case strings.Contains(name, "海运") || strings.Contains(name, "ocean"):
				field = "ocean"
				target = "USD"
			case strings.Contains(name, "内陆") || strings.Contains(name, "inland"):
				field = "inland"
			case strings.Contains(name, "港杂") || strings.Contains(name, "port"):
				field = "port"
			case strings.Contains(name, "短导") || strings.Contains(name, "short"):
				field = "short"
			case strings.Contains(name, "分条") || strings.Contains(name, "slit"):
				field = "slitting"
			default:
				return apierr.Invalid("OFFER_LOGISTICS_COMPONENT", "请物流在费用名称中明确内陆、港杂、海运、短导或分条费用后核价")
			}
			amount, err := decimal.NewFromString(c.Amount)
			if err != nil || amount.IsNegative() {
				return apierr.Invalid("OFFER_LOGISTICS_AMOUNT", "物流费用无效")
			}
			if !strings.EqualFold(c.Unit, line.Unit) {
				if !strings.EqualFold(c.Unit, "MT") {
					return apierr.Invalid("OFFER_LOGISTICS_UNIT", "物流与客户计价单位不同，请先统一单位")
				}
				mt, e := decimal.NewFromString(line.Calculation.MTPerUnit)
				if e != nil || !mt.IsPositive() {
					return apierr.Invalid("OFFER_UNIT_WEIGHT", "请填写客户单位对应吨数")
				}
				amount = amount.Mul(mt)
			}
			currency := strings.ToUpper(c.Currency)
			if currency != target {
				if currency == "CNY" && target == "USD" {
					amount = amount.Div(fx)
				} else if currency == "USD" && target == "CNY" {
					amount = amount.Mul(fx)
				} else {
					return apierr.Invalid("OFFER_LOGISTICS_CURRENCY", "当前公式支持人民币或美元物流报价")
				}
			}
			sums[field] = sums[field].Add(amount)
		}
		line.Calculation.Inland = sums["inland"].Round(8).String()
		line.Calculation.Port = sums["port"].Round(8).String()
		line.Calculation.ShortHaul = sums["short"].Round(8).String()
		line.Calculation.Ocean = sums["ocean"].Round(8).String()
		line.Calculation.Slitting = sums["slitting"].Round(8).String()
		line.Calculation.Days = body.TransitDays
		return nil
	}
	return apierr.Invalid("OFFER_LOGISTICS_SOURCE", "物流报价不存在，请刷新上游报价")
}
