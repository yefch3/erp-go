package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
)

type CargoScheduleProgress struct {
	ID, ContractID                         int64
	ScheduleNo, Status, ETD, ETA, ATD, ATA string
}

// This is a narrow cross-department projection, not general schedule access.
// The gateway has already authorized the PO and resolved its contract line IDs.
// Recheck the tenant, contract, schedule and allocated cargo in one query.
func (s *Service) GetCargoScheduleProgress(ctx context.Context, tenantID, scheduleID, contractID int64, itemIDs []int64) (CargoScheduleProgress, error) {
	if scheduleID <= 0 || contractID <= 0 || len(itemIDs) == 0 || len(itemIDs) > 500 {
		return CargoScheduleProgress{}, apierr.Invalid("CARGO_PROGRESS_REFERENCE_INVALID", "船期货物关联无效")
	}
	for _, id := range itemIDs {
		if id <= 0 {
			return CargoScheduleProgress{}, apierr.Invalid("CARGO_PROGRESS_REFERENCE_INVALID", "船期货物关联无效")
		}
	}
	var out CargoScheduleProgress
	err := s.pool.QueryRow(ctx, `SELECT s.id,s.contract_id,s.schedule_no,s.status,coalesce(s.etd::text,''),coalesce(s.eta::text,''),coalesce(s.atd::text,''),coalesce(s.ata::text,'') FROM shipping_schedules s WHERE s.tenant_id=$1 AND s.id=$2 AND s.contract_id=$3 AND EXISTS(SELECT 1 FROM contract_shipping_handoffs h JOIN contract_shipping_handoff_cargo c ON c.tenant_id=h.tenant_id AND c.handoff_id=h.id WHERE h.tenant_id=s.tenant_id AND h.schedule_id=s.id AND h.contract_id=s.contract_id AND c.contract_item_id=ANY($4::bigint[]))`, tenantID, scheduleID, contractID, itemIDs).Scan(&out.ID, &out.ContractID, &out.ScheduleNo, &out.Status, &out.ETD, &out.ETA, &out.ATD, &out.ATA)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, apierr.NotFound("CARGO_SCHEDULE_NOT_FOUND", "未找到本采购货物关联的船期")
	}
	return out, err
}
