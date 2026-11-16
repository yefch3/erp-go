package app

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/export/internal/store"
	"github.com/shopspring/decimal"
)

type DeliveryQualityLine struct {
	ContractItemID int64
	Qty            string
}

type DeliveryQualityGuard interface {
	Check(context.Context, int64, []DeliveryQualityLine) error
}

func (s *Service) UseDeliveryQualityGuard(g DeliveryQualityGuard) { s.deliveryQuality = g }

func (s *Service) checkDeliveryQuality(ctx context.Context, tx pgx.Tx, tenantID, contractID int64, items []store.ItemsOfShipmentRow) error {
	if s.deliveryQuality == nil {
		return apierr.Conflict("QUALITY_CHECK_UNAVAILABLE", "质检核验暂不可用，不能确认交货，请稍后重试")
	}
	// Serialize different sailing documents for the same contract while reading
	// already shipped quantities, so two batches cannot reuse one inspection.
	var lockedID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM contracts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, contractID).Scan(&lockedID); err != nil {
		return err
	}
	var unknownHistory bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM contract_shipments cs LEFT JOIN contract_items ci ON ci.tenant_id=cs.tenant_id AND ci.id=cs.contract_item_id WHERE cs.tenant_id=$1 AND cs.contract_id=$2 AND ci.id IS NULL)`, tenantID, contractID).Scan(&unknownHistory); err != nil {
		return err
	}
	if unknownHistory {
		return apierr.Conflict("QUALITY_DELIVERY_HISTORY_REQUIRED", "历史已发运记录缺少商品快照，无法核验累计交货数量，请先核对历史货物关联")
	}
	var lines []DeliveryQualityLine
	for _, item := range items {
		if item.ContractID != contractID {
			continue
		}
		if item.ContractItemID <= 0 {
			return apierr.Conflict("QUALITY_DELIVERY_SOURCE_REQUIRED", "出运货物缺少可核验的合同明细，不能交货")
		}
		var versionID, productID, skuID int64
		var productCode, productName, spec, uom string
		if err := tx.QueryRow(ctx, `SELECT contract_version_id,product_id,coalesce(sku_id,0),product_code,product_name,spec,uom_code FROM contract_items WHERE tenant_id=$1 AND id=$2`, tenantID, item.ContractItemID).Scan(&versionID, &productID, &skuID, &productCode, &productName, &spec, &uom); err != nil {
			return err
		}
		var shipped decimal.Decimal
		var remapped bool
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(cs.qty),0),coalesce(bool_or(cs.contract_item_id<>$3),false)
		 FROM contract_shipments cs LEFT JOIN contract_items old ON old.tenant_id=cs.tenant_id AND old.id=cs.contract_item_id
		 WHERE cs.tenant_id=$1 AND cs.contract_id=$2 AND (cs.contract_item_id=$3 OR
		 (old.contract_version_id<>$4 AND old.product_id=$5 AND coalesce(old.sku_id,0)=$6 AND old.product_code=$7
		  AND old.product_name=$8 AND old.spec=$9 AND old.uom_code=$10))`, tenantID, contractID, item.ContractItemID, versionID, productID, skuID, productCode, productName, spec, uom).Scan(&shipped, &remapped); err != nil {
			return err
		}
		if remapped {
			var matching int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM contract_items WHERE tenant_id=$1 AND contract_version_id=$2
			 AND product_id=$3 AND coalesce(sku_id,0)=$4 AND product_code=$5 AND product_name=$6 AND spec=$7 AND uom_code=$8`, tenantID, versionID, productID, skuID, productCode, productName, spec, uom).Scan(&matching); err != nil {
				return err
			}
			if matching != 1 {
				return apierr.Conflict("QUALITY_VERSION_ALLOCATION_REQUIRED", "合同变更后存在相同规格的多条明细，无法确认历史已发量归属，请先核对货物关联")
			}
		}
		qty, err := decimal.NewFromString(item.Qty)
		if err != nil || !qty.IsPositive() {
			return apierr.Invalid("EX_SHIPMENT_QTY_INVALID", "出运数量必须大于 0")
		}
		lines = append(lines, DeliveryQualityLine{ContractItemID: item.ContractItemID, Qty: shipped.Add(qty).String()})
	}
	if len(lines) == 0 {
		return apierr.Conflict("QUALITY_DELIVERY_SOURCE_REQUIRED", "出运单没有可核验的合同货物，不能交货")
	}
	return s.deliveryQuality.Check(ctx, contractID, lines)
}
