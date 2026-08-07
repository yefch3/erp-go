package grpcout

import (
	"context"
	"fmt"

	"github.com/sgao19/erp-go/pkg/livefeed"
)

// ReminderNotifier 将已持久化的船期通知转换为轻量级浏览器刷新提示。
// Redis 只负责提示，永远不是通知数据的真实来源。
type ReminderNotifier struct{ publisher *livefeed.Publisher }

func NewReminderNotifier(publisher *livefeed.Publisher) *ReminderNotifier {
	return &ReminderNotifier{publisher: publisher}
}

func (n *ReminderNotifier) NotifyArrivalReminder(ctx context.Context, tenantID, employeeID, scheduleID int64) {
	if n == nil || n.publisher == nil {
		return
	}
	n.publisher.ToEmployees(ctx, tenantID, []int64{employeeID}, livefeed.Event{
		Type: livefeed.ShippingArrivalReminder, Subject: fmt.Sprintf("SHIPPING:%d", scheduleID),
	})
}
