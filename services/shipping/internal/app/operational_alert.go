package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

type OperationalAlert struct {
	ID, ScheduleID, RecipientEmployeeID                           int64
	ScheduleNo, ContractNo, AlertType, RecipientRole              string
	Title, Content, OldValue, NewValue, DueDate                   string
	ReadAt, ResolvedAt, ResolutionNote, ResolvedByName, CreatedAt string
}

type operationalAlertPlan struct {
	AlertType, Role, Title, Content, OldValue, NewValue string
	DueDate                                             pgtype.Date
	Recipients                                          []int64
}

func financeETAChangePlan(scheduleNo string, oldETA, newETA pgtype.Date, recipients []int64) (operationalAlertPlan, bool) {
	if !oldETA.Valid || !newETA.Valid || oldETA.Time.Equal(newETA.Time) {
		return operationalAlertPlan{}, false
	}
	plan := operationalAlertPlan{
		AlertType: "ETA_DELAYED", Role: "FINANCE", Title: "ETA 延后，请调整付款安排",
		Content:  "船期 " + scheduleNo + " 的 ETA 已延后，请按最新到港日期调整付款准备。",
		OldValue: dateText(oldETA), NewValue: dateText(newETA), DueDate: newETA, Recipients: recipients,
	}
	if newETA.Time.Before(oldETA.Time) {
		plan.AlertType = "ETA_ADVANCED"
		plan.Title = "ETA 提前，请准备付款"
		plan.Content = "船期 " + scheduleNo + " 的 ETA 已提前，请核对付款准备。"
	}
	return plan, true
}

func (s *Service) recipientsForRoles(ctx context.Context, tenantID int64, roles ...string) ([]int64, error) {
	if s.directory == nil {
		return nil, nil
	}
	scoped := grpcx.WithOperator(ctx, grpcx.Operator{TenantID: tenantID})
	seen := map[int64]struct{}{}
	for _, role := range roles {
		ids, err := s.directory.EmployeeIDsByRole(scoped, role)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if id > 0 {
				seen[id] = struct{}{}
			}
		}
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) scheduleChangeAlertPlans(ctx context.Context, tenantID int64, current store.ShippingSchedule, next store.CreateScheduleParams) ([]operationalAlertPlan, error) {
	plans := make([]operationalAlertPlan, 0, 3)
	if next.Eta.Valid && current.Eta.Valid && next.Eta.Time.Before(current.Eta.Time) {
		recipients, err := s.recipientsForRoles(ctx, tenantID, "FINANCE")
		if err != nil {
			return nil, err
		}
		plans = append(plans, operationalAlertPlan{AlertType: "ETA_ADVANCED", Role: "FINANCE", Title: "ETA 提前，请准备付款", Content: "船期 " + current.ScheduleNo + " 的 ETA 已提前，请核对付款准备。", OldValue: dateText(current.Eta), NewValue: dateText(next.Eta), DueDate: next.Eta, Recipients: recipients})
	} else if next.Eta.Valid && current.Eta.Valid && next.Eta.Time.After(current.Eta.Time) {
		recipients, err := s.recipientsForRoles(ctx, tenantID, "FINANCE")
		if err != nil {
			return nil, err
		}
		plans = append(plans, operationalAlertPlan{AlertType: "ETA_DELAYED", Role: "FINANCE", Title: "ETA 延后，请调整付款安排", Content: "船期 " + current.ScheduleNo + " 的 ETA 已延后，请按最新到港日期调整付款准备。", OldValue: dateText(current.Eta), NewValue: dateText(next.Eta), DueDate: next.Eta, Recipients: recipients})
	}
	if !current.Atd.Valid && (current.Status == "PLANNED" || current.Status == "DELAYED") {
		if next.Etd.Valid && current.Etd.Valid && next.Etd.Time.After(current.Etd.Time) {
			recipients, err := s.recipientsForRoles(ctx, tenantID, "BUYER", "PROCUREMENT_MANAGER")
			if err != nil {
				return nil, err
			}
			plans = append(plans, operationalAlertPlan{AlertType: "ETD_DELAYED", Role: "PROCUREMENT", Title: "ETD 延后，请联系工厂", Content: "船期 " + current.ScheduleNo + " 尚未开航且 ETD 延后，请采购联系工厂确认备货与进仓安排。", OldValue: dateText(current.Etd), NewValue: dateText(next.Etd), DueDate: next.Etd, Recipients: recipients})
		}
		oldWarehouse, _ := parseDate(dateText(current.WarehouseEntryDate), "进仓日期", false)
		newWarehouse, _ := parseDate(next.WarehouseEntryDate, "进仓日期", false)
		if oldWarehouse.Valid && newWarehouse.Valid && newWarehouse.Time.After(oldWarehouse.Time) {
			recipients, err := s.recipientsForRoles(ctx, tenantID, "BUYER", "PROCUREMENT_MANAGER")
			if err != nil {
				return nil, err
			}
			plans = append(plans, operationalAlertPlan{AlertType: "WAREHOUSE_DELAYED", Role: "PROCUREMENT", Title: "进仓日期延后，请联系工厂", Content: "船期 " + current.ScheduleNo + " 尚未开航且进仓日期延后，请采购联系工厂。", OldValue: dateText(oldWarehouse), NewValue: dateText(newWarehouse), DueDate: newWarehouse, Recipients: recipients})
		}
	}
	return plans, nil
}

