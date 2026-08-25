// 死信运维面：列出被放弃的事件、把一条重放一遍。
//
// 网关按平台操作员名单守门后转发到这里；服务自身不再验身份——内部调用都带
// 签名，能伪造签名的人已经不需要这个入口了。没装 Console 时明确拒绝，
// 不装死也不装聋。
package grpcin

import (
	"context"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/deadletter"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// UseDeadLetterConsole 装上死信面。构造时装不进来（main 里消费者建在
// handler 之后），用不带默认值的显式一步。
func (h *OrderHandler) UseDeadLetterConsole(c *deadletter.Console) { h.dead = c }

func (h *OrderHandler) ListFailedEvents(ctx context.Context, _ *prv1.ListFailedEventsRequest) (*prv1.ListFailedEventsResponse, error) {
	if h.dead == nil {
		return nil, apierr.Internal("DL_NOT_WIRED", "死信面未接线")
	}
	rows, err := h.dead.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*commonv1.FailedEvent, 0, len(rows))
	for _, p := range rows {
		out = append(out, &commonv1.FailedEvent{
			Id: p.ID, TenantId: p.TenantID, EventKey: p.EventKey,
			ConsumerGroup: p.ConsumerGroup, Topic: p.Topic,
			EventType: p.EventType, AggregateId: p.AggregateID,
			Reason: p.Reason, ParkedAt: p.ParkedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return &prv1.ListFailedEventsResponse{Events: out}, nil
}

func (h *OrderHandler) ReplayFailedEvent(ctx context.Context, req *prv1.ReplayFailedEventRequest) (*prv1.ReplayFailedEventResponse, error) {
	if h.dead == nil {
		return nil, apierr.Internal("DL_NOT_WIRED", "死信面未接线")
	}
	if err := h.dead.Replay(ctx, req.GetConsumerGroup(), req.GetId()); err != nil {
		// 原因原样带回：点重放的人正看着屏幕，这是把错误交到人手上的时机。
		return nil, apierr.Invalid("DL_REPLAY_FAILED", err.Error())
	}
	return &prv1.ReplayFailedEventResponse{}, nil
}
