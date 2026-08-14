package app

import (
	"testing"

	"github.com/shopspring/decimal"
)

func dec(value string) decimal.Decimal { return decimal.RequireFromString(value) }

func TestConvertAmountUsesUnitsPerUSDRates(t *testing.T) {
	got := convertAmount(dec("100"), dec("0.8"), dec("7.2"))
	if !got.Equal(dec("900")) {
		t.Fatalf("converted amount = %s, want 900", got)
	}
}

func TestAllocateChargesByTonnageKeepsRoundedTotal(t *testing.T) {
	lines := []costLine{{qty: dec("10")}, {qty: dec("40")}, {qty: dec("50")}}
	allocateCharges(lines, dec("1000"), "TONS", dec("100"), decimal.Zero)
	want := []decimal.Decimal{dec("100"), dec("400"), dec("500")}
	total := decimal.Zero
	for i := range lines {
		if !lines[i].allocated.Equal(want[i]) {
			t.Fatalf("line %d allocation = %s, want %s", i, lines[i].allocated, want[i])
		}
		total = total.Add(lines[i].allocated)
	}
	if !total.Equal(dec("1000")) {
		t.Fatalf("allocated total = %s, want 1000", total)
	}
}

func TestAllocateChargesAssignsRoundingResidualToLastLine(t *testing.T) {
	lines := []costLine{{productCost: dec("1")}, {productCost: dec("1")}, {productCost: dec("1")}}
	allocateCharges(lines, dec("1"), "PRODUCT_AMOUNT", decimal.Zero, dec("3"))
	want := []decimal.Decimal{dec("0.33"), dec("0.33"), dec("0.34")}
	for i := range lines {
		if !lines[i].allocated.Equal(want[i]) {
			t.Fatalf("line %d allocation = %s, want %s", i, lines[i].allocated, want[i])
		}
	}
}
