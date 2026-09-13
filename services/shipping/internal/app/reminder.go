package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

// ReminderNotifier 在持久化通知提交后，仅发送尽力而为的页面刷新提示。
// 通知本身始终保存在 PostgreSQL 中，因此 Redis 故障不会造成通知丢失。
type ReminderNotifier interface {
	NotifyArrivalReminder(context.Context, int64, int64, int64)
}

type ArrivalNotificationPage struct {
	Reminders   []store.ShippingArrivalReminder
	UnreadCount int64
}

const (
	maxArrivalReminderRules = 20
	maxArrivalReminderDays  = 3650
)

// normalizeArrivalReminderDays 校验、去重并按从大到小排列提醒天数。
// 0 代表港口事件当天；空列表代表关闭该船期尚未触发的运输提醒。
func normalizeArrivalReminderDays(days []int32) ([]int32, error) {
	if len(days) > maxArrivalReminderRules {
		return nil, apierr.Invalid("SHIPPING_REMINDER_RULE_LIMIT", "每条船期最多设置 20 个运输提醒")
	}
	seen := make(map[int32]struct{}, len(days))
	out := make([]int32, 0, len(days))
	for _, day := range days {
		if day < 0 || day > maxArrivalReminderDays {
			return nil, apierr.Invalid("SHIPPING_REMINDER_DAYS_INVALID", "提醒天数必须在 0 到 3650 之间")
		}
		if _, ok := seen[day]; ok {
			continue
		}
		seen[day] = struct{}{}
		out = append(out, day)
	}
	slices.SortFunc(out, func(a, b int32) int { return int(b - a) })
	return out, nil
}

