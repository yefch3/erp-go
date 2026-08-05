package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

type ScheduleDetails struct {
	Schedule  store.ShippingSchedule
	Changes   []store.ShippingScheduleChange
	Route     []store.ShippingRouteNode
	Delays    []store.ShippingDelayEvent
	Reminders []store.ShippingArrivalReminder
}

type RouteNodeInput struct {
	NodeType, PortCode, PortName, Timezone string
	InsertAfterNodeID                      int64
	LatestETAAt, LatestETDAt               string
	Reason, Remark                         string
	RouteVersion                           int32
}

type ProgressInput struct {
	RouteNodeID                               int64
	Action, ActualTime, LatestETA, ReasonCode string
	Reason, ImpactType, Note                  string
	LatestETAAt, LatestETDAt                  string
	ActualArrivalAt, ActualDepartureAt        string
	AffectedNodeID, FromNodeID, ToNodeID      int64
	RouteVersion                              int32
}

func dateTimestamp(d pgtype.Date) pgtype.Timestamptz {
	if !d.Valid {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: d.Time.UTC(), Valid: true}
}

func parseTimestamp(value, field string) (pgtype.Timestamptz, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Timestamptz{}, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return pgtype.Timestamptz{}, apierr.Invalid("SHIPPING_TIME_INVALID", field+"必须是包含时区的时间")
	}
	return pgtype.Timestamptz{Time: t, Valid: true}, nil
}

func timestampText(v pgtype.Timestamptz) string {
	if !v.Valid {
		return ""
	}
	return v.Time.Format(time.RFC3339)
}

func timestampDate(v pgtype.Timestamptz) pgtype.Date {
	if !v.Valid {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: v.Time, Valid: true}
}

func sameTimestamp(a, b pgtype.Timestamptz) bool {
	return a.Valid == b.Valid && (!a.Valid || a.Time.Equal(b.Time))
}

// validateRouteTimeline checks the route as one chronological chain, not as
// isolated ports. A later port may not occur before the preceding port.
func validateRouteTimeline(nodes []store.ShippingRouteNode, checkEstimates, checkActuals bool) error {
	ordered := append([]store.ShippingRouteNode(nil), nodes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].SequenceNo < ordered[j].SequenceNo })
	for i, node := range ordered {
		if checkEstimates && node.LatestEtaAt.Valid && node.LatestEtdAt.Valid && node.LatestEtdAt.Time.Before(node.LatestEtaAt.Time) {
			return apierr.Invalid("SHIPPING_ROUTE_TIMELINE_INVALID", node.PortName+" 的预计离港时间不能早于预计到港时间")
		}
		if checkActuals && node.ActualArrivalAt.Valid && node.ActualDepartureAt.Valid && node.ActualDepartureAt.Time.Before(node.ActualArrivalAt.Time) {
			return apierr.Invalid("SHIPPING_ROUTE_TIMELINE_INVALID", node.PortName+" 的实际离港时间不能早于实际到港时间")
		}
		if i == 0 {
			continue
		}
		previous := ordered[i-1]
		previousEstimate := previous.LatestEtdAt
		if !previousEstimate.Valid {
			previousEstimate = previous.LatestEtaAt
		}
		currentEstimate := node.LatestEtaAt
		if !currentEstimate.Valid {
			currentEstimate = node.LatestEtdAt
		}
		if checkEstimates && previousEstimate.Valid && currentEstimate.Valid && currentEstimate.Time.Before(previousEstimate.Time) {
			return apierr.Invalid("SHIPPING_ROUTE_TIMELINE_INVALID", fmt.Sprintf("%s 的预计时间不能早于前一港口 %s", node.PortName, previous.PortName))
		}
		previousActual := previous.ActualDepartureAt
		if !previousActual.Valid {
			previousActual = previous.ActualArrivalAt
		}
		currentActual := node.ActualArrivalAt
		if !currentActual.Valid {
			currentActual = node.ActualDepartureAt
		}
		if checkActuals && previousActual.Valid && currentActual.Valid && currentActual.Time.Before(previousActual.Time) {
			return apierr.Invalid("SHIPPING_ROUTE_TIMELINE_INVALID", fmt.Sprintf("%s 的实际时间不能早于前一港口 %s", node.PortName, previous.PortName))
		}
	}
	return nil
}

