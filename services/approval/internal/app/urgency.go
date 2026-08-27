package app

import "time"

const (
	approvalTaskSLA      = 72 * time.Hour
	approvalUrgentWindow = 24 * time.Hour
	approvalHighWindow   = 48 * time.Hour
	priorityOverdue      = "OVERDUE"
	priorityUrgent       = "URGENT"
	priorityHigh         = "HIGH"
	priorityNormal       = "NORMAL"
)

// ApprovalUrgency 按统一的 72 小时审批时限计算截止时间和紧急等级。
// 该计算放在服务端，避免不同浏览器的时间、时区或页面实现产生不同结果。
func ApprovalUrgency(createdAt, now time.Time) (dueAt time.Time, priority string, remainingMinutes int64) {
	dueAt = createdAt.Add(approvalTaskSLA)
	remaining := dueAt.Sub(now)
	remainingMinutes = int64(remaining / time.Minute)

	switch {
	case !now.Before(dueAt):
		priority = priorityOverdue
	case remaining <= approvalUrgentWindow:
		priority = priorityUrgent
	case remaining <= approvalHighWindow:
		priority = priorityHigh
	default:
		priority = priorityNormal
	}
	return dueAt, priority, remainingMinutes
}
