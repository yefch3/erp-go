package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	ivv1 "github.com/sgao19/erp-go/gen/go/erp/inventory/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

// 死信运维面：三个消费事件的服务各有一张「放弃了的事件」表，这里汇成一页。
//
// 平台操作员专用。这不是任何一家公司的业务数据——它回答的问题是「哪家公司
// 正在丢事件」，和平台开户页同一个受众，所以守的也是同一道门（操作员名单，
// 判定在 iam）。不能用 s.perm：每家公司的超管都有全部权限，用权限当门等于
// 向所有客户公司敞开。
func (s *Server) requirePlatformOperator(w http.ResponseWriter, r *http.Request) bool {
	resp, err := s.Platform.CheckOperator(r.Context(), &iamv1.CheckOperatorRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return false
	}
	if !resp.GetOperator() {
		s.writeError(w, http.StatusForbidden, "IAM_PLATFORM_ONLY", "平台操作员专用")
		return false
	}
	return true
}

// failedEventView 是一条死信在页面上的样子，多带一个「哪个服务」。
type failedEventView struct {
	Service       string `json:"service"`
	ID            int64  `json:"id"`
	TenantID      int64  `json:"tenantId"`
	ConsumerGroup string `json:"consumerGroup"`
	EventType     string `json:"eventType"`
	AggregateID   string `json:"aggregateId"`
	Reason        string `json:"reason"`
	ParkedAt      string `json:"parkedAt"`
}

func (s *Server) listFailedEvents(w http.ResponseWriter, r *http.Request) {
	if !s.requirePlatformOperator(w, r) {
		return
	}
	out := []failedEventView{}
	collect := func(service string, events []*commonv1.FailedEvent, err error) {
		// 一个服务答不上来不该黑掉整页：剩下两个服务的死信仍然要给人看。
		// 但也不能装没事——用一条假事件把故障本身放进列表里。
		if err != nil {
			out = append(out, failedEventView{
				Service: service, Reason: "该服务的死信列表暂时读不到：" + err.Error(),
			})
			return
		}
		for _, e := range events {
			out = append(out, failedEventView{
				Service: service, ID: e.GetId(), TenantID: e.GetTenantId(),
				ConsumerGroup: e.GetConsumerGroup(), EventType: e.GetEventType(),
				AggregateID: e.GetAggregateId(), Reason: e.GetReason(),
				ParkedAt: e.GetParkedAt(),
			})
		}
	}
	iv, err := s.Stocks.ListFailedEvents(r.Context(), &ivv1.ListFailedEventsRequest{})
	collect("inventory", iv.GetEvents(), err)
	pr, err := s.Orders.ListFailedEvents(r.Context(), &prv1.ListFailedEventsRequest{})
	collect("procurement", pr.GetEvents(), err)
	ex, err := s.Contracts.ListFailedEvents(r.Context(), &exv1.ListFailedEventsRequest{})
	collect("export", ex.GetEvents(), err)

	s.writeJSON(w, map[string]any{"events": out})
}

func (s *Server) replayFailedEvent(w http.ResponseWriter, r *http.Request) {
	if !s.requirePlatformOperator(w, r) {
		return
	}
	var input struct {
		Service       string `json:"service"`
		ConsumerGroup string `json:"consumer_group"`
		ID            int64  `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_JSON", "请求格式不正确")
		return
	}
	if input.ID <= 0 || input.ConsumerGroup == "" {
		s.writeError(w, http.StatusBadRequest, "DL_REPLAY_ARGS", "缺少事件编号或消费组")
		return
	}
	var err error
	switch input.Service {
	case "inventory":
		_, err = s.Stocks.ReplayFailedEvent(r.Context(), &ivv1.ReplayFailedEventRequest{
			ConsumerGroup: input.ConsumerGroup, Id: input.ID,
		})
	case "procurement":
		_, err = s.Orders.ReplayFailedEvent(r.Context(), &prv1.ReplayFailedEventRequest{
			ConsumerGroup: input.ConsumerGroup, Id: input.ID,
		})
	case "export":
		_, err = s.Contracts.ReplayFailedEvent(r.Context(), &exv1.ReplayFailedEventRequest{
			ConsumerGroup: input.ConsumerGroup, Id: input.ID,
		})
	default:
		s.writeError(w, http.StatusBadRequest, "DL_REPLAY_ARGS", "未知的服务："+input.Service)
		return
	}
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeJSON(w, map[string]any{"replayed": strconv.FormatInt(input.ID, 10)})
}
