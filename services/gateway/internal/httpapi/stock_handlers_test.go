package httpapi

import (
	"testing"

	ivv1 "github.com/sgao19/erp-go/gen/go/erp/inventory/v1"
)

func TestRedactStockCostsRemovesEveryValuationField(t *testing.T) {
	stocks := []*ivv1.Stock{{AvgCost: "12.34", TotalCost: "1234", CostCurrency: "CNY"}, nil}
	redactStockCosts(stocks)
	if stocks[0].GetAvgCost() != "" || stocks[0].GetTotalCost() != "" || stocks[0].GetCostCurrency() != "" {
		t.Fatalf("cost fields leaked after redaction: %+v", stocks[0])
	}
}
