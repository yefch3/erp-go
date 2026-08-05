// Package app contains shipping schedule use cases and business validation.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

const SchemaVersion int32 = 4

type databasePinger interface{ Ping(context.Context) error }

type Service struct {
	db   databasePinger
	pool *pgxpool.Pool
	q    *store.Queries
}

// New accepts the small pinger interface so the readiness check remains easy
// to unit test. Schedule commands require the production pgx pool.
func New(db databasePinger) *Service {
	s := &Service{db: db}
	if pool, ok := db.(*pgxpool.Pool); ok {
		s.pool, s.q = pool, store.New(pool)
	}
	return s
}

func (s *Service) ModuleStatus(ctx context.Context) error { return s.db.Ping(ctx) }

type Operator struct {
	ID   int64
	Name string
}

type ScheduleInput struct {
	ContractID, CustomerID, CarrierID                    int64
	ContractNo, CustomerName, CarrierForwarder           string
	VesselName, VoyageNo, PortOfLoading, PortOfDischarge string
	ETD, ATD, ETA, ATA                                   string
	ResponsibleEmployeeID                                int64
	ResponsibleName, Remark                              string
}

type ListFilter struct {
	Keyword, Status, PortOfLoading, PortOfDischarge string
	ETDFrom, ETDTo, ETAFrom, ETATo                  string
	Page, PageSize                                  int32
}

func parseDate(value, field string, required bool) (pgtype.Date, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return pgtype.Date{}, apierr.Invalid("SHIPPING_REQUIRED_FIELDS", field+"必填")
		}
		return pgtype.Date{}, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return pgtype.Date{}, apierr.Invalid("SHIPPING_DATE_INVALID", field+"必须是 YYYY-MM-DD 日期")
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

func validateInput(in ScheduleInput) (store.CreateScheduleParams, error) {
	in.VesselName = strings.TrimSpace(in.VesselName)
	in.VoyageNo = strings.TrimSpace(in.VoyageNo)
	in.PortOfLoading = strings.TrimSpace(in.PortOfLoading)
	in.PortOfDischarge = strings.TrimSpace(in.PortOfDischarge)
	in.ResponsibleName = strings.TrimSpace(in.ResponsibleName)
	if in.VesselName == "" || in.VoyageNo == "" || in.PortOfLoading == "" ||
		in.PortOfDischarge == "" || in.ResponsibleEmployeeID == 0 || in.ResponsibleName == "" {
		return store.CreateScheduleParams{}, apierr.Invalid(
			"SHIPPING_REQUIRED_FIELDS", "船名、航次、起运港、目的港、ETD、ETA 和负责人必填")
	}
	etd, err := parseDate(in.ETD, "ETD", true)
	if err != nil {
		return store.CreateScheduleParams{}, err
	}
	eta, err := parseDate(in.ETA, "ETA", true)
	if err != nil {
		return store.CreateScheduleParams{}, err
	}
	if eta.Time.Before(etd.Time) {
		return store.CreateScheduleParams{}, apierr.Invalid("SHIPPING_ETA_BEFORE_ETD", "ETA 不能早于 ETD")
	}
	var contractID, customerID, carrierID *int64
	if in.ContractID > 0 {
		contractID = &in.ContractID
	}
	if in.CustomerID > 0 {
		customerID = &in.CustomerID
	}
	if in.CarrierID > 0 {
		carrierID = &in.CarrierID
	}
	return store.CreateScheduleParams{
		ContractID: contractID, ContractNo: strings.TrimSpace(in.ContractNo),
		CustomerID: customerID, CustomerName: strings.TrimSpace(in.CustomerName),
		CarrierID: carrierID, CarrierForwarder: strings.TrimSpace(in.CarrierForwarder), VesselName: in.VesselName,
		VoyageNo: in.VoyageNo, PortOfLoading: in.PortOfLoading,
		PortOfDischarge: in.PortOfDischarge, Etd: etd, Eta: eta,
		ResponsibleEmployeeID: in.ResponsibleEmployeeID, ResponsibleName: in.ResponsibleName,
		Remark: strings.TrimSpace(in.Remark),
	}, nil
}

