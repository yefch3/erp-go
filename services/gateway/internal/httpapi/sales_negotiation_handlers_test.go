package httpapi

import (
	"strings"
	"testing"
	"unicode"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func TestFormalQuotationItemTextOmitsInternalSupplierAndFixedChinese(t *testing.T) {
	item := &prv1.CustomerSelectionItem{
		SupplierName:              "内部供应商 A",
		ProductSpec:               "ASTM A36 · 2.00 × 1250",
		FinalCustomerPaymentTerms: "30% deposit, 70% before shipment",
		FinalCustomerIncoterm:     "CFR VALPARAISO",
		FinalCustomerRequiredDate: "2026-11-15",
	}

	spec, remark := formalQuotationItemText(item)
	combined := spec + "\n" + remark
	if strings.Contains(combined, item.SupplierName) {
		t.Fatalf("customer-facing quotation leaked supplier name: %q", combined)
	}
	if strings.IndexFunc(combined, func(r rune) bool { return unicode.Is(unicode.Han, r) }) >= 0 {
		t.Fatalf("customer-facing quotation contains fixed Chinese text: %q", combined)
	}
	for _, want := range []string{"ASTM A36", "30% deposit", "CFR VALPARAISO", "2026-11-15"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("customer-facing quotation omitted %q: %q", want, combined)
		}
	}
}
