package app

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"
)

// Fx snapshots (A4 P6). A document in a foreign currency gets the cross rate
// to the book currency stamped on it at entry — not at read time, because
// the question gain/loss answers is "what was it worth THEN", and THEN only
// exists if somebody wrote it down.
//
// Failure degrades to zero, never blocks: a payment is a fact — the money
// already left the bank — and refusing to record a fact because the fx
// service is napping would push it into a spreadsheet. Zero means "not
// captured"; such rows sit out of gain/loss instead of pretending.

// defaultBaseCurrency is the book currency. The company's books are CNY;
// BASE_CURRENCY in the environment overrides it (compose passes it through —
// an env var the compose file does not carry is silently dead).
const defaultBaseCurrency = "CNY"

// UseBaseCurrency overrides the book currency. Empty keeps the default.
func (s *Service) UseBaseCurrency(currency string) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency != "" {
		s.baseCurrency = currency
	}
}

func (s *Service) bookCurrency() string {
	if s.baseCurrency == "" {
		return defaultBaseCurrency
	}
	return s.baseCurrency
}

// crossRate returns how many units of book currency one unit of `currency`
// buys right now, or zero when it cannot honestly say. The fx service quotes
// everything in units-per-USD, so the cross is target/source — same idiom as
// convertAmount in cost_scenario.go.
func (s *Service) crossRate(ctx context.Context, currency string) decimal.Decimal {
	base := s.bookCurrency()
	if currency == base {
		return decimal.NewFromInt(1)
	}
	if s.rates == nil {
		return decimal.Zero
	}
	src, err := s.rates.Latest(ctx, currency)
	if err != nil || src.Rate.IsZero() {
		return decimal.Zero
	}
	dst, err := s.rates.Latest(ctx, base)
	if err != nil || dst.Rate.IsZero() {
		return decimal.Zero
	}
	return dst.Rate.Div(src.Rate).Round(8)
}

// fxSnapshot prices an amount into the book currency. rate zero → base zero:
// "not captured", never a fabricated conversion.
func (s *Service) fxSnapshot(ctx context.Context, currency string, amount decimal.Decimal) (rate, baseAmount decimal.Decimal) {
	rate = s.crossRate(ctx, currency)
	if rate.IsZero() {
		return decimal.Zero, decimal.Zero
	}
	return rate, amount.Mul(rate).Round(2)
}