func (s *Service) rejectDuplicate(ctx context.Context, q *store.Queries, tenantID, excludeID int64, p store.CreateScheduleParams, confirmed bool) error {
	if confirmed {
		return nil
	}
	rows, err := q.FindPossibleDuplicates(ctx, store.FindPossibleDuplicatesParams{
		TenantID: tenantID, VesselName: p.VesselName, VoyageNo: p.VoyageNo,
		PortOfLoading: p.PortOfLoading, Etd: p.Etd, ID: excludeID,
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	numbers := make([]string, len(rows))
	for i, row := range rows {
		numbers[i] = row.ScheduleNo
	}
	return apierr.Conflict("SHIPPING_POSSIBLE_DUPLICATE",
		"发现可能重复的船期（"+strings.Join(numbers, "、")+"），请确认后继续")
}

func (s *Service) CreateSchedule(ctx context.Context, tenantID int64, in ScheduleInput, op Operator, confirmed bool) (store.ShippingSchedule, error) {
	p, err := validateInput(in)
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	if err = s.rejectDuplicate(ctx, s.q, tenantID, 0, p, confirmed); err != nil {
		return store.ShippingSchedule{}, err
	}
	p.TenantID, p.CreatedBy, p.CreatedByName = tenantID, op.ID, op.Name
	var out store.ShippingSchedule
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		out, err = q.CreateSchedule(ctx, p)
		if err != nil {
			return err
		}
		if err = createInitialRoute(ctx, q, out, op); err != nil {
			return err
		}
		out, err = q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenantID, ID: out.ID})
		return err
	})
	return out, err
}

func (s *Service) GetSchedule(ctx context.Context, tenantID, id int64) (store.ShippingSchedule, []store.ShippingScheduleChange, error) {
	schedule, err := s.q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ShippingSchedule{}, nil, apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
	}
	if err != nil {
		return store.ShippingSchedule{}, nil, err
	}
	changes, err := s.q.ListScheduleChanges(ctx, store.ListScheduleChangesParams{TenantID: tenantID, ScheduleID: id})
	return schedule, changes, err
}

func (s *Service) ListSchedules(ctx context.Context, tenantID int64, f ListFilter) ([]store.ListSchedulesRow, int64, int32, int32, error) {
	f.Status = strings.ToUpper(strings.TrimSpace(f.Status))
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 20
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	if f.Status != "" && f.Status != "ACTIVE" && f.Status != "ARCHIVED" && !validStatus(f.Status) {
		return nil, 0, f.Page, f.PageSize, apierr.Invalid("SHIPPING_STATUS_INVALID", "船期状态无效")
	}
	etdFrom, err := parseDate(f.ETDFrom, "ETD 起始日期", false)
	if err != nil {
		return nil, 0, f.Page, f.PageSize, err
	}
	etdTo, err := parseDate(f.ETDTo, "ETD 结束日期", false)
	if err != nil {
		return nil, 0, f.Page, f.PageSize, err
	}
	etaFrom, err := parseDate(f.ETAFrom, "ETA 起始日期", false)
	if err != nil {
		return nil, 0, f.Page, f.PageSize, err
	}
	etaTo, err := parseDate(f.ETATo, "ETA 结束日期", false)
	if err != nil {
		return nil, 0, f.Page, f.PageSize, err
	}
	rows, err := s.q.ListSchedules(ctx, store.ListSchedulesParams{
		TenantID: tenantID, Keyword: strings.TrimSpace(f.Keyword), Status: f.Status,
		PortOfLoading: strings.TrimSpace(f.PortOfLoading), PortOfDischarge: strings.TrimSpace(f.PortOfDischarge),
		EtdFrom: etdFrom, EtdTo: etdTo, EtaFrom: etaFrom, EtaTo: etaTo,
		RowLimit: f.PageSize, RowOffset: (f.Page - 1) * f.PageSize,
	})
	if err != nil {
		return nil, 0, f.Page, f.PageSize, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	} else {
		total, err = s.q.CountSchedules(ctx, store.CountSchedulesParams{
			TenantID: tenantID, Keyword: strings.TrimSpace(f.Keyword), Status: f.Status,
			PortOfLoading: strings.TrimSpace(f.PortOfLoading), PortOfDischarge: strings.TrimSpace(f.PortOfDischarge),
			EtdFrom: etdFrom, EtdTo: etdTo, EtaFrom: etaFrom, EtaTo: etaTo,
		})
		if err != nil {
			return nil, 0, f.Page, f.PageSize, err
		}
	}
	return rows, total, f.Page, f.PageSize, nil
}

