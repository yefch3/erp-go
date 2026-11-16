package app

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type ContractDispatchLine struct {
	ItemID                                                 int64
	ProductCode, ProductName, Specification, UOM, Quantity string
}

// Read-only cross-department projection. The trusted gateway first checks
// contract ownership. Only allocated goods on actually-dispatched voyages count;
// plans and cancellations without dispatch evidence never become shipped goods.
func (s *Service) ContractDispatchProgress(ctx context.Context, tenant, contract int64) ([]ContractDispatchLine, error) {
	if tenant <= 0 || contract <= 0 {
		return nil, apierr.Invalid("SHIPPING_CONTRACT_REQUIRED", "合同关联无效")
	}
	rows, err := s.pool.Query(ctx, `SELECT c.contract_item_id,c.product_code,c.product_name,c.specification,c.uom_code,sum(c.quantity)::text
 FROM shipping_schedules v JOIN contract_shipping_handoffs h ON h.tenant_id=v.tenant_id AND h.id=v.contract_handoff_id AND h.schedule_id=v.id
 JOIN contract_shipping_handoff_cargo c ON c.tenant_id=h.tenant_id AND c.handoff_id=h.id
 LEFT JOIN manual_shipping_orders m ON m.tenant_id=h.tenant_id AND m.handoff_id=h.id
 WHERE v.tenant_id=$1 AND ((h.contract_id=$2 AND v.contract_id=$2 AND m.handoff_id IS NULL AND h.allocation_status='ALLOCATED') OR (m.linked_contract_id=$2 AND h.contract_id=0))
 AND (v.atd IS NOT NULL OR v.status IN ('SAILED','IN_TRANSIT','ARRIVED','COMPLETED')
 OR EXISTS(SELECT 1 FROM shipping_schedule_changes ch WHERE ch.tenant_id=v.tenant_id AND ch.schedule_id=v.id AND ch.field_name='status' AND ch.new_value IN ('SAILED','IN_TRANSIT','ARRIVED')))
 GROUP BY c.contract_item_id,c.product_code,c.product_name,c.specification,c.uom_code ORDER BY c.contract_item_id`, tenant, contract)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContractDispatchLine{}
	for rows.Next() {
		var line ContractDispatchLine
		if err = rows.Scan(&line.ItemID, &line.ProductCode, &line.ProductName, &line.Specification, &line.UOM, &line.Quantity); err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, rows.Err()
}
