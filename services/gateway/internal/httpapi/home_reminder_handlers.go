package httpapi

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

const (
	homeReminderReceivable     = "RECEIVABLE"
	homeReminderArrival        = "ARRIVAL"
	homeReminderBL             = "BL"
	homeReminderShippingAction = "SHIPPING_ACTION"
)

type homeReminderItem struct {
	Key       string `json:"key"`
	Source    string `json:"source"`
	SourceID  string `json:"sourceId"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	BizNo     string `json:"bizNo"`
	DueAt     string `json:"dueAt"`
	CreatedAt string `json:"createdAt"`
	Unread    bool   `json:"unread"`
	Timing    string `json:"timing"`
	DetailURL string `json:"detailUrl"`
}

type homeReminderSummary struct {
	Upcoming int64 `json:"upcoming"`
	Overdue  int64 `json:"overdue"`
	Unread   int64 `json:"unread"`
}

type homeReminderSourceState struct {
	Source    string `json:"source"`
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
}

type homeReminderPage struct {
	Items    []homeReminderItem        `json:"items"`
	Total    int                       `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
	Summary  homeReminderSummary       `json:"summary"`
	Sources  []homeReminderSourceState `json:"sources"`
}

// listHomeReminders 将现有应收、到港和提单收件箱合成个人提醒视图。
// 每个来源仍由自己的服务按租户和收件人隔离；网关只在当前员工拥有来源读取权限时读取。
func (s *Server) listHomeReminders(w http.ResponseWriter, r *http.Request) {
	today := time.Now().UTC()
	items := make([]homeReminderItem, 0, 64)
	summary := homeReminderSummary{}
	sources := make([]homeReminderSourceState, 0, 3)

	receivableAllowed, permissionErr := s.hasPermission(r, "export:receipt:read")
	if permissionErr != nil {
		sources = append(sources, homeReminderSourceState{Source: homeReminderReceivable, Message: "权限检查失败"})
	} else if receivableAllowed {
		resp, err := s.Receipts.ListReceivableReminders(r.Context(), &exv1.ListReceivableRemindersRequest{Limit: 100})
		if err != nil {
			sources = append(sources, homeReminderSourceState{Source: homeReminderReceivable, Message: "应收提醒暂时不可用"})
		} else {
			sources = append(sources, homeReminderSourceState{Source: homeReminderReceivable, Available: true})
			summary.Unread += resp.GetUnreadTotal()
			for _, row := range resp.GetItems() {
				timing := classifyHomeReminderDate(row.GetDueDate(), today)
				items = append(items, homeReminderItem{
					Key: homeReminderReceivable + ":" + strconv.FormatInt(row.GetId(), 10), Source: homeReminderReceivable,
					SourceID: strconv.FormatInt(row.GetId(), 10), Title: row.GetTitle(), Content: row.GetContent(),
					BizNo: row.GetContractNo(), DueAt: row.GetDueDate(), CreatedAt: row.GetCreatedAt(),
					Unread: row.GetUnread(), Timing: timing,
					DetailURL: receivableReminderURL(row.GetContractNo(), row.GetDetailUrl()),
				})
				addHomeReminderTiming(&summary, timing)
			}
		}
	}

	shippingAllowed, permissionErr := s.hasPermission(r, "shipping:schedule:read")
	if permissionErr != nil {
		sources = append(sources,
			homeReminderSourceState{Source: homeReminderArrival, Message: "权限检查失败"},
			homeReminderSourceState{Source: homeReminderBL, Message: "权限检查失败"},
		)
	} else if shippingAllowed {
		arrivalResp, err := s.Shipping.ListArrivalNotifications(r.Context(), &shippingv1.ListArrivalNotificationsRequest{})
		if err != nil {
			sources = append(sources, homeReminderSourceState{Source: homeReminderArrival, Message: "到港提醒暂时不可用"})
		} else {
			sources = append(sources, homeReminderSourceState{Source: homeReminderArrival, Available: true})
			summary.Unread += arrivalResp.GetUnreadCount()
			for _, row := range arrivalResp.GetReminders() {
				timing := classifyHomeReminderDate(row.GetTargetEta(), today)
				items = append(items, homeReminderItem{
					Key: homeReminderArrival + ":" + strconv.FormatInt(row.GetId(), 10), Source: homeReminderArrival,
					SourceID: strconv.FormatInt(row.GetId(), 10), Title: row.GetTitle(), Content: row.GetContent(),
					BizNo: strconv.FormatInt(row.GetScheduleId(), 10), DueAt: datePart(row.GetTargetEta()), CreatedAt: row.GetSentAt(),
					Unread: row.GetReadAt() == "", Timing: timing, DetailURL: row.GetDetailUrl(),
				})
				addHomeReminderTiming(&summary, timing)
			}
		}

		blResp, err := s.Shipping.ListBlReminders(r.Context(), &shippingv1.ListBlRemindersRequest{Limit: 100})
		if err != nil {
			sources = append(sources, homeReminderSourceState{Source: homeReminderBL, Message: "提单提醒暂时不可用"})
		} else {
			sources = append(sources, homeReminderSourceState{Source: homeReminderBL, Available: true})
			summary.Unread += blResp.GetUnreadTotal()
			for _, row := range blResp.GetItems() {
				dueAt := addDays(row.GetDepartedOn(), 3)
				timing := classifyHomeReminderDate(dueAt, today)
				items = append(items, homeReminderItem{
					Key: homeReminderBL + ":" + strconv.FormatInt(row.GetId(), 10), Source: homeReminderBL,
					SourceID: strconv.FormatInt(row.GetId(), 10), Title: row.GetTitle(), Content: row.GetContent(),
					BizNo: row.GetScheduleNo(), DueAt: dueAt, CreatedAt: row.GetCreatedAt(),
					Unread: row.GetUnread(), Timing: timing, DetailURL: row.GetDetailUrl(),
				})
				addHomeReminderTiming(&summary, timing)
			}
		}

	}

	// 运输协同待办是定向给当前员工的。采购收件人不需要获得整个船期模块的读取
	// 权限；shipping 服务按 tenant + employee 双重条件过滤，避免泄露其他船期资料。
	actionResp, err := s.Shipping.ListOperationalAlerts(r.Context(), &shippingv1.ListOperationalAlertsRequest{OpenOnly: true})
	if err != nil {
		sources = append(sources, homeReminderSourceState{Source: homeReminderShippingAction, Message: "运输协同待办暂时不可用"})
	} else {
		sources = append(sources, homeReminderSourceState{Source: homeReminderShippingAction, Available: true})
		summary.Unread += actionResp.GetUnreadCount()
		for _, row := range actionResp.GetItems() {
			timing := classifyHomeReminderDate(row.GetDueDate(), today)
			items = append(items, homeReminderItem{Key: homeReminderShippingAction + ":" + strconv.FormatInt(row.GetId(), 10), Source: homeReminderShippingAction, SourceID: strconv.FormatInt(row.GetId(), 10), Title: row.GetTitle(), Content: row.GetContent(), BizNo: row.GetScheduleNo(), DueAt: row.GetDueDate(), CreatedAt: row.GetCreatedAt(), Unread: row.GetReadAt() == "", Timing: timing})
			addHomeReminderTiming(&summary, timing)
		}
	}

	items = filterAndSortHomeReminders(items, r)
	page, pageSize := homeReminderPageParams(r)
	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	s.writeJSON(w, homeReminderPage{Items: items[start:end], Total: total, Page: page, PageSize: pageSize, Summary: summary, Sources: sources})
}

