// Package money provides exact-decimal amounts with a currency, and the
// FxSnapshot value object shared by every document that freezes a rate.
// float64 must never be used for amounts anywhere in the system.
package money

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// BaseCurrency is the system base currency (business decision: USD).
// Every base_amount column stores amounts in this currency.
const BaseCurrency = "USD"

// Money is an exact decimal amount in a specific currency.
type Money struct {
	Amount   decimal.Decimal
	Currency string
}

func New(amount decimal.Decimal, currency string) (Money, error) {
	if err := validCurrency(currency); err != nil {
		return Money{}, err
	}
	return Money{Amount: amount, Currency: currency}, nil
}

// FromString parses an exact decimal string such as "1234.56".
func FromString(amount, currency string) (Money, error) {
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return Money{}, fmt.Errorf("money: invalid amount %q: %w", amount, err)
	}
	return New(d, currency)
}

func Zero(currency string) Money {
	return Money{Amount: decimal.Zero, Currency: currency}
}

// Add returns m + other. Mixing currencies is a programming error, not a
// rounding concern, so it fails loudly.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("money: cannot add %s to %s", other.Currency, m.Currency)
	}
	return Money{Amount: m.Amount.Add(other.Amount), Currency: m.Currency}, nil
}

func (m Money) Sub(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("money: cannot subtract %s from %s", other.Currency, m.Currency)
	}
	return Money{Amount: m.Amount.Sub(other.Amount), Currency: m.Currency}, nil
}

// MulQty multiplies a unit price by a quantity and rounds to 2 decimals,
// which is the storage scale for amounts (NUMERIC(18,2)).
func (m Money) MulQty(qty decimal.Decimal) Money {
	return Money{Amount: m.Amount.Mul(qty).Round(2), Currency: m.Currency}
}

func (m Money) IsNegative() bool { return m.Amount.IsNegative() }
func (m Money) IsZero() bool     { return m.Amount.IsZero() }

func (m Money) String() string {
	return m.Amount.StringFixed(2) + " " + m.Currency
}

// FxSnapshot is the exchange rate frozen into a document at business time.
// It is embedded by value into the document row (fx_rate / fx_rate_at /
// fx_source columns): documents hold no reference to live rate tables, so
// later rate changes cannot alter historical documents.
type FxSnapshot struct {
	BaseCurrency string
	Rate         decimal.Decimal
	QuotedAt     time.Time
	Source       string
}

func NewFxSnapshot(base string, rate decimal.Decimal, quotedAt time.Time, source string) (FxSnapshot, error) {
	if err := validCurrency(base); err != nil {
		return FxSnapshot{}, err
	}
	if rate.LessThanOrEqual(decimal.Zero) {
		return FxSnapshot{}, fmt.Errorf("money: fx rate must be positive, got %s", rate)
	}
	if source == "" {
		return FxSnapshot{}, fmt.Errorf("money: fx source is required")
	}
	if quotedAt.IsZero() {
		return FxSnapshot{}, fmt.Errorf("money: fx quote time is required")
	}
	return FxSnapshot{BaseCurrency: base, Rate: rate, QuotedAt: quotedAt, Source: source}, nil
}

// ToBase converts an amount into the snapshot's base currency, rounded to
// the storage scale (2 decimals).
func (s FxSnapshot) ToBase(m Money) Money {
	return Money{Amount: m.Amount.Mul(s.Rate).Round(2), Currency: s.BaseCurrency}
}

func validCurrency(c string) error {
	if len(c) != 3 {
		return fmt.Errorf("money: currency must be a 3-letter ISO code, got %q", c)
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return fmt.Errorf("money: currency must be uppercase letters, got %q", c)
		}
	}
	return nil
}