func createInitialRoute(ctx context.Context, q *store.Queries, s store.ShippingSchedule, op Operator) error {
	origin, err := q.InsertRouteNode(ctx, store.InsertRouteNodeParams{
		TenantID: s.TenantID, ScheduleID: s.ID, SequenceNo: 1, NodeType: "ORIGIN",
		PortName: s.PortOfLoading, Timezone: "UTC", OriginalEtdAt: dateTimestamp(s.Etd),
		CreatedBy: op.ID, CreatedByName: op.Name,
	})
	if err != nil {
		return err
	}
	destination, err := q.InsertRouteNode(ctx, store.InsertRouteNodeParams{
		TenantID: s.TenantID, ScheduleID: s.ID, SequenceNo: 2, NodeType: "DESTINATION",
		PortName: s.PortOfDischarge, Timezone: "UTC", OriginalEtaAt: dateTimestamp(s.Eta),
		CreatedBy: op.ID, CreatedByName: op.Name,
	})
	if err != nil {
		return err
	}
	_, err = q.UpdateScheduleProgress(ctx, store.UpdateScheduleProgressParams{
		TenantID: s.TenantID, ID: s.ID, CurrentRouteNodeID: &origin.ID,
		CurrentProgress: "等待离开 " + s.PortOfLoading, UpdatedBy: op.ID, UpdatedByName: op.Name,
	})
	if err != nil {
		return err
	}
	return q.CreateArrivalReminder(ctx, store.CreateArrivalReminderParams{
		TenantID: s.TenantID, ScheduleID: s.ID, DestinationNodeID: destination.ID,
		RecipientEmployeeID: s.ResponsibleEmployeeID, EtaRevision: s.EtaRevision, TargetEta: s.Eta,
	})
}

func (s *Service) GetScheduleDetails(ctx context.Context, tenantID, id int64) (ScheduleDetails, error) {
	schedule, changes, err := s.GetSchedule(ctx, tenantID, id)
	if err != nil {
		return ScheduleDetails{}, err
	}
	route, err := s.q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
	if err != nil {
		return ScheduleDetails{}, err
	}
	delays, err := s.q.ListDelayEvents(ctx, store.ListDelayEventsParams{TenantID: tenantID, ScheduleID: id})
	if err != nil {
		return ScheduleDetails{}, err
	}
	reminders, err := s.q.ListArrivalReminders(ctx, store.ListArrivalRemindersParams{TenantID: tenantID, ScheduleID: id})
	return ScheduleDetails{Schedule: schedule, Changes: changes, Route: route, Delays: delays, Reminders: reminders}, err
}

func (s *Service) ShippingStatistics(ctx context.Context, tenantID int64) (store.ShippingStatisticsRow, error) {
	return s.q.ShippingStatistics(ctx, tenantID)
}

func validRouteNodeType(v string) bool { return v == "TRANSIT" || v == "TEMPORARY" }

func (s *Service) AddRouteNode(ctx context.Context, tenantID, id int64, in RouteNodeInput, op Operator) ([]store.ShippingRouteNode, int32, error) {
	in.NodeType = strings.ToUpper(strings.TrimSpace(in.NodeType))
	in.PortName = strings.TrimSpace(in.PortName)
	in.Reason = strings.TrimSpace(in.Reason)
	if !validRouteNodeType(in.NodeType) || in.PortName == "" || in.InsertAfterNodeID == 0 || in.Reason == "" {
		return nil, 0, apierr.Invalid("SHIPPING_ROUTE_FIELDS_REQUIRED", "港口类型、港口名称、插入位置和变更原因必填")
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	eta, err := parseTimestamp(in.LatestETAAt, "预计到港时间")
	if err != nil {
		return nil, 0, err
	}
	etd, err := parseTimestamp(in.LatestETDAt, "预计离港时间")
	if err != nil {
		return nil, 0, err
	}
	var version int32
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetScheduleForUpdate(ctx, store.GetScheduleForUpdateParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
		}
		if err != nil {
			return err
		}
		if current.Status == "COMPLETED" || current.Status == "CANCELLED" {
			return apierr.Conflict("SHIPPING_FINAL_STATE", "已完成或已取消的船期不能调整路线")
		}
		if current.RouteVersion != in.RouteVersion {
			return apierr.Conflict("SHIPPING_ROUTE_VERSION_CONFLICT", "路线已被其他员工更新，请刷新后重试")
		}
		nodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
		if err != nil {
			return err
		}
		insertAt := -1
		for i, n := range nodes {
			if n.ID == in.InsertAfterNodeID {
				if n.NodeType == "DESTINATION" {
					return apierr.Invalid("SHIPPING_ROUTE_POSITION_INVALID", "不能在目的港之后添加港口")
				}
				insertAt = i + 1
			}
		}
		if insertAt < 0 {
			return apierr.NotFound("SHIPPING_ROUTE_NODE_NOT_FOUND", "插入位置不存在")
		}
		for _, n := range nodes {
			if err := q.SetRouteNodeSequence(ctx, store.SetRouteNodeSequenceParams{TenantID: tenantID, ScheduleID: id, ID: n.ID, SequenceNo: n.SequenceNo + 100000}); err != nil {
				return err
			}
		}
		newNode, err := q.InsertRouteNode(ctx, store.InsertRouteNodeParams{
			TenantID: tenantID, ScheduleID: id, SequenceNo: int32(insertAt + 1), NodeType: in.NodeType,
			PortCode: strings.TrimSpace(in.PortCode), PortName: in.PortName, Timezone: in.Timezone,
			OriginalEtaAt: eta, OriginalEtdAt: etd, Remark: strings.TrimSpace(in.Remark), CreatedBy: op.ID, CreatedByName: op.Name,
		})
		if err != nil {
			return err
		}
		ordered := append(nodes, store.ShippingRouteNode{})
		copy(ordered[insertAt+1:], ordered[insertAt:])
		ordered[insertAt] = newNode
		if err = validateRouteTimeline(ordered, true, true); err != nil {
			return err
		}
		for i, n := range ordered {
			if n.ID == newNode.ID {
				continue
			}
			if err := q.SetRouteNodeSequence(ctx, store.SetRouteNodeSequenceParams{TenantID: tenantID, ScheduleID: id, ID: n.ID, SequenceNo: int32(i + 1)}); err != nil {
				return err
			}
		}
		version, err = q.BumpRouteVersion(ctx, store.BumpRouteVersionParams{TenantID: tenantID, ID: id, UpdatedBy: op.ID, UpdatedByName: op.Name})
		if err != nil {
			return err
		}
		kind := "ROUTE"
		if in.NodeType == "TEMPORARY" {
			kind = "TEMPORARY_CALL"
		}
		return addChange(ctx, q, tenantID, id, kind, "route", "", fmt.Sprintf("新增%s：%s", in.NodeType, in.PortName), in.Reason, op)
	})
	if err != nil {
		return nil, 0, err
	}
	nodes, err := s.q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
	return nodes, version, err
}