func timestampEventDate(value pgtype.Timestamptz, timezone string) pgtype.Date {
	if !value.Valid {
		return pgtype.Date{}
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		location = time.UTC
	}
	local := value.Time.In(location)
	return pgtype.Date{Time: time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func transportEventRevision(value pgtype.Timestamptz) int32 {
	if !value.Valid {
		return 0
	}
	// 数据库字段沿用旧版 eta_revision。毫秒时间散列让港口时间每次变化都能
	// 生成一个新版本，同时相同时间重复保存仍保持幂等。
	return int32((value.Time.UTC().UnixMilli() % 2147483646) + 1)
}

// createConfiguredTransportReminders 为各有效港口的预计到港和预计离港建立提醒。
// 目的港没有离港业务，因此只生成到港提醒；已经发生的实际事件不再提醒。
func createConfiguredTransportReminders(ctx context.Context, q *store.Queries, schedule store.ShippingSchedule) error {
	days, err := q.ListArrivalReminderRules(ctx, store.ListArrivalReminderRulesParams{TenantID: schedule.TenantID, ScheduleID: schedule.ID})
	if err != nil {
		return err
	}
	preference, err := q.GetUserReminderPreference(ctx, store.GetUserReminderPreferenceParams{TenantID: schedule.TenantID, EmployeeID: schedule.ResponsibleEmployeeID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		preference = store.ShippingUserReminderPreference{Timezone: "UTC", CachedHolidays: []byte("{}")}
	}
	nodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: schedule.TenantID, ScheduleID: schedule.ID})
	if err != nil {
		return err
	}
	create := func(node store.ShippingRouteNode, eventType string, at pgtype.Timestamptz) error {
		target := timestampEventDate(at, node.Timezone)
		now := time.Now().UTC()
		type pendingReminder struct {
			day int32
			due pgtype.Timestamptz
		}
		var latestDue *pendingReminder
		createDay := func(day int32, due pgtype.Timestamptz) error {
			return q.CreateRouteEventReminder(ctx, store.CreateRouteEventReminderParams{
				TenantID: schedule.TenantID, ScheduleID: schedule.ID, DestinationNodeID: node.ID,
				RecipientEmployeeID: schedule.ResponsibleEmployeeID, EtaRevision: transportEventRevision(at),
				TargetEta: target, EventType: eventType, LeadDays: day, DueAt: due,
			})
		}
		for _, day := range days {
			due := arrivalReminderDueAt(target, day, preference.Timezone, preference.CachedHolidays)
			if due.Time.After(now) {
				if err := createDay(day, due); err != nil {
					return err
				}
				continue
			}
			if latestDue == nil || due.Time.After(latestDue.due.Time) {
				latestDue = &pendingReminder{day: day, due: due}
			}
		}
		// 如果修改时间时已经越过多个提醒点，只即时补发最近的一条，避免
		// 同一个港口事件一次弹出 14/7/3 天三条相同通知。
		if latestDue != nil {
			if err := createDay(latestDue.day, latestDue.due); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range nodes {
		if !node.IsActive {
			continue
		}
		if node.LatestEtaAt.Valid && !node.ActualArrivalAt.Valid {
			if err := create(node, "ARRIVAL", node.LatestEtaAt); err != nil {
				return err
			}
		}
		if node.NodeType != "DESTINATION" && node.LatestEtdAt.Valid && !node.ActualDepartureAt.Valid {
			if err := create(node, "DEPARTURE", node.LatestEtdAt); err != nil {
				return err
			}
		}
	}
	return nil
}

// arrivalReminderDueAt applies the recipient's business timezone and skips
// Saturdays and Sundays. Lead day 0 always means the ETA date itself.
func arrivalReminderDueAt(eta pgtype.Date, leadDays int32, timezone string, _ []byte) pgtype.Timestamptz {
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		location = time.UTC
	}
	year, month, day := eta.Time.Date()
	due := time.Date(year, month, day, 0, 0, 0, 0, location)
	for remaining := leadDays; remaining > 0; {
		due = due.AddDate(0, 0, -1)
		if due.Weekday() == time.Saturday || due.Weekday() == time.Sunday {
			continue
		}
		remaining--
	}
	return pgtype.Timestamptz{Time: due.UTC(), Valid: eta.Valid}
}

// GetArrivalReminderRules 返回某条船期当前启用的全部提前天数。
func (s *Service) GetArrivalReminderRules(ctx context.Context, tenantID, scheduleID int64, operators ...Operator) ([]int32, error) {
	var op Operator
	if len(operators) > 0 {
		op = operators[0]
	}
	if _, err := s.authorizeSchedule(ctx, tenantID, scheduleID, op); err != nil {
		return nil, err
	}
	if _, err := s.q.GetSchedule(ctx, store.GetScheduleParams{TenantID: tenantID, ID: scheduleID}); errors.Is(err, pgx.ErrNoRows) {
		return nil, apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
	} else if err != nil {
		return nil, err
	}
	return s.q.ListArrivalReminderRules(ctx, store.ListArrivalReminderRulesParams{TenantID: tenantID, ScheduleID: scheduleID})
}

// UpdateArrivalReminderRules 替换一条船期的提醒规则，并按最新 ETA 重建未发送提醒。
// 已发送提醒作为业务历史保留，不会因修改规则而删除。
func (s *Service) UpdateArrivalReminderRules(ctx context.Context, tenantID, scheduleID int64, days []int32, op Operator) ([]int32, error) {
	if _, err := s.authorizeSchedule(ctx, tenantID, scheduleID, op); err != nil {
		return nil, err
	}
	normalized, err := normalizeArrivalReminderDays(days)
	if err != nil {
		return nil, err
	}
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.GetScheduleForUpdate(ctx, store.GetScheduleForUpdateParams{TenantID: tenantID, ID: scheduleID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.NotFound("SHIPPING_NOT_FOUND", "船期不存在")
		}
		if err != nil {
			return err
		}
		if current.Status == "COMPLETED" || current.Status == "CANCELLED" {
			return apierr.Conflict("SHIPPING_FINAL_STATE", "已完成或已取消的船期不能修改提醒")
		}
		oldDays, err := q.ListArrivalReminderRules(ctx, store.ListArrivalReminderRulesParams{TenantID: tenantID, ScheduleID: scheduleID})
		if err != nil {
			return err
		}
		if err = q.CancelPendingReminders(ctx, store.CancelPendingRemindersParams{TenantID: tenantID, ScheduleID: scheduleID}); err != nil {
			return err
		}
		if err = q.DeleteArrivalReminderRules(ctx, store.DeleteArrivalReminderRulesParams{TenantID: tenantID, ScheduleID: scheduleID}); err != nil {
			return err
		}
		for _, day := range normalized {
			if err = q.CreateArrivalReminderRule(ctx, store.CreateArrivalReminderRuleParams{TenantID: tenantID, ScheduleID: scheduleID, LeadDays: day, CreatedBy: op.ID, CreatedByName: op.Name}); err != nil {
				return err
			}
		}
		if err = createConfiguredTransportReminders(ctx, q, current); err != nil {
			return err
		}
		toText := func(values []int32) string {
			parts := make([]string, len(values))
			for i, value := range values {
				parts[i] = strconv.Itoa(int(value))
			}
			return strings.Join(parts, ",")
		}
		return addChange(ctx, q, tenantID, scheduleID, "REMINDER", "transport_reminder_days", toText(oldDays), toText(normalized), "修改运输提醒设置", op)
	})
	if err == nil {
		s.wakeReminderWorker()
	}
	return normalized, err
}

func (s *Service) UseReminderNotifier(notifier ReminderNotifier) { s.reminderNotifier = notifier }

func (s *Service) wakeReminderWorker() {
	select {
	case s.reminderWake <- struct{}{}:
	default:
	}
}

func reminderRetryAt(attempt int32, now time.Time) time.Time {
	minutes := 1 << min(attempt, 6)
	return now.Add(time.Duration(minutes) * time.Minute)
}

// arrivalReminderLeadDays 从持久化的提醒类型（例如 ARRIVAL_14D）读取提前天数。
// 无法识别的旧数据使用通用标题，避免再次显示错误的固定天数。
func transportReminderType(reminderType string) (string, int32, bool) {
	parts := strings.Split(strings.TrimSpace(reminderType), "_")
	if len(parts) != 2 || (parts[0] != "ARRIVAL" && parts[0] != "DEPARTURE") {
		return "", 0, false
	}
	value := strings.TrimSuffix(parts[1], "D")
	days, err := strconv.ParseInt(value, 10, 32)
	if err != nil || days < 0 {
		return "", 0, false
	}
	return parts[0], int32(days), true
}

func arrivalReminderText(r store.GetDueArrivalReminderForUpdateRow, now time.Time) (string, string, string) {
	value := func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "—"
		}
		return strings.TrimSpace(v)
	}
	eventType, _, _ := transportReminderType(r.ReminderType)
	eventName := "到港"
	if eventType == "DEPARTURE" {
		eventName = "开船"
	}
	title := "船期即将" + eventName
	if r.TargetEta.Valid {
		today := now.UTC().Truncate(24 * time.Hour)
		eta := r.TargetEta.Time.UTC().Truncate(24 * time.Hour)
		days := int(eta.Sub(today) / (24 * time.Hour))
		switch {
		case days > 0:
			title = fmt.Sprintf("船期预计 %d 天后%s", days, eventName)
		case days == 0:
			title = "船期预计今天" + eventName
		default:
			title = fmt.Sprintf("船期预计已逾期 %d 天", -days)
		}
	}
	portName := r.PortName
	if strings.TrimSpace(portName) == "" {
		portName = r.PortOfDischarge
		if eventType == "DEPARTURE" {
			portName = r.PortOfLoading
		}
	}
	port := value(portName)
	timeLabel := "预计到港（ETA）"
	if eventType == "DEPARTURE" {
		timeLabel = "预计离港（ETD）"
	}
	content := fmt.Sprintf("船期编号：%s；合同编号：%s；客户：%s；船名/航次：%s / %s；港口：%s；%s：%s",
		value(r.ScheduleNo), value(r.ContractNo), value(r.CustomerName), value(r.VesselName),
		value(r.VoyageNo), port, timeLabel, dateText(r.TargetEta))
	return title, content, fmt.Sprintf("/shipping/%d", r.ScheduleID)
}

