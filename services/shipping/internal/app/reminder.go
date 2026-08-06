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
// 0 代表到港当天；空列表代表关闭该船期尚未触发的到港提醒。
func normalizeArrivalReminderDays(days []int32) ([]int32, error) {
	if len(days) > maxArrivalReminderRules {
		return nil, apierr.Invalid("SHIPPING_REMINDER_RULE_LIMIT", "每条船期最多设置 20 个到港提醒")
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

// createConfiguredArrivalReminders 按船期当前规则为最新 ETA 建立待发送提醒。
// 已经发送过的同版本、同天数提醒由数据库唯一约束保护，不会重复发送。
func createConfiguredArrivalReminders(ctx context.Context, q *store.Queries, tenantID, scheduleID, destinationNodeID, employeeID int64, etaRevision int32, eta pgtype.Date) error {
	days, err := q.ListArrivalReminderRules(ctx, store.ListArrivalReminderRulesParams{TenantID: tenantID, ScheduleID: scheduleID})
	if err != nil {
		return err
	}
	for _, day := range days {
		if err = q.CreateArrivalReminder(ctx, store.CreateArrivalReminderParams{
			TenantID: tenantID, ScheduleID: scheduleID, DestinationNodeID: destinationNodeID,
			RecipientEmployeeID: employeeID, EtaRevision: etaRevision, TargetEta: eta, LeadDays: day,
		}); err != nil {
			return err
		}
	}
	return nil
}

// GetArrivalReminderRules 返回某条船期当前启用的全部提前天数。
func (s *Service) GetArrivalReminderRules(ctx context.Context, tenantID, scheduleID int64) ([]int32, error) {
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
		nodes, err := q.ListRouteNodes(ctx, store.ListRouteNodesParams{TenantID: tenantID, ScheduleID: scheduleID})
		if err != nil {
			return err
		}
		var destinationID int64
		for _, node := range nodes {
			if node.NodeType == "DESTINATION" && node.IsActive {
				destinationID = node.ID
				break
			}
		}
		if destinationID == 0 {
			return apierr.Conflict("SHIPPING_DESTINATION_MISSING", "船期缺少目的港节点")
		}
		if err = createConfiguredArrivalReminders(ctx, q, tenantID, scheduleID, destinationID, current.ResponsibleEmployeeID, current.EtaRevision, current.Eta); err != nil {
			return err
		}
		toText := func(values []int32) string {
			parts := make([]string, len(values))
			for i, value := range values {
				parts[i] = strconv.Itoa(int(value))
			}
			return strings.Join(parts, ",")
		}
		return addChange(ctx, q, tenantID, scheduleID, "REMINDER", "arrival_reminder_days", toText(oldDays), toText(normalized), "修改到港提醒设置", op)
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

func arrivalReminderText(r store.GetDueArrivalReminderForUpdateRow) (string, string, string) {
	value := func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "—"
		}
		return strings.TrimSpace(v)
	}
	title := "船期将在 7 天内到港"
	content := fmt.Sprintf("船期编号：%s；合同编号：%s；客户：%s；船名/航次：%s / %s；目的港：%s；最新 ETA：%s",
		value(r.ScheduleNo), value(r.ContractNo), value(r.CustomerName), value(r.VesselName),
		value(r.VoyageNo), value(r.PortOfDischarge), dateText(r.TargetEta))
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
			title, content, link := arrivalReminderText(candidate)
			notification, getErr = q.MarkArrivalReminderSent(ctx, store.MarkArrivalReminderSentParams{
				ID: id, Title: title, Content: content, DetailUrl: link,
			})
			if getErr != nil {
				return getErr
			}
			return addChange(ctx, q, candidate.TenantID, candidate.ScheduleID, "REMINDER", "arrival_7d", "", title,
				"系统按最新 ETA 生成到港提醒", Operator{Name: "系统提醒任务"})
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
		return store.ShippingArrivalReminder{}, apierr.NotFound("SHIPPING_REMINDER_NOT_FOUND", "到港提醒不存在")
	}
	return row, err
}