type markHomeRemindersReadInput struct {
	Source string   `json:"source"`
	IDs    []string `json:"ids"`
}

// markHomeRemindersRead 把首页的已读动作写回原提醒表，确保首页、顶部徽标和业务模块共享状态。
func (s *Server) markHomeRemindersRead(w http.ResponseWriter, r *http.Request) {
	input := markHomeRemindersReadInput{}
	if !s.decodeJSON(w, r, &input) {
		return
	}
	input.Source = strings.ToUpper(strings.TrimSpace(input.Source))
	ids, ok := parseHomeReminderIDs(input.IDs)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "HOME_REMINDER_ID_INVALID", "提醒编号不正确")
		return
	}

	marked := int64(0)
	sourceErrors := make([]homeReminderSourceState, 0, 3)
	markReceivable := input.Source == "" || input.Source == "ALL" || input.Source == homeReminderReceivable
	markShipping := input.Source == "" || input.Source == "ALL" || input.Source == homeReminderArrival || input.Source == homeReminderBL

	if markReceivable {
		allowed, err := s.hasPermission(r, "export:receipt:read")
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		if input.Source == homeReminderReceivable && !allowed {
			s.writeError(w, http.StatusForbidden, "AUTH_PERMISSION_DENIED", "没有查看应收提醒的权限")
			return
		}
		if allowed {
			resp, err := s.Receipts.MarkReceivableRemindersRead(r.Context(), &exv1.MarkReceivableRemindersReadRequest{Ids: ids})
			if err != nil {
				sourceErrors = append(sourceErrors, homeReminderSourceState{Source: homeReminderReceivable, Message: "应收提醒标记失败"})
			} else {
				marked += resp.GetMarked()
			}
		}
	}

	if markShipping {
		allowed, err := s.hasPermission(r, "shipping:schedule:read")
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		if (input.Source == homeReminderArrival || input.Source == homeReminderBL) && !allowed {
			s.writeError(w, http.StatusForbidden, "AUTH_PERMISSION_DENIED", "没有查看船期提醒的权限")
			return
		}
		if allowed && (input.Source == "" || input.Source == "ALL" || input.Source == homeReminderBL) {
			resp, err := s.Shipping.MarkBlRemindersRead(r.Context(), &shippingv1.MarkBlRemindersReadRequest{Ids: ids})
			if err != nil {
				sourceErrors = append(sourceErrors, homeReminderSourceState{Source: homeReminderBL, Message: "提单提醒标记失败"})
			} else {
				marked += resp.GetMarked()
			}
		}
		if allowed && (input.Source == "" || input.Source == "ALL" || input.Source == homeReminderArrival) {
			arrivalIDs := ids
			if len(arrivalIDs) == 0 {
				resp, err := s.Shipping.ListArrivalNotifications(r.Context(), &shippingv1.ListArrivalNotificationsRequest{UnreadOnly: true})
				if err != nil {
					sourceErrors = append(sourceErrors, homeReminderSourceState{Source: homeReminderArrival, Message: "到港提醒标记失败"})
					arrivalIDs = nil
				} else {
					arrivalIDs = make([]int64, 0, len(resp.GetReminders()))
					for _, item := range resp.GetReminders() {
						arrivalIDs = append(arrivalIDs, item.GetId())
					}
				}
			}
			for _, id := range arrivalIDs {
				if _, err := s.Shipping.MarkArrivalReminderRead(r.Context(), &shippingv1.MarkArrivalReminderReadRequest{ReminderId: id}); err != nil {
					sourceErrors = append(sourceErrors, homeReminderSourceState{Source: homeReminderArrival, Message: "到港提醒标记失败"})
					break
				}
				marked++
			}
		}
	}
	if input.Source == "" || input.Source == "ALL" || input.Source == homeReminderShippingAction {
		resp, err := s.Shipping.MarkOperationalAlertsRead(r.Context(), &shippingv1.MarkOperationalAlertsReadRequest{Ids: ids})
		if err != nil {
			sourceErrors = append(sourceErrors, homeReminderSourceState{Source: homeReminderShippingAction, Message: "运输协同待办标记失败"})
		} else {
			marked += resp.GetMarked()
		}
	}

	s.writeJSON(w, map[string]any{"marked": marked, "sources": sourceErrors})
}

