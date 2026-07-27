package money

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestFromStringRejectsGarbage(t *testing.T) {
	if _, err := FromString("12.34.56", "USD"); err == nil {
		t.Fatal("expected error for malformed amount")
	}
	if _, err := FromString("100", "usd"); err == nil {
		t.Fatal("expected error for lowercase currency")
	}
	if _, err := FromString("100", "US"); err == nil {
		t.Fatal("expected error for 2-letter currency")
	}
}

func TestAddRejectsCurrencyMismatch(t *testing.T) {
	usd, _ := FromString("10.00", "USD")
	cny, _ := FromString("10.00", "CNY")
	if _, err := usd.Add(cny); err == nil {
		t.Fatal("expected error when adding CNY to USD")
	}
}

func TestMulQtyRoundsToStorageScale(t *testing.T) {
	price, _ := FromString("0.333", "USD")
	got := price.MulQty(decimal.NewFromInt(3))
	if got.Amount.String() != "1" {
		t.Fatalf("0.333 * 3 rounded = %s, want 1", got.Amount)
	}
	// Exactness where float64 would fail: 0.1 + 0.2.
	a, _ := FromString("0.1", "USD")
	b, _ := FromString("0.2", "USD")
	sum, _ := a.Add(b)
	if sum.Amount.String() != "0.3" {
		t.Fatalf("0.1 + 0.2 = %s, want 0.3", sum.Amount)
	}
}

func TestFxSnapshotToBase(t *testing.T) {
	rate, _ := decimal.NewFromString("7.2435")
	snap, err := NewFxSnapshot("USD", rate, time.Now(), "MANUAL")
	if err != nil {
		t.Fatal(err)
	}
	cny, _ := FromString("100.00", "CNY")
	got := snap.ToBase(cny)
	if got.Currency != "USD" {
		t.Fatalf("base currency = %s, want USD", got.Currency)
	}
	if got.Amount.String() != "724.35" {
		t.Fatalf("converted = %s, want 724.35", got.Amount)
	}
}

func TestFxSnapshotRejectsInvalid(t *testing.T) {
	if _, err := NewFxSnapshot("USD", decimal.Zero, time.Now(), "MANUAL"); err == nil {
		t.Fatal("expected error for zero rate")
	}
	if _, err := NewFxSnapshot("USD", decimal.NewFromInt(7), time.Now(), ""); err == nil {
		t.Fatal("expected error for empty source")
	}
	if _, err := NewFxSnapshot("USD", decimal.NewFromInt(7), time.Time{}, "MANUAL"); err == nil {
		t.Fatal("expected error for zero quote time")
	}
}
