package app

import (
	"context"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// Deliberately not an Order: no order IDs, prices, quantities, payment terms or
// write handles leave procurement through this shipping-only projection.
type ShippingPurchaseRemark struct {
	SupplierName, PurchaseOrderRemark string
}

// The trusted gateway resolves contract provenance from a shipping handoff the
// employee can read. There is no HTTP route accepting arbitrary contract IDs.
// Purchase-order scope is intentionally NOT widened: it still fences all full
// order endpoints. Only these two explicitly approved fields cross that fence.
func (s *Service) ListShippingPurchaseRemarks(ctx context.Context, tenantID, contractID int64, contractNo string, op Operator) ([]ShippingPurchaseRemark, error) {
	access, ok := s.scopes.(InquiryAccess)
	if !ok || tenantID <= 0 || op.ID <= 0 {
		return nil, apierr.Permission("SHIPPING_PURCHASE_REMARKS_PERMISSION", "没有查看海运采购备注的权限")
	}
	allowed, err := access.HasPermission(ctx, op.ID, "shipping:schedule:read")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, apierr.Permission("SHIPPING_PURCHASE_REMARKS_PERMISSION", "没有查看海运采购备注的权限")
	}
	contractNo = strings.TrimSpace(contractNo)
	if contractID <= 0 || contractNo == "" {
		return nil, apierr.Invalid("SHIPPING_PURCHASE_REMARKS_CONTRACT", "海运实单尚未关联合同")
	}
	rows, err := s.pool.Query(ctx, `SELECT o.supplier_name,o.remark
		FROM purchase_orders o
		WHERE o.tenant_id=$1 AND o.business_type='PROCUREMENT'
		AND o.status IN ('ORDERED','PARTIALLY_RECEIVED','RECEIVED')
		AND (o.source_business_id=0 OR o.source_business_id=$2)
		AND EXISTS(SELECT 1 FROM purchase_order_items i
		 JOIN purchase_requirements pr ON pr.tenant_id=i.tenant_id AND pr.id=i.requirement_id
		 WHERE i.tenant_id=o.tenant_id AND i.po_id=o.id AND pr.contract_id=$2 AND pr.contract_no=$3)
		ORDER BY o.id`, tenantID, contractID, contractNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShippingPurchaseRemark{}
	for rows.Next() {
		var row ShippingPurchaseRemark
		if err = rows.Scan(&row.SupplierName, &row.PurchaseOrderRemark); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