// RemoveRouteNode soft-deletes an unused transit or temporary-call node. The
// row and its change history remain available for audit; origin/destination
// nodes and ports that already have operational progress cannot be removed.
func (s *Service) RemoveRouteNode(ctx context.Context, tenantID, id, nodeID int64, reason string, routeVersion int32, op Operator) ([]store.ShippingRouteNode, int32, error) {
	reason = strings.TrimSpace(reason)
	if nodeID == 0 || reason == "" {
		return nil, 0, apierr.Invalid("SHIPPING_ROUTE_REMOVE_FIELDS_REQUIRED", "港口节点和移除原因必填")
	}
	var version int32
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetScheduleForUpdate(ctx, store.GetScheduleForUpdateParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
		}
		if err != nil {
			return err
		}
		if current.Status == "COMPLETED" || current.Status == "CANCELLED" {
			return apierr.Conflict("SHIPPING_FINAL_STATE", "已完成或已取消的船期不能移除港口")
		}
		if current.RouteVersion != routeVersion {
			return apierr.Conflict("SHIPPING_ROUTE_VERSION_CONFLICT", "路线已被其他员工更新，请刷新后重试")
		}
		node, err := q.GetRouteNodeForUpdate(ctx, store.GetRouteNodeForUpdateParams{TenantID: tenantID, ScheduleID: id, ID: nodeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("SHIPPING_ROUTE_NODE_NOT_FOUND", "港口节点不存在")
		}
		if err != nil {
			return err
		}
		if node.NodeType != "TRANSIT" && node.NodeType != "TEMPORARY" {
			return apierr.Invalid("SHIPPING_ROUTE_TERMINAL_REMOVE_FORBIDDEN", "起运港和目的港不能移除")
		}
		if node.ActualArrivalAt.Valid || node.ActualDepartureAt.Valid || node.NodeStatus == "ARRIVED" || node.NodeStatus == "DEPARTED" || node.NodeStatus == "SKIPPED" {
			return apierr.Conflict("SHIPPING_ROUTE_NODE_HAS_PROGRESS", "已有到港、离港或跳过记录的港口不能移除，请使用进度更正")
		}
		if err = q.DeactivateRouteNode(ctx, store.DeactivateRouteNodeParams{TenantID: tenantID, ScheduleID: id, ID: nodeID, UpdatedBy: op.ID, UpdatedByName: op.Name}); err != nil {
			return err
		}
		version, err = q.BumpRouteVersion(ctx, store.BumpRouteVersionParams{TenantID: tenantID, ID: id, UpdatedBy: op.ID, UpdatedByName: op.Name})
		if err != nil {
			return err
		}
		nodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
		if err != nil {
			return err
		}
		currentNodeID, progress, status := deriveProgress(nodes)
		if _, err = q.UpdateScheduleProgress(ctx, store.UpdateScheduleProgressParams{TenantID: tenantID, ID: id, CurrentRouteNodeID: &currentNodeID, CurrentProgress: progress, UpdatedBy: op.ID, UpdatedByName: op.Name}); err != nil {
			return err
		}
		if current.Status != "DELAYED" && current.Status != status {
			if _, err = q.UpdateScheduleStatus(ctx, store.UpdateScheduleStatusParams{TenantID: tenantID, ID: id, Status: status, UpdatedBy: op.ID, UpdatedByName: op.Name}); err != nil {
				return err
			}
			if err = addChange(ctx, q, tenantID, id, "STATUS", "status", current.Status, status, "根据港口移除自动更新", op); err != nil {
				return err
			}
		}
		kind := "ROUTE"
		if node.NodeType == "TEMPORARY" {
			kind = "TEMPORARY_CALL"
		}
		return addChange(ctx, q, tenantID, id, kind, "route_node", fmt.Sprintf("%d:%s", node.ID, node.PortName), "已移除", reason, op)
	})
	if err != nil {
		return nil, 0, err
	}
	nodes, err := s.q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
	return nodes, version, err
}

