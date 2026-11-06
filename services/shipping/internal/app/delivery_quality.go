package app

import (
	"context"

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
	contractID, err := deliveryContractID(ctx, tx, current)
	if err != nil {
		return err
	}
	// Normal and standalone orders share the real sales-contract lock. Their
	// independent logistics identities must not create separate QC allowances.
	if err := lockDeliveryContract(ctx, tx, current.TenantID, contractID); err != nil {
		return err
	}
	if err := s.checkDispatchContract(ctx, tx, current, contractID); err != nil {
		return err
	}
	lines, err := cumulativeDeliveryLines(ctx, tx, current, contractID)
	if err != nil {
		return err
	}
	return s.deliveryQuality.Check(ctx, contractID, lines)
}