func dateText(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

func addChange(ctx context.Context, q *store.Queries, tenantID, id int64, kind, field, oldValue, newValue, reason string, op Operator) error {
	if oldValue == newValue {
		return nil
	}
	return q.AddScheduleChange(ctx, store.AddScheduleChangeParams{
		TenantID: tenantID, ScheduleID: id, ChangeType: kind, FieldName: field,
		OldValue: oldValue, NewValue: newValue, Reason: reason,
		OperatorID: op.ID, OperatorName: op.Name,
	})
}

func (s *Service) UpdateSchedule(ctx context.Context, tenantID, id int64, in ScheduleInput, reason string, op Operator, confirmed bool) (store.ShippingSchedule, error) {
	p, err := validateInput(in)
	if err != nil {
		return store.ShippingSchedule{}, err
	}
	if err = s.rejectDuplicate(ctx, s.q, tenantID, id, p, confirmed); err != nil {
		return store.ShippingSchedule{}, err
	}
	var out store.ShippingSchedule
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
			return apierr.Conflict("SHIPPING_FINAL_STATE", "已完成或已取消的船期不能编辑")
		}
		// Actual departure/arrival belong to progress tracking. A basic edit
		// must preserve them instead of accepting or clearing those timestamps.
		p.Atd, p.Ata = current.Atd, current.Ata
		dateChanged := dateText(current.Etd) != dateText(p.Etd) || dateText(current.Eta) != dateText(p.Eta)
		if dateChanged && strings.TrimSpace(reason) == "" {
			return apierr.Invalid("SHIPPING_DATE_REASON_REQUIRED", "修改 ETD 或 ETA 时必须填写原因")
		}
		out, err = q.UpdateSchedule(ctx, store.UpdateScheduleParams{
			TenantID: tenantID, ID: id, ContractID: p.ContractID, ContractNo: p.ContractNo,
			CustomerID: p.CustomerID, CustomerName: p.CustomerName, CarrierID: p.CarrierID, CarrierForwarder: p.CarrierForwarder,
			VesselName: p.VesselName, VoyageNo: p.VoyageNo, PortOfLoading: p.PortOfLoading,
			PortOfDischarge: p.PortOfDischarge, Etd: p.Etd, Atd: p.Atd, Eta: p.Eta, Ata: p.Ata,
			ResponsibleEmployeeID: p.ResponsibleEmployeeID, ResponsibleName: p.ResponsibleName,
			Remark: p.Remark, UpdatedBy: op.ID, UpdatedByName: op.Name,
		})
		if err != nil {
			return err
		}
		if dateText(current.Eta) != dateText(p.Eta) {
			out, err = q.UpdateScheduleETA(ctx, store.UpdateScheduleETAParams{
				TenantID: tenantID, ID: id, Eta: p.Eta, UpdatedBy: op.ID, UpdatedByName: op.Name,
			})
			if err != nil {
				return err
			}
			nodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: id})
			if err != nil {
				return err
			}
			var destinationID int64
			for _, node := range nodes {
				if node.NodeType == "DESTINATION" {
					destinationID = node.ID
					break
				}
			}
			if destinationID == 0 {
				return apierr.Conflict("SHIPPING_DESTINATION_MISSING", "船期缺少目的港节点")
			}
			if _, err = q.SetDestinationETA(ctx, store.SetDestinationETAParams{TenantID: tenantID, ScheduleID: id, ID: destinationID, LatestEtaAt: dateTimestamp(p.Eta), UpdatedBy: op.ID, UpdatedByName: op.Name}); err != nil {
				return err
			}
			changeDays := int32(p.Eta.Time.Sub(current.Eta.Time).Hours() / 24)
			if _, err = q.AddDelayEvent(ctx, store.AddDelayEventParams{
				TenantID: tenantID, ScheduleID: id, ImpactType: "SCHEDULE", ReasonCode: "OTHER",
				Reason: strings.TrimSpace(reason), OldEta: current.Eta, NewEta: p.Eta,
				ChangeDays: changeDays, CumulativeDelayDays: out.DelayDays,
				OperatorID: op.ID, OperatorName: op.Name,
			}); err != nil {
				return err
			}
			if err = q.CancelPendingReminders(ctx, store.CancelPendingRemindersParams{TenantID: tenantID, ScheduleID: id}); err != nil {
				return err
			}
			if err = q.CreateArrivalReminder(ctx, store.CreateArrivalReminderParams{TenantID: tenantID, ScheduleID: id, DestinationNodeID: destinationID, RecipientEmployeeID: out.ResponsibleEmployeeID, EtaRevision: out.EtaRevision, TargetEta: out.Eta}); err != nil {
				return err
			}
		}
		if err = addChange(ctx, q, tenantID, id, "DATE", "ETD", dateText(current.Etd), dateText(p.Etd), strings.TrimSpace(reason), op); err != nil {
			return err
		}
		if err = addChange(ctx, q, tenantID, id, "ETA", "latest_eta", dateText(current.Eta), dateText(p.Eta), strings.TrimSpace(reason), op); err != nil {
			return err
		}
		if err = addChange(ctx, q, tenantID, id, "VESSEL_VOYAGE", "vessel_name", current.VesselName, p.VesselName, "基础信息修改", op); err != nil {
			return err
		}
		return addChange(ctx, q, tenantID, id, "VESSEL_VOYAGE", "voyage_no", current.VoyageNo, p.VoyageNo, "基础信息修改", op)
	})
	return out, err
}

