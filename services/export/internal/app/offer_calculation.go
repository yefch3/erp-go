package app

import (
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/shopspring/decimal"
	"regexp"
	"strings"
)

// OfferCalculation costs are per selling unit, not whole-shipment totals.
// MTPerUnit makes the USD/MT interest term explicit for PCS and other units.
// EffectiveCNYPerUSD is supplied by the service, never trusted from the client.
type OfferCalculation struct {
	Formula   int    `json:"formula"`
	Factory   string `json:"factory"`
	Slitting  string `json:"slitting"`
	ShortHaul string `json:"shortHaul"`
	Inland    string `json:"inland"`
	Port      string `json:"port"`
	Ocean     string `json:"ocean"`
	Days      string `json:"days"`
	MTPerUnit string `json:"mtPerUnit"`
}

var offerDecimal = regexp.MustCompile(`^[0-9]{1,14}(\.[0-9]{1,8})?$`)

func calculateOfferRaw(in OfferCalculation, effectiveCNYPerUSD string) (decimal.Decimal, error) {
	invalid := func(field string) error {
		return apierr.Invalid("OFFER_CALCULATION_INPUT", "请填写有效的非负计算费用和天数").WithMeta("field", field)
	}
	parse := func(value, field string) (decimal.Decimal, error) {
		if strings.TrimSpace(value) == "" {
			return decimal.Zero, nil
		}
		v, err := decimal.NewFromString(value)
		if err != nil || v.IsNegative() || !offerDecimal.MatchString(value) {
			return decimal.Zero, invalid(field)
		}
		return v, nil
	}
	if in.Formula < 1 || in.Formula > 5 {
		return decimal.Zero, apierr.Invalid("OFFER_FORMULA", "请选择正式报价公式")
	}
	factory, err := parse(in.Factory, "factory")
	if err != nil {
		return decimal.Zero, err
	}
	days, err := parse(in.Days, "days")
	if err != nil {
		return decimal.Zero, err
	}
	mt, err := decimal.NewFromString(in.MTPerUnit)
	if err != nil || !mt.IsPositive() || !offerDecimal.MatchString(in.MTPerUnit) {
		return decimal.Zero, apierr.Invalid("OFFER_UNIT_WEIGHT", "请填写每个销售单位对应的吨数，吨单位填写 1")
	}
	base := factory
	if in.Formula >= 3 {
		for _, f := range []struct{ name, value string }{{"inland", in.Inland}, {"port", in.Port}} {
			v, e := parse(f.value, f.name)
			if e != nil {
				return decimal.Zero, e
			}
			base = base.Add(v)
		}
	}
	if in.Formula >= 4 {
		for _, f := range []struct{ name, value string }{{"slitting", in.Slitting}, {"shortHaul", in.ShortHaul}} {
			v, e := parse(f.value, f.name)
			if e != nil {
				return decimal.Zero, e
			}
			base = base.Add(v)
		}
	}
	if in.Formula != 2 {
		rate, e := decimal.NewFromString(effectiveCNYPerUSD)
		if e != nil || !rate.GreaterThan(decimal.RequireFromString("0.05")) {
			return decimal.Zero, apierr.Invalid("OFFER_EFFECTIVE_FX", "需要已确认且大于 0.05 的 USD/CNY 有效汇率")
		}
		base = base.DivRound(rate.Sub(decimal.RequireFromString("0.05")), 24)
	}
	if in.Formula != 5 {
		ocean, e := parse(in.Ocean, "ocean")
		if e != nil {
			return decimal.Zero, e
		}
		base = base.Add(ocean)
	}
	interest := decimal.NewFromInt(5).Mul(mt).Mul(days).DivRound(decimal.NewFromInt(30), 24)
	return base.Add(interest), nil
}

func calculateOfferPrice(in OfferCalculation, effectiveCNYPerUSD string) (string, error) {
	value, err := calculateOfferRaw(in, effectiveCNYPerUSD)
	if err != nil {
		return "", err
	}
	return value.StringFixed(2), nil
}
func offerLineAmount(quantity, price string) (string, error) {
	q, qe := decimal.NewFromString(quantity)
	p, pe := decimal.NewFromString(price)
	if qe != nil || pe != nil || !q.IsPositive() || p.IsNegative() || !offerDecimal.MatchString(quantity) || !offerDecimal.MatchString(price) {
		return "", apierr.Invalid("OFFER_PRICE_INPUT", "请填写正数数量和非负销售单价")
	}
	return q.Mul(p.Round(2)).StringFixed(2), nil
}