// ProcessDueArrivalReminders 确保每条到期提醒只持久化一次。
// 每条提醒使用独立事务；单条失败会被记录并重试，不会回滚已成功发送的提醒。
func (s *Service) ProcessDueArrivalReminders(ctx context.Context, batchSize int32) (int, error) {
	if batchSize <= 0 {
		batchSize = 100
	}
	if err := s.q.CancelIneligibleArrivalReminders(ctx); err != nil {
		return 0, err
	}
	ids, err := s.q.ListDueArrivalReminderIDs(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	delivered := 0
	for _, id := range ids {
		var notification store.ShippingArrivalReminder
		var attempt int32
		err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			candidate, getErr := q.GetDueArrivalReminderForUpdate(ctx, id)
			if getErr != nil {
				return getErr
			}
			attempt = candidate.AttemptCount
			title, content, link := arrivalReminderText(candidate, time.Now().UTC())
			notification, getErr = q.MarkArrivalReminderSent(ctx, store.MarkArrivalReminderSentParams{
				ID: id, Title: title, Content: content, DetailUrl: link,
			})
			if getErr != nil {
				return getErr
			}
			field := "transport_reminder"
			if eventType, days, ok := transportReminderType(candidate.ReminderType); ok {
				field = fmt.Sprintf("%s_%dd", strings.ToLower(eventType), days)
			}
			return addChange(ctx, q, candidate.TenantID, candidate.ScheduleID, "REMINDER", field, "", title,
				"系统按最新港口时间生成运输提醒", Operator{Name: "系统提醒任务"})
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			retry := pgtype.Timestamptz{Time: reminderRetryAt(attempt, time.Now().UTC()), Valid: true}
			if markErr := s.q.MarkArrivalReminderFailed(ctx, store.MarkArrivalReminderFailedParams{
				ID: id, LastError: err.Error(), NextRetryAt: retry,
			}); markErr != nil {
				s.log.Error("arrival reminder failed and retry could not be recorded", "id", id, "err", err, "mark_err", markErr)
			} else {
				s.log.Error("arrival reminder failed; retry scheduled", "id", id, "err", err, "retry_at", retry.Time)
			}
			continue
		}
		delivered++
		if s.reminderNotifier != nil {
			s.reminderNotifier.NotifyArrivalReminder(ctx, notification.TenantID, notification.RecipientEmployeeID, notification.ScheduleID)
		}
	}
	return delivered, nil
}