func validStatus(status string) bool {
	switch status {
	case "PLANNED", "SAILED", "IN_TRANSIT", "ARRIVED", "COMPLETED", "DELAYED", "CANCELLED":
		return true
	default:
		return false
	}
}

var transitions = map[string]map[string]bool{
	"PLANNED":    {"SAILED": true, "DELAYED": true, "CANCELLED": true},
	"SAILED":     {"IN_TRANSIT": true, "DELAYED": true, "CANCELLED": true},
	"IN_TRANSIT": {"ARRIVED": true, "DELAYED": true, "CANCELLED": true},
	"DELAYED":    {"PLANNED": true, "SAILED": true, "IN_TRANSIT": true, "ARRIVED": true, "CANCELLED": true},
	"ARRIVED":    {"COMPLETED": true},
}

func (s *Service) changeStatus(ctx context.Context, tenantID, id int64, next, reason, kind string, op Operator) (store.ShippingSchedule, error) {
	if !validStatus(next) {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_STATUS_INVALID", "船期状态无效")
	}
	if strings.TrimSpace(reason) == "" {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_STATUS_REASON_REQUIRED", "状态修改原因必填")
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
		if current.Status == next {
			return apierr.Conflict("SHIPPING_STATUS_UNCHANGED", "船期已经是该状态")
		}
		if !transitions[current.Status][next] {
			return apierr.Conflict("SHIPPING_STATUS_TRANSITION_INVALID", fmt.Sprintf("不能从 %s 直接变更为 %s", current.Status, next))
		}
		out, err = q.UpdateScheduleStatus(ctx, store.UpdateScheduleStatusParams{TenantID: tenantID, ID: id, Status: next, UpdatedBy: op.ID, UpdatedByName: op.Name})
		if err != nil {
			return err
		}
		return addChange(ctx, q, tenantID, id, kind, "status", current.Status, next, strings.TrimSpace(reason), op)
	})
	return out, err
}

func (s *Service) UpdateScheduleStatus(ctx context.Context, tenantID, id int64, status, reason string, op Operator) (store.ShippingSchedule, error) {
	if status == "CANCELLED" {
		return store.ShippingSchedule{}, apierr.Invalid("SHIPPING_USE_CANCEL", "请使用取消船期操作")
	}
	return s.changeStatus(ctx, tenantID, id, status, reason, "STATUS", op)
}

func (s *Service) CancelSchedule(ctx context.Context, tenantID, id int64, reason string, op Operator) (store.ShippingSchedule, error) {
	return s.changeStatus(ctx, tenantID, id, "CANCELLED", reason, "CANCEL", op)
}