func idsText(nodes []store.ShippingRouteNode) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = fmt.Sprintf("%d:%s", n.ID, n.PortName)
	}
	return strings.Join(parts, " → ")
}

func (s *Service) ReorderRoute(ctx context.Context, tenantID, id int64, ids []int64, reason string, routeVersion int32, op Operator) ([]store.ShippingRouteNode, int32, error) {
	if len(ids) < 2 || strings.TrimSpace(reason) == "" {
		return nil, 0, apierr.Invalid("SHIPPING_ROUTE_ORDER_REQUIRED", "完整路线顺序和变更原因必填")
	}
	var version int32
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetScheduleForUpdate(ctx, store.GetScheduleForUpdateParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
		}
		if err != nil {
			return err
		}
		if current.RouteVersion != routeVersion {
			return apierr.Conflict("SHIPPING_ROUTE_VERSION_CONFLICT", "路线已被其他员工更新，请刷新后重试")
		}
		nodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
		if err != nil {
			return err
		}
		if len(nodes) != len(ids) {
			return apierr.Invalid("SHIPPING_ROUTE_ORDER_INVALID", "路线顺序必须包含全部有效港口")
		}
		byID := make(map[int64]store.ShippingRouteNode, len(nodes))
		for _, n := range nodes {
			byID[n.ID] = n
		}
		ordered := make([]store.ShippingRouteNode, len(ids))
		for i, nodeID := range ids {
			n, ok := byID[nodeID]
			if !ok {
				return apierr.Invalid("SHIPPING_ROUTE_ORDER_INVALID", "路线包含无效港口")
			}
			ordered[i] = n
			delete(byID, nodeID)
		}
		if ordered[0].NodeType != "ORIGIN" || ordered[len(ordered)-1].NodeType != "DESTINATION" {
			return apierr.Invalid("SHIPPING_ROUTE_TERMINALS_FIXED", "起运港必须排第一，目的港必须排最后")
		}
		if err = validateRouteTimeline(ordered, true, true); err != nil {
			return err
		}
		oldOrder := idsText(nodes)
		for _, n := range nodes {
			if err := q.SetRouteNodeSequence(ctx, store.SetRouteNodeSequenceParams{TenantID: tenantID, ScheduleID: id, ID: n.ID, SequenceNo: n.SequenceNo + 100000}); err != nil {
				return err
			}
		}
		for i, n := range ordered {
			if err := q.SetRouteNodeSequence(ctx, store.SetRouteNodeSequenceParams{TenantID: tenantID, ScheduleID: id, ID: n.ID, SequenceNo: int32(i + 1)}); err != nil {
				return err
			}
		}
		version, err = q.BumpRouteVersion(ctx, store.BumpRouteVersionParams{TenantID: tenantID, ID: id, UpdatedBy: op.ID, UpdatedByName: op.Name})
		if err != nil {
			return err
		}
		return addChange(ctx, q, tenantID, id, "ROUTE", "route_order", oldOrder, idsText(ordered), strings.TrimSpace(reason), op)
	})
	if err != nil {
		return nil, 0, err
	}
	nodes, err := s.q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
	return nodes, version, err
}