func homeReminderPageParams(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 10
	}
	return page, pageSize
}

func filterAndSortHomeReminders(items []homeReminderItem, r *http.Request) []homeReminderItem {
	q := r.URL.Query()
	keyword := strings.ToLower(strings.TrimSpace(q.Get("keyword")))
	source := strings.ToUpper(strings.TrimSpace(q.Get("source")))
	readState := strings.ToUpper(strings.TrimSpace(q.Get("read")))
	timing := strings.ToUpper(strings.TrimSpace(q.Get("timing")))
	filtered := make([]homeReminderItem, 0, len(items))
	for _, item := range items {
		if source != "" && item.Source != source {
			continue
		}
		if readState == "UNREAD" && !item.Unread || readState == "READ" && item.Unread {
			continue
		}
		if timing != "" && item.Timing != timing {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Content+" "+item.BizNo), keyword) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := homeReminderTimingRank(filtered[i].Timing), homeReminderTimingRank(filtered[j].Timing)
		if left != right {
			return left < right
		}
		if filtered[i].Unread != filtered[j].Unread {
			return filtered[i].Unread
		}
		if filtered[i].DueAt != filtered[j].DueAt {
			return filtered[i].DueAt < filtered[j].DueAt
		}
		return filtered[i].CreatedAt > filtered[j].CreatedAt
	})
	return filtered
}

func classifyHomeReminderDate(value string, now time.Time) string {
	value = datePart(value)
	due, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "REMINDER"
	}
	today, _ := time.Parse("2006-01-02", now.UTC().Format("2006-01-02"))
	days := int(due.Sub(today).Hours() / 24)
	if days < 0 {
		return "OVERDUE"
	}
	if days <= 3 {
		return "UPCOMING"
	}
	return "REMINDER"
}

func addHomeReminderTiming(summary *homeReminderSummary, timing string) {
	if timing == "OVERDUE" {
		summary.Overdue++
	}
	if timing == "UPCOMING" {
		summary.Upcoming++
	}
}

func homeReminderTimingRank(timing string) int {
	switch timing {
	case "OVERDUE":
		return 0
	case "UPCOMING":
		return 1
	default:
		return 2
	}
}

func datePart(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func addDays(value string, days int) string {
	date, err := time.Parse("2006-01-02", datePart(value))
	if err != nil {
		return ""
	}
	return date.AddDate(0, 0, days).Format("2006-01-02")
}

func receivableReminderURL(contractNo, fallback string) string {
	if contractNo != "" {
		return "/receivable-due?keyword=" + url.QueryEscape(contractNo)
	}
	if fallback != "" {
		return fallback
	}
	return "/receivable-due"
}

func parseHomeReminderIDs(values []string) ([]int64, bool) {
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}
