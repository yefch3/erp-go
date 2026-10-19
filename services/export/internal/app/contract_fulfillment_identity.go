package app

import (
	"context"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/export/internal/store"
	"github.com/shopspring/decimal"
)

// Versions can change price and unshipped quantity, but must retain the identity
// and quantity of goods already dispatched. This also preserves cross-version progress.
func (s *Service) checkShippedContractLines(ctx context.Context, tenant, id int64, lines []store.ListContractItemsRow) error {
	rows, err := s.pool.Query(ctx, `SELECT ci.product_id,coalesce(ci.sku_id,0),
 CASE WHEN ci.product_id=0 THEN ci.product_name ELSE '' END,
 CASE WHEN ci.product_id=0 THEN ci.uom_code ELSE '' END,ci.spec,sum(cs.qty)::text
 FROM contract_shipments cs JOIN contract_items ci ON ci.tenant_id=cs.tenant_id AND ci.id=cs.contract_item_id
 WHERE cs.tenant_id=$1 AND cs.contract_id=$2
 GROUP BY ci.product_id,coalesce(ci.sku_id,0),CASE WHEN ci.product_id=0 THEN ci.product_name ELSE '' END,
 CASE WHEN ci.product_id=0 THEN ci.uom_code ELSE '' END,ci.spec`, tenant, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var product, sku int64
		var name, unit, spec, shipped string
		if err := rows.Scan(&product, &sku, &name, &unit, &spec, &shipped); err != nil {
			return err
		}
		quantity := decimal.Zero
		for _, line := range lines {
			if line.ProductID == product && normalizedSKU(line.SkuID) == sku && line.Spec == spec && (product != 0 || (line.ProductName == name && line.UomCode == unit)) {
				n, err := decimal.NewFromString(line.Qty)
				if err != nil {
					return err
				}
				quantity = quantity.Add(n)
			}
		}
		sent, err := decimal.NewFromString(shipped)
		if err != nil {
			return err
		}
		if quantity.LessThan(sent) {
			return apierr.Invalid("EX_CHANGE_BELOW_SHIPPED", "已有发运的产品及规格不能删除或替换，合同数量不能小于已发运数量").WithMeta("product", name, "spec", spec, "shipped", shipped)
		}
	}
	return rows.Err()
}