func createOperationalAlerts(ctx context.Context, q *store.Queries, tenantID, scheduleID int64, plans []operationalAlertPlan) error {
	for _, plan := range plans {
		for _, recipient := range plan.Recipients {
			if err := q.CreateOperationalAlert(ctx, store.CreateOperationalAlertParams{TenantID: tenantID, ScheduleID: scheduleID, AlertType: plan.AlertType, RecipientEmployeeID: recipient, RecipientRole: plan.Role, Title: plan.Title, Content: plan.Content, OldValue: plan.OldValue, NewValue: plan.NewValue, DueDate: plan.DueDate}); err != nil {
				return err
			}
		}
	}
	return nil
}

func alertTime(value pgtype.Timestamptz) string {
	if !value.Valid {
		return ""
	}
	return value.Time.Format(time.RFC3339)
}
func recipientAlert(row store.ListOperationalAlertsForRecipientRow) OperationalAlert {
	return OperationalAlert{ID: row.ID, ScheduleID: row.ScheduleID, RecipientEmployeeID: row.RecipientEmployeeID, ScheduleNo: row.ScheduleNo, ContractNo: row.ContractNo, AlertType: row.AlertType, RecipientRole: row.RecipientRole, Title: row.Title, Content: row.Content, OldValue: row.OldValue, NewValue: row.NewValue, DueDate: dateText(row.DueDate), ReadAt: alertTime(row.ReadAt), ResolvedAt: alertTime(row.ResolvedAt), ResolutionNote: row.ResolutionNote, ResolvedByName: row.ResolvedByName, CreatedAt: alertTime(row.CreatedAt)}
}
func scheduleAlert(row store.ListOperationalAlertsForScheduleRow) OperationalAlert {
	return OperationalAlert{ID: row.ID, ScheduleID: row.ScheduleID, RecipientEmployeeID: row.RecipientEmployeeID, ScheduleNo: row.ScheduleNo, ContractNo: row.ContractNo, AlertType: row.AlertType, RecipientRole: row.RecipientRole, Title: row.Title, Content: row.Content, OldValue: row.OldValue, NewValue: row.NewValue, DueDate: dateText(row.DueDate), ReadAt: alertTime(row.ReadAt), ResolvedAt: alertTime(row.ResolvedAt), ResolutionNote: row.ResolutionNote, ResolvedByName: row.ResolvedByName, CreatedAt: alertTime(row.CreatedAt)}
}

func (s *Service) ListOperationalAlerts(ctx context.Context, tenantID int64, openOnly bool, op Operator) ([]OperationalAlert, int64, error) {
	rows, err := s.q.ListOperationalAlertsForRecipient(ctx, store.ListOperationalAlertsForRecipientParams{TenantID: tenantID, RecipientEmployeeID: op.ID, OpenOnly: openOnly})
	if err != nil {
		return nil, 0, err
	}
	out := make([]OperationalAlert, 0, len(rows))
	var unread int64
	for _, row := range rows {
		out = append(out, recipientAlert(row))
		if !row.ReadAt.Valid {
			unread++
		}
	}
	return out, unread, nil
}

func (s *Service) ListScheduleOperationalAlerts(ctx context.Context, tenantID, scheduleID int64, op Operator) ([]OperationalAlert, error) {
	if _, err := s.authorizeSchedule(ctx, tenantID, scheduleID, op); err != nil {
		return nil, err
	}
	rows, err := s.q.ListOperationalAlertsForSchedule(ctx, store.ListOperationalAlertsForScheduleParams{TenantID: tenantID, ScheduleID: scheduleID})
	if err != nil {
		return nil, err
	}
	out := make([]OperationalAlert, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleAlert(row))
	}
	return out, nil
}

func (s *Service) MarkOperationalAlertsRead(ctx context.Context, tenantID int64, ids []int64, op Operator) (int64, error) {
	return s.q.MarkOperationalAlertsRead(ctx, store.MarkOperationalAlertsReadParams{TenantID: tenantID, RecipientEmployeeID: op.ID, Ids: ids})
}

func (s *Service) ResolveOperationalAlert(ctx context.Context, tenantID, id int64, note string, op Operator) (OperationalAlert, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return OperationalAlert{}, apierr.Invalid("SHIPPING_ALERT_RESOLUTION_REQUIRED", "请填写处理结果")
	}
	resolvedBy := op.ID
	row, err := s.q.ResolveOperationalAlert(ctx, store.ResolveOperationalAlertParams{TenantID: tenantID, ResolvedBy: &resolvedBy, ID: id, ResolutionNote: note, ResolvedByName: op.Name})
	if errors.Is(err, pgx.ErrNoRows) {
		return OperationalAlert{}, apierr.NotFound("SHIPPING_ALERT_NOT_FOUND", "待办不存在或已处理")
	}
	if err != nil {
		return OperationalAlert{}, err
	}
	return OperationalAlert{ID: row.ID, ScheduleID: row.ScheduleID, AlertType: row.AlertType, RecipientRole: row.RecipientRole, Title: row.Title, Content: row.Content, OldValue: row.OldValue, NewValue: row.NewValue, DueDate: dateText(row.DueDate), ReadAt: alertTime(row.ReadAt), ResolvedAt: alertTime(row.ResolvedAt), ResolutionNote: row.ResolutionNote, ResolvedByName: row.ResolvedByName, CreatedAt: alertTime(row.CreatedAt)}, nil
}
