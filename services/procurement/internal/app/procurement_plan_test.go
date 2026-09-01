package app

import (
	"strings"
	"testing"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func TestInvalidProcurementPlanCandidateIdentifiesQuote(t *testing.T) {
	candidate := store.ProcurementPlanCandidateRow{
		QuoteLineID: 77, SupplierName: "供应商甲", BuyerName: "采购乙",
		QuoteVersionNo: 3, ValidUntil: "2026-08-31",
	}
	err := invalidProcurementPlanCandidate(candidate, "SC_PLAN_QUOTE_EXPIRED", "已于 2026-08-31 过期，请选择有效的新版本")

	if apierr.CodeFromError(err) != "SC_PLAN_QUOTE_EXPIRED" {
		t.Fatalf("code = %q", apierr.CodeFromError(err))
	}
	if !strings.Contains(err.Error(), "供应商甲 · 采购乙 · V3") || !strings.Contains(err.Error(), "2026-08-31") {
		t.Fatalf("error does not identify the quote: %v", err)
	}
}
