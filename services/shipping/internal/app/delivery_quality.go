package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

// DeliveryQualityLine includes previous shipments of this contract line. A
// passed quantity cannot be reused to authorize several separate batches.
type DeliveryQualityLine struct {
	ContractItemID int64
	Qty            string
}

type DeliveryQualityGuard interface {
	Check(context.Context, int64, []DeliveryQualityLine) error
}

func (s *Service) UseDeliveryQualityGuard(g DeliveryQualityGuard) { s.deliveryQuality = g }

func recordedDispatch(ctx context.Context, tx pgx.Tx, current store.ShippingSchedule) (bool, error) {
	if current.Atd.Valid || current.Status == "SAILED" || current.Status == "IN_TRANSIT" || current.Status == "ARRIVED" || current.Status == "COMPLETED" {
		return true, nil
	}
	var dispatched bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shipping_schedule_changes WHERE tenant_id=$1 AND schedule_id=$2 AND field_name='status' AND new_value IN ('SAILED','IN_TRANSIT','ARRIVED'))`, current.TenantID, current.ID).Scan(&dispatched)
	return dispatched, err
}

// Automatic lifecycle updates must obey the same first-dispatch gate as
// explicit status commands, even when the initiating action only edits a plan.
func (s *Service) checkDerivedDeliveryQuality(ctx context.Context, tx pgx.Tx, current store.ShippingSchedule, next string) error {
	if next != "SAILED" && next != "IN_TRANSIT" && next != "ARRIVED" {
		return nil
	}
	dispatched, err := recordedDispatch(ctx, tx, current)
	if err != nil {
		return err
	}
	if dispatched {
		return nil
	}
	return s.checkDeliveryQuality(ctx, tx, current)
}

// Planning a voyage is allowed before inspection. This guard belongs only at
// commands that record goods as actually dispatched, never at schedule creation.
func (s *Service) checkDeliveryQuality(ctx context.Context, tx pgx.Tx, current store.ShippingSchedule) error {
	if s.deliveryQuality == nil {
		return apierr.Conflict("QUALITY_CHECK_UNAVAILABLE", "质检核验暂不可用，不能确认交货，请稍后重试")
	}
	if current.ContractID == nil || *current.ContractID <= 0 || current.ContractHandoffID == nil {
		return apierr.Conflict("QUALITY_DELIVERY_SOURCE_REQUIRED", "本批货物缺少可核验的外销合同明细，请先补齐货物关联并完成质检")
	}
	// Share the handoff update lock, so refreshed cargo and concurrent departures
	// cannot authorize a different quantity from the one committed here.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("shipping-contract:%d:%d", current.TenantID, *current.ContractID)); err != nil {
		return err
	}
	var linked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM contract_shipping_handoffs h WHERE h.tenant_id=$1 AND h.id=$2 AND h.contract_id=$3 AND h.allocation_status='ALLOCATED'
		AND NOT EXISTS(SELECT 1 FROM contract_shipping_handoff_cargo c WHERE c.tenant_id=h.tenant_id AND c.handoff_id=h.id AND c.contract_item_id<=0))`, current.TenantID, *current.ContractHandoffID, *current.ContractID).Scan(&linked); err != nil {
		return err
	}
	if !linked {
		return apierr.Conflict("QUALITY_DELIVERY_SOURCE_REQUIRED", "本批货物尚未完成分配，不能核验质检并确认交货")
	}
	var unknownHistory bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shipping_schedules v WHERE v.tenant_id=$1 AND v.contract_id=$2 AND v.id<>$3
	 AND (v.atd IS NOT NULL OR v.status IN ('SAILED','IN_TRANSIT','ARRIVED','COMPLETED')
	  OR EXISTS(SELECT 1 FROM shipping_schedule_changes ch WHERE ch.tenant_id=v.tenant_id AND ch.schedule_id=v.id AND ch.field_name='status' AND ch.new_value IN ('SAILED','IN_TRANSIT','ARRIVED')))
	 AND (v.contract_handoff_id IS NULL OR NOT EXISTS(SELECT 1 FROM contract_shipping_handoff_cargo c WHERE c.tenant_id=v.tenant_id AND c.handoff_id=v.contract_handoff_id)
	  OR EXISTS(SELECT 1 FROM contract_shipping_handoff_cargo c WHERE c.tenant_id=v.tenant_id AND c.handoff_id=v.contract_handoff_id AND c.contract_item_id<=0)))`, current.TenantID, *current.ContractID, current.ID).Scan(&unknownHistory); err != nil {
		return err
	}
	if unknownHistory {
		return apierr.Conflict("QUALITY_DELIVERY_HISTORY_REQUIRED", "历史已发运记录缺少货物明细，无法核验累计交货数量，请先补齐货物关联")
	}
	rows, err := tx.Query(ctx, `
		WITH batch AS (
		 SELECT c.*,h.contract_version_id FROM contract_shipping_handoff_cargo c
		 JOIN contract_shipping_handoffs h ON h.tenant_id=c.tenant_id AND h.id=c.handoff_id
		 WHERE c.tenant_id=$1 AND c.handoff_id=$2 AND c.contract_item_id>0 AND c.quantity>0
		)
		SELECT batch.contract_item_id,totals.qty,
		 totals.remapped AND (SELECT count(DISTINCT nc.contract_item_id) FROM contract_shipping_handoff_cargo nc
		  JOIN contract_shipping_handoffs nh ON nh.tenant_id=nc.tenant_id AND nh.id=nc.handoff_id
		  WHERE nc.tenant_id=$1 AND nh.contract_id=$3 AND nh.contract_version_id=batch.contract_version_id
		   AND nc.product_code=batch.product_code AND nc.product_name=batch.product_name
		   AND nc.specification=batch.specification AND nc.uom_code=batch.uom_code)>1
		FROM batch CROSS JOIN LATERAL (
		 SELECT sum(cargo.quantity)::text AS qty,
		  bool_or(h.contract_version_id<>batch.contract_version_id AND cargo.contract_item_id<>batch.contract_item_id) AS remapped
		 FROM contract_shipping_handoff_cargo cargo
		 JOIN contract_shipping_handoffs h ON h.tenant_id=cargo.tenant_id AND h.id=cargo.handoff_id
		 JOIN shipping_schedules voyage ON voyage.tenant_id=cargo.tenant_id AND voyage.contract_handoff_id=cargo.handoff_id
		 WHERE cargo.tenant_id=$1 AND voyage.contract_id=$3
		  AND (cargo.contract_item_id=batch.contract_item_id OR (h.contract_version_id<>batch.contract_version_id
		   AND cargo.product_code=batch.product_code AND cargo.product_name=batch.product_name
		   AND cargo.specification=batch.specification AND cargo.uom_code=batch.uom_code))
		  AND (voyage.id=$4 OR voyage.atd IS NOT NULL OR voyage.status IN ('SAILED','IN_TRANSIT','ARRIVED','COMPLETED')
		   OR EXISTS(SELECT 1 FROM shipping_schedule_changes ch WHERE ch.tenant_id=voyage.tenant_id AND ch.schedule_id=voyage.id
		    AND ch.field_name='status' AND ch.new_value IN ('SAILED','IN_TRANSIT','ARRIVED')))
		) totals ORDER BY batch.contract_item_id`, current.TenantID, *current.ContractHandoffID, *current.ContractID, current.ID)
	if err != nil {
		return err
	}
	var lines []DeliveryQualityLine
	for rows.Next() {
		var line DeliveryQualityLine
		var ambiguous bool
		if err := rows.Scan(&line.ContractItemID, &line.Qty, &ambiguous); err != nil {
			rows.Close()
			return err
		}
		if ambiguous {
			rows.Close()
			return apierr.Conflict("QUALITY_VERSION_ALLOCATION_REQUIRED", "合同变更后存在相同规格的多条明细，无法确认历史已发量归属，请先核对货物关联")
		}
		lines = append(lines, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(lines) == 0 {
		return apierr.Conflict("QUALITY_DELIVERY_SOURCE_REQUIRED", "本批货物没有可核验的合同明细，请先补齐货物关联并完成质检")
	}
	return s.deliveryQuality.Check(ctx, *current.ContractID, lines)
}
