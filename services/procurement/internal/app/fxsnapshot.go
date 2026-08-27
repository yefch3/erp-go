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
		s.noRate(ctx, currency, base, "取不到"+currency+"的汇率", err)
		return decimal.Zero
	}
	dst, err := s.rates.Latest(ctx, base)
	if err != nil || dst.Rate.IsZero() {
		// 本位币这一次特别值得说：**每一笔外币单据都要它**。CNY 取不到，
		// 那一段时间里所有付款和采购发票的本位币金额会一起变成 0。
		s.noRate(ctx, currency, base, "取不到本位币"+base+"的汇率", err)
		return decimal.Zero
	}
	return dst.Rate.Div(src.Rate).Round(8)
}

// noRate 记下「这一单没采到汇率」。
//
// 降级本身是对的（见文件头）：钱已经走了，不能因为 fx 打盹就拒绝记账，
// 而零表示「没采到」，比编一个汇率诚实。**但沉默不对。**
//
// 原来这里一声不吭：fx 挂一天，那天所有付款和采购发票的 base_amount 都是 0，
// 账上悄悄多出一批洞，要等有人翻汇兑损益报表才发现——那时候已经不知道是哪天、
// 哪些行了。日志里带上币种和本位币，是为了能直接按时间段把那些行捞出来补。
func (s *Service) noRate(ctx context.Context, currency, base, why string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.log.WarnContext(ctx, "没采到汇率，这一单的本位币金额记为 0",
		"currency", currency, "base", base, "why", why, "err", msg,
		"impact", "这一行不参与汇兑损益，直到有人补上汇率并重算")
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