func ptrID(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// deriveProgress rebuilds the visible current position after a correction.
// Completed ports stay in the route history, while the first port after the
// furthest completed node becomes the current destination.
func deriveProgress(nodes []store.ShippingRouteNode) (int64, string, string) {
	if len(nodes) == 0 {
		return 0, "", "PLANNED"
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		switch n.NodeStatus {
		case "ARRIVED":
			status := "IN_TRANSIT"
			if n.NodeType == "DESTINATION" {
				status = "ARRIVED"
			}
			return n.ID, "已抵达 " + n.PortName, status
		case "APPROACHING":
			return n.ID, "驶往 " + n.PortName, "IN_TRANSIT"
		case "DEPARTED", "SKIPPED":
			if i+1 < len(nodes) {
				next := nodes[i+1]
				status := "IN_TRANSIT"
				if n.NodeType == "ORIGIN" {
					status = "SAILED"
				}
				return next.ID, "驶往 " + next.PortName, status
			}
			return n.ID, "已离开 " + n.PortName, "IN_TRANSIT"
		}
	}
	return nodes[0].ID, "等待离开 " + nodes[0].PortName, "PLANNED"
}

// updateNodeTimes changes only the editable latest/actual timestamps. Original
// ETA/ETD values are intentionally untouched. Every changed field is appended
// to the schedule history, and terminal changes are synchronized to the
// schedule summary and its ETA reminder lifecycle.
func (s *Service) updateNodeTimes(ctx context.Context, q *store.Queries, current store.ShippingSchedule, node store.ShippingRouteNode, in ProgressInput, op Operator) (store.ShippingSchedule, error) {
	latestETA, err := parseTimestamp(in.LatestETAAt, "预计到港时间")
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	latestETD, err := parseTimestamp(in.LatestETDAt, "预计离港时间")
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	actualArrival, err := parseTimestamp(in.ActualArrivalAt, "实际到港时间")
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	actualDeparture, err := parseTimestamp(in.ActualDepartureAt, "实际离港时间")
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	if node.NodeType == "DESTINATION" && !latestETA.Valid {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_DESTINATION_ETA_REQUIRED", "目的港预计到港时间不能为空")
	}
	if node.NodeType == "ORIGIN" && !latestETD.Valid {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_ORIGIN_ETD_REQUIRED", "起运港预计离港时间不能为空")
	}
	if latestETA.Valid && latestETD.Valid && latestETD.Time.Before(latestETA.Time) {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_NODE_ETD_BEFORE_ETA", "预计离港时间不能早于预计到港时间")
	}
	if actualArrival.Valid && actualDeparture.Valid && actualDeparture.Time.Before(actualArrival.Time) {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_NODE_DEPARTURE_BEFORE_ARRIVAL", "实际离港时间不能早于实际到港时间")
	}

	changes := []struct {
		field string
		old   pgtype.Timestamptz
		new   pgtype.Timestamptz
	}{
		{"latest_eta_at", node.LatestEtaAt, latestETA},
		{"latest_etd_at", node.LatestEtdAt, latestETD},
		{"actual_arrival_at", node.ActualArrivalAt, actualArrival},
		{"actual_departure_at", node.ActualDepartureAt, actualDeparture},
	}
	changed := false
	for _, c := range changes {
		changed = changed || !sameTimestamp(c.old, c.new)
	}
	if !changed {
		return store.ShippingSchedule{}, apierr.Conflict("SHIPPING_NODE_TIMES_UNCHANGED", "港口时间没有变化")
	}

	routeNodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: current.TenantID, ScheduleID: current.ID})
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	for i := range routeNodes {
		if routeNodes[i].ID == node.ID {
			routeNodes[i].LatestEtaAt, routeNodes[i].LatestEtdAt = latestETA, latestETD
			routeNodes[i].ActualArrivalAt, routeNodes[i].ActualDepartureAt = actualArrival, actualDeparture
			break
		}
	}
	estimateChanged := !sameTimestamp(node.LatestEtaAt, latestETA) || !sameTimestamp(node.LatestEtdAt, latestETD)
	actualChanged := !sameTimestamp(node.ActualArrivalAt, actualArrival) || !sameTimestamp(node.ActualDepartureAt, actualDeparture)
	if err = validateRouteTimeline(routeNodes, estimateChanged, actualChanged); err != nil {
		return store.ShippingSchedule{}, err
	}

	_, err = q.UpdateRouteNodeTimes(ctx, store.UpdateRouteNodeTimesParams{
		TenantID: current.TenantID, ScheduleID: current.ID, ID: node.ID,
		LatestEtaAt: latestETA, LatestEtdAt: latestETD,
		ActualArrivalAt: actualArrival, ActualDepartureAt: actualDeparture,
		UpdatedBy: op.ID, UpdatedByName: op.Name,
	})
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	out := current
	for _, c := range changes {
		if sameTimestamp(c.old, c.new) {
			continue
		}
		if err = addChange(ctx, q, current.TenantID, current.ID, "PROGRESS", node.PortName+"."+c.field, timestampText(c.old), timestampText(c.new), in.Reason, op); err != nil {
			return store.ShippingSchedule{}, err
		}
	}

	if node.NodeType == "ORIGIN" && !sameTimestamp(node.LatestEtdAt, latestETD) {
		out, err = q.SetScheduleETD(ctx, store.SetScheduleETDParams{TenantID: current.TenantID, ID: current.ID, Etd: timestampDate(latestETD), UpdatedBy: op.ID, UpdatedByName: op.Name})
	}
	if err == nil && node.NodeType == "ORIGIN" && !sameTimestamp(node.ActualDepartureAt, actualDeparture) {
		out, err = q.SetScheduleATDNullable(ctx, store.SetScheduleATDNullableParams{TenantID: current.TenantID, ID: current.ID, Atd: timestampDate(actualDeparture), UpdatedBy: op.ID, UpdatedByName: op.Name})
	}
	if err == nil && node.NodeType == "DESTINATION" && !sameTimestamp(node.ActualArrivalAt, actualArrival) {
		out, err = q.SetScheduleATANullable(ctx, store.SetScheduleATANullableParams{TenantID: current.TenantID, ID: current.ID, Ata: timestampDate(actualArrival), UpdatedBy: op.ID, UpdatedByName: op.Name})
	}
	if err != nil {
		return store.ShippingSchedule{}, err
	}

	if node.NodeType == "DESTINATION" && !sameTimestamp(node.LatestEtaAt, latestETA) {
		newETA := timestampDate(latestETA)
		if dateText(newETA) != dateText(current.Eta) {
			oldETA := current.Eta
			out, err = q.UpdateScheduleETA(ctx, store.UpdateScheduleETAParams{TenantID: current.TenantID, ID: current.ID, Eta: newETA, UpdatedBy: op.ID, UpdatedByName: op.Name})
			if err != nil {
				return store.ShippingSchedule{}, err
			}
			_, err = q.AddDelayEvent(ctx, store.AddDelayEventParams{
				TenantID: current.TenantID, ScheduleID: current.ID, ImpactType: "PORT", AffectedNodeID: &node.ID,
				ReasonCode: "OTHER", Reason: in.Reason, Note: strings.TrimSpace(in.Note),
				OldEta: oldETA, NewEta: newETA, ChangeDays: int32(newETA.Time.Sub(oldETA.Time).Hours() / 24),
				CumulativeDelayDays: out.DelayDays, OperatorID: op.ID, OperatorName: op.Name,
			})
			if err != nil {
				return store.ShippingSchedule{}, err
			}
			if err = q.CancelPendingReminders(ctx, store.CancelPendingRemindersParams{TenantID: current.TenantID, ScheduleID: current.ID}); err != nil {
				return store.ShippingSchedule{}, err
			}
			if err = q.CreateArrivalReminder(ctx, store.CreateArrivalReminderParams{TenantID: current.TenantID, ScheduleID: current.ID, DestinationNodeID: node.ID, RecipientEmployeeID: out.ResponsibleEmployeeID, EtaRevision: out.EtaRevision, TargetEta: out.Eta}); err != nil {
				return store.ShippingSchedule{}, err
			}
		}
	}

	nodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: current.TenantID, ScheduleID: current.ID})
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	currentNodeID, progress, status := deriveProgress(nodes)
	out, err = q.UpdateScheduleProgress(ctx, store.UpdateScheduleProgressParams{TenantID: current.TenantID, ID: current.ID, CurrentRouteNodeID: &currentNodeID, CurrentProgress: progress, UpdatedBy: op.ID, UpdatedByName: op.Name})
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	if current.Status != status {
		out, err = q.UpdateScheduleStatus(ctx, store.UpdateScheduleStatusParams{TenantID: current.TenantID, ID: current.ID, Status: status, UpdatedBy: op.ID, UpdatedByName: op.Name})
		if err != nil {
			return store.ShippingSchedule{}, err
		}
		if err = addChange(ctx, q, current.TenantID, current.ID, "STATUS", "status", current.Status, status, "根据港口时间更正自动更新", op); err != nil {
			return store.ShippingSchedule{}, err
		}
	}
	return out, nil
}

