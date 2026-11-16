package app

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
	"strings"
	"time"
)

type PortMetadataCorrection struct {
	Terminal, Name, Code, Timezone, Reason string
	PortID                                 int64
	RouteVersion                           int32
}

// Repairs masterdata identity, including completed voyages. No route times,
// cargo, financial state, transport status or attachments can be changed here.
func (s *Service) CorrectPortMetadata(ctx context.Context, tenant, id int64, in PortMetadataCorrection, op Operator) (store.ShippingSchedule, error) {
	if _, err := s.authorizeSchedule(ctx, tenant, id, op); err != nil {
		return store.ShippingSchedule{}, err
	}
	if (in.Terminal != "ORIGIN" && in.Terminal != "DESTINATION") || in.PortID <= 0 || strings.TrimSpace(in.Reason) == "" || in.Code == "" || in.Name == "" {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_PORT_CORRECTION_REQUIRED", "请选择港口并填写纠正原因")
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_PORT_TIMEZONE_INVALID", "港口时区无效")
	}
	var out store.ShippingSchedule
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetScheduleForUpdate(ctx, store.GetScheduleForUpdateParams{TenantID: tenant, ID: id})
		if err != nil {
			return err
		}
		if current.RouteVersion != in.RouteVersion {
			return apierr.Conflict("SHIPPING_ROUTE_VERSION_CONFLICT", "路线已更新，请刷新后再纠正")
		}
		name, code, zone := current.PortOfLoading, current.LoadingPortCode, current.LoadingPortTimezone
		portID := current.LoadingPortID
		if in.Terminal == "DESTINATION" {
			name, code, zone, portID = current.PortOfDischarge, current.DischargePortCode, current.DischargePortTimezone, current.DischargePortID
		}
		// The physical port name is intentionally immutable in this repair.
		if !strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(in.Name)) {
			return apierr.Conflict("SHIPPING_PORT_NAME_CHANGED", "仅可纠正原港口的编码和时区，不能更换实际港口")
		}
		nodeRows, err := tx.Query(ctx, `SELECT id,port_name FROM shipping_route_nodes WHERE tenant_id=$1 AND schedule_id=$2 AND node_type=$3 FOR UPDATE`, tenant, id, in.Terminal)
		if err != nil {
			return err
		}
		count := 0
		for nodeRows.Next() {
			var nodeID int64
			var nodeName string
			if err = nodeRows.Scan(&nodeID, &nodeName); err != nil {
				nodeRows.Close()
				return err
			}
			count++
			if !strings.EqualFold(strings.TrimSpace(nodeName), strings.TrimSpace(name)) {
				nodeRows.Close()
				return apierr.Conflict("SHIPPING_PORT_ROUTE_MISMATCH", "路线港口名称不一致，请先核对")
			}
		}
		nodeErr := nodeRows.Err()
		nodeRows.Close()
		if nodeErr != nil {
			return nodeErr
		}
		if count != 1 {
			return apierr.Conflict("SHIPPING_PORT_ROUTE_MISMATCH", "未找到唯一的起运港或目的港")
		}
		// These date-only columns are cached local dates of the unchanged route
		// timestamps. Recompute them when repairing the timezone, without recording
		// a new departure/arrival or triggering delay and finance workflow actions.
		if in.Terminal == "ORIGIN" {
			_, err = tx.Exec(ctx, `UPDATE shipping_schedules s SET
 etd=coalesce((n.latest_etd_at AT TIME ZONE $3)::date,s.etd),
 atd=coalesce((n.actual_departure_at AT TIME ZONE $3)::date,s.atd)
 FROM shipping_route_nodes n WHERE s.tenant_id=$1 AND s.id=$2
 AND n.tenant_id=s.tenant_id AND n.schedule_id=s.id AND n.node_type='ORIGIN'`, tenant, id, in.Timezone)
		} else {
			_, err = tx.Exec(ctx, `UPDATE shipping_schedules s SET
 eta=coalesce((n.latest_eta_at AT TIME ZONE $3)::date,s.eta),
 original_eta=coalesce((n.original_eta_at AT TIME ZONE $3)::date,s.original_eta),
 ata=coalesce((n.actual_arrival_at AT TIME ZONE $3)::date,s.ata)
 FROM shipping_route_nodes n WHERE s.tenant_id=$1 AND s.id=$2
 AND n.tenant_id=s.tenant_id AND n.schedule_id=s.id AND n.node_type='DESTINATION'`, tenant, id, in.Timezone)
		}
		if err != nil {
			return err
		}
		refreshed, err := q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenant, ID: id})
		if err != nil {
			return err
		}
		dates := func(s store.ShippingSchedule) string {
			return dateText(s.Etd) + "/" + dateText(s.Atd) + "/" + dateText(s.Eta) + "/" + dateText(s.Ata) + "/" + dateText(s.OriginalEta)
		}
		if portID != nil && *portID == in.PortID && code == in.Code && zone == in.Timezone && dates(current) == dates(refreshed) {
			out = refreshed
			return nil
		}
		if in.Terminal == "ORIGIN" {
			_, err = tx.Exec(ctx, `UPDATE shipping_schedules SET loading_port_id=$3,loading_port_code=$4,loading_port_timezone=$5 WHERE tenant_id=$1 AND id=$2`, tenant, id, in.PortID, in.Code, in.Timezone)
		} else {
			_, err = tx.Exec(ctx, `UPDATE shipping_schedules SET discharge_port_id=$3,discharge_port_code=$4,discharge_port_timezone=$5 WHERE tenant_id=$1 AND id=$2`, tenant, id, in.PortID, in.Code, in.Timezone)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE shipping_route_nodes SET port_id=$4,port_code=$5,timezone=$6 WHERE tenant_id=$1 AND schedule_id=$2 AND node_type=$3`, tenant, id, in.Terminal, in.PortID, in.Code, in.Timezone)
		if err != nil {
			return err
		}
		if _, err = q.BumpRouteVersion(ctx, store.BumpRouteVersionParams{TenantID: tenant, ID: id, UpdatedBy: op.ID, UpdatedByName: op.Name}); err != nil {
			return err
		}
		oldID := int64(0)
		if portID != nil {
			oldID = *portID
		}
		if err = addChange(ctx, q, tenant, id, "ROUTE", in.Terminal+"_PORT_METADATA", fmt.Sprintf("%d/%s/%s; local dates=%s", oldID, code, zone, dates(current)), fmt.Sprintf("%d/%s/%s; local dates=%s", in.PortID, in.Code, in.Timezone, dates(refreshed)), strings.TrimSpace(in.Reason), op); err != nil {
			return err
		}
		out, err = q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenant, ID: id})
		return err
	})
	return out, err
}