// RunArrivalReminderWorker 在服务启动、每日定时以及 ETA 新增或改期时运行，
// 用于立即补扫应当生成的到港提醒。
func (s *Service) RunArrivalReminderWorker(ctx context.Context, interval time.Duration, batchSize int32) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	run := func() {
		for {
			count, err := s.ProcessDueArrivalReminders(ctx, batchSize)
			if err != nil {
				s.log.Error("arrival reminder sweep failed; next run will retry", "err", err)
				return
			}
			if count < int(batchSize) {
				return
			}
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// 失败记录带有 next_retry_at；短周期重试会按该时间处理，
	// 同时不会改变每日全量扫描的节奏。
	retryTicker := time.NewTicker(time.Minute)
	defer retryTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		case <-retryTicker.C:
			run()
		case <-s.reminderWake:
			run()
		}
	}
}

func (s *Service) ListArrivalNotifications(ctx context.Context, tenantID, employeeID int64, unreadOnly bool) (ArrivalNotificationPage, error) {
	if employeeID == 0 {
		return ArrivalNotificationPage{}, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	rows, err := s.q.ListEmployeeArrivalNotifications(ctx, store.ListEmployeeArrivalNotificationsParams{
		TenantID: tenantID, RecipientEmployeeID: employeeID, UnreadOnly: unreadOnly,
	})
	if err != nil {
		return ArrivalNotificationPage{}, err
	}
	unread, err := s.q.CountUnreadEmployeeArrivalNotifications(ctx, store.CountUnreadEmployeeArrivalNotificationsParams{
		TenantID: tenantID, RecipientEmployeeID: employeeID,
	})
	return ArrivalNotificationPage{Reminders: rows, UnreadCount: unread}, err
}

func (s *Service) MarkArrivalReminderRead(ctx context.Context, tenantID, employeeID, reminderID int64) (store.ShippingArrivalReminder, error) {
	if employeeID == 0 {
		return store.ShippingArrivalReminder{}, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	row, err := s.q.MarkEmployeeArrivalReminderRead(ctx, store.MarkEmployeeArrivalReminderReadParams{
		TenantID: tenantID, RecipientEmployeeID: employeeID, ID: reminderID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ShippingArrivalReminder{}, apierr.NotFound("SHIPPING_REMINDER_NOT_FOUND", "运输提醒不存在")
	}
	return row, err
}

// CleanupExpiredArrivalReminders 删除当前员工已过期或已结束船期的通知，保留船期本身。
func (s *Service) CleanupExpiredArrivalReminders(ctx context.Context, tenantID, employeeID int64) (int64, error) {
	if employeeID == 0 {
		return 0, apierr.Unauthorized("AUTH_TOKEN_MISSING", "缺少登录凭证")
	}
	ids, err := s.q.DeleteExpiredEmployeeArrivalReminders(ctx, store.DeleteExpiredEmployeeArrivalRemindersParams{
		TenantID: tenantID, RecipientEmployeeID: employeeID,
	})
	return int64(len(ids)), err
}