// UpdateProgress records operational movement at a route node. Progress actions
// drive the schedule lifecycle automatically; UPDATE_TIMES is the audited
// correction path and never overwrites original ETA/ETD values.
func (s *Service) UpdateProgress(ctx context.Context, tenantID, id int64, in ProgressInput, op Operator) (store.ShippingSchedule, []store.ShippingRouteNode, []store.ShippingDelayEvent, error) {
	in.Action = strings.ToUpper(strings.TrimSpace(in.Action))
	in.Reason = strings.TrimSpace(in.Reason)
	if in.RouteNodeID == 0 || in.Action == "" || in.Reason == "" {
		return store.ShippingSchedule{}, nil, nil, apierr.Invalid("SHIPPING_PROGRESS_FIELDS_REQUIRED", "港口、进度动作和原因必填")
	}
	var out store.ShippingSchedule
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetScheduleForUpdate(ctx, store.GetScheduleForUpdateParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
		}
		if err != nil {
			return err
		}
		if current.Status == "COMPLETED" || current.Status == "CANCELLED" {
			return apierr.Conflict("SHIPPING_FINAL_STATE", "已完成或已取消的船期不能更新进度")
		}
		if current.RouteVersion != in.RouteVersion {
			return apierr.Conflict("SHIPPING_ROUTE_VERSION_CONFLICT", "路线已更新，请刷新后重试")
		}
		node, err := q.GetRouteNodeForUpdate(ctx, store.GetRouteNodeForUpdateParams{TenantID: tenantID, ScheduleID: id, ID: in.RouteNodeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("SHIPPING_ROUTE_NODE_NOT_FOUND", "港口节点不存在")
		}
		if err != nil {
			return err
		}
		oldStatus := node.NodeStatus
		progress := ""
		switch in.Action {
		case "APPROACH":
			if err = q.ResetOtherApproachingRouteNodes(ctx, store.ResetOtherApproachingRouteNodesParams{TenantID: tenantID, ScheduleID: id, ID: node.ID}); err != nil {
				return err
			}
			_, err = q.SetRouteNodeApproaching(ctx, store.SetRouteNodeApproachingParams{TenantID: tenantID, ScheduleID: id, ID: node.ID, UpdatedBy: op.ID, UpdatedByName: op.Name})
			progress = "驶往 " + node.PortName
		case "ARRIVE", "DEPART":
			actual, e := parseTimestamp(in.ActualTime, "实际时间")
			if e != nil {
				return e
			}
			if !actual.Valid {
				actual = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			}
			routeNodes, e := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
			if e != nil {
				return e
			}
			for i := range routeNodes {
				if routeNodes[i].ID != node.ID {
					continue
				}
				if in.Action == "ARRIVE" {
					routeNodes[i].ActualArrivalAt = actual
				} else {
					routeNodes[i].ActualDepartureAt = actual
				}
				break
			}
			if e = validateRouteTimeline(routeNodes, false, true); e != nil {
				return e
			}
			if in.Action == "ARRIVE" {
				_, err = q.SetRouteNodeArrival(ctx, store.SetRouteNodeArrivalParams{TenantID: tenantID, ScheduleID: id, ID: node.ID, ActualArrivalAt: actual, UpdatedBy: op.ID, UpdatedByName: op.Name})
				progress = "已抵达 " + node.PortName
				if node.NodeType == "DESTINATION" {
					out, err = q.SetScheduleATA(ctx, store.SetScheduleATAParams{TenantID: tenantID, ID: id, Ata: pgtype.Date{Time: actual.Time, Valid: true}, UpdatedBy: op.ID, UpdatedByName: op.Name})
				}
			} else {
				_, err = q.SetRouteNodeDeparture(ctx, store.SetRouteNodeDepartureParams{TenantID: tenantID, ScheduleID: id, ID: node.ID, ActualDepartureAt: actual, UpdatedBy: op.ID, UpdatedByName: op.Name})
				progress = "已离开 " + node.PortName
				if node.NodeType == "ORIGIN" {
					out, err = q.SetScheduleATD(ctx, store.SetScheduleATDParams{TenantID: tenantID, ID: id, Atd: pgtype.Date{Time: actual.Time, Valid: true}, UpdatedBy: op.ID, UpdatedByName: op.Name})
				}
			}
		case "SKIP":
			_, err = q.SetRouteNodeSkipped(ctx, store.SetRouteNodeSkippedParams{TenantID: tenantID, ScheduleID: id, ID: node.ID, UpdatedBy: op.ID, UpdatedByName: op.Name})
			progress = "已跳过 " + node.PortName
		case "UPDATE_ETA":
			if node.NodeType != "DESTINATION" {
				return apierr.Invalid("SHIPPING_ETA_DESTINATION_ONLY", "船期最新ETA只能在目的港更新")
			}
			newETA, e := parseDate(in.LatestETA, "最新ETA", true)
			if e != nil {
				return e
			}
			if dateText(newETA) == dateText(current.Eta) {
				return apierr.Conflict("SHIPPING_ETA_UNCHANGED", "最新ETA没有变化")
			}
			routeNodes, e := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
			if e != nil {
				return e
			}
			for i := range routeNodes {
				if routeNodes[i].ID == node.ID {
					routeNodes[i].LatestEtaAt = dateTimestamp(newETA)
					break
				}
			}
			if e = validateRouteTimeline(routeNodes, true, false); e != nil {
				return e
			}
			_, err = q.SetDestinationETA(ctx, store.SetDestinationETAParams{TenantID: tenantID, ScheduleID: id, ID: node.ID, LatestEtaAt: dateTimestamp(newETA), UpdatedBy: op.ID, UpdatedByName: op.Name})
			if err != nil {
				return err
			}
			oldETA := current.Eta
			impact := defaultImpact(in.ImpactType)
			if impact == "PORT" && in.AffectedNodeID == 0 {
				return apierr.Invalid("SHIPPING_DELAY_PORT_REQUIRED", "港口延误必须选择受影响港口")
			}
			if impact == "LEG" && (in.FromNodeID == 0 || in.ToNodeID == 0) {
				return apierr.Invalid("SHIPPING_DELAY_LEG_REQUIRED", "航段延误必须选择起点和终点")
			}
			out, err = q.UpdateScheduleETA(ctx, store.UpdateScheduleETAParams{TenantID: tenantID, ID: id, Eta: newETA, UpdatedBy: op.ID, UpdatedByName: op.Name})
			if err != nil {
				return err
			}
			changeDays := int32(newETA.Time.Sub(oldETA.Time).Hours() / 24)
			_, err = q.AddDelayEvent(ctx, store.AddDelayEventParams{
				TenantID: tenantID, ScheduleID: id, ImpactType: impact,
				AffectedNodeID: ptrID(in.AffectedNodeID), FromNodeID: ptrID(in.FromNodeID), ToNodeID: ptrID(in.ToNodeID),
				ReasonCode: defaultReasonCode(in.ReasonCode), Reason: in.Reason, Note: strings.TrimSpace(in.Note),
				OldEta: oldETA, NewEta: newETA, ChangeDays: changeDays, CumulativeDelayDays: out.DelayDays,
				OperatorID: op.ID, OperatorName: op.Name,
			})
			if err != nil {
				return err
			}
			if err = q.CancelPendingReminders(ctx, store.CancelPendingRemindersParams{TenantID: tenantID, ScheduleID: id}); err != nil {
				return err
			}
			if err = q.CreateArrivalReminder(ctx, store.CreateArrivalReminderParams{TenantID: tenantID, ScheduleID: id, DestinationNodeID: node.ID, RecipientEmployeeID: out.ResponsibleEmployeeID, EtaRevision: out.EtaRevision, TargetEta: out.Eta}); err != nil {
				return err
			}
			if err = addChange(ctx, q, tenantID, id, "ETA", "latest_eta", dateText(oldETA), dateText(newETA), in.Reason, op); err != nil {
				return err
			}
			progress = current.CurrentProgress
		case "UPDATE_TIMES":
			out, err = s.updateNodeTimes(ctx, q, current, node, in, op)
			progress = current.CurrentProgress
		default:
			return apierr.Invalid("SHIPPING_PROGRESS_ACTION_INVALID", "进度动作无效")
		}
		if err != nil {
			return err
		}
		if in.Action != "UPDATE_ETA" && in.Action != "UPDATE_TIMES" {
			out, err = q.UpdateScheduleProgress(ctx, store.UpdateScheduleProgressParams{TenantID: tenantID, ID: id, CurrentRouteNodeID: &node.ID, CurrentProgress: progress, UpdatedBy: op.ID, UpdatedByName: op.Name})
			if err != nil {
				return err
			}
			nextStatus := ""
			if in.Action == "DEPART" && node.NodeType == "ORIGIN" && current.Status == "PLANNED" {
				nextStatus = "SAILED"
			}
			if (in.Action == "APPROACH" || in.Action == "ARRIVE" || in.Action == "DEPART") && node.NodeType != "ORIGIN" && node.NodeType != "DESTINATION" && (current.Status == "PLANNED" || current.Status == "SAILED") {
				nextStatus = "IN_TRANSIT"
			}
			if in.Action == "ARRIVE" && node.NodeType == "DESTINATION" && current.Status != "ARRIVED" && current.Status != "COMPLETED" {
				nextStatus = "ARRIVED"
			}
			if nextStatus != "" {
				out, err = q.UpdateScheduleStatus(ctx, store.UpdateScheduleStatusParams{TenantID: tenantID, ID: id, Status: nextStatus, UpdatedBy: op.ID, UpdatedByName: op.Name})
				if err != nil {
					return err
				}
				if err = addChange(ctx, q, tenantID, id, "STATUS", "status", current.Status, nextStatus, "根据港口进度自动更新", op); err != nil {
					return err
				}
			}
			if err = addChange(ctx, q, tenantID, id, "PROGRESS", node.PortName, oldStatus, in.Action, in.Reason, op); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return store.ShippingSchedule{}, nil, nil, err
	}
	route, err := s.q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
	if err != nil {
		return store.ShippingSchedule{}, nil, nil, err
	}
	delays, err := s.q.ListDelayEvents(ctx, store.ListDelayEventsParams{TenantID: tenantID, ScheduleID: id})
	return out, route, delays, err
}

func defaultReasonCode(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return "OTHER"
	}
	return v
}
func defaultImpact(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "PORT" || v == "LEG" {
		return v
	}
	return "SCHEDULE"
}

// StableRouteIDs is kept small and exported for route-order unit tests.
func StableRouteIDs(nodes []store.ShippingRouteNode) []int64 {
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].SequenceNo < nodes[j].SequenceNo })
	ids := make([]int64, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
