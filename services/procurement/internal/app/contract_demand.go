package app

import (
	"github.com/sgao19/erp-go/services/procurement/internal/store"
	"github.com/shopspring/decimal"
)

// Identical products may occupy several customer lines or supplier orders. Their
// combined new commitments must fit the latest sale after all older orders.
func contractGroupConflict(reqs []store.RequirementsForOrderRow, quantities map[int64]decimal.Decimal) string {
	type key struct {
		contract, product, sku int64
		code, name, spec, uom  string
	}
	totals := map[key]decimal.Decimal{}
	budgets := map[key]decimal.Decimal{}
	for _, r := range reqs {
		if r.ContractID <= 0 || r.GroupOpenQty == "" {
			continue
		}
		k := key{r.ContractID, r.ProductID, r.SkuID, r.ProductCode, r.ProductName, r.Spec, r.UomCode}
		budget, err := decimal.NewFromString(r.GroupOpenQty)
		if err != nil {
			return "合同可采购数量无效，请刷新"
		}
		budgets[k] = budget
		totals[k] = totals[k].Add(quantities[r.ID])
	}
	for k, total := range totals {
		if total.GreaterThan(budgets[k]) {
			return "「" + k.name + "」合计采购数量超过最新合同剩余需求；已下单的旧版本采购单仍计入覆盖数量"
		}
	}
	return ""
}
