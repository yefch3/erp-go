package grpcin

import (
	"context"
	"strings"

	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
)

func (h *Handler) enrichScheduleIdentity(ctx context.Context, schedules []*shippingv1.Schedule, arrivals []*shippingv1.ArrivalReminder, bills []*shippingv1.BlReminder, alerts []*shippingv1.OperationalAlert) error {
	ids := []int64{}
	for _, v := range schedules {
		ids = append(ids, v.Id)
	}
	for _, v := range arrivals {
		ids = append(ids, v.ScheduleId)
	}
	for _, v := range bills {
		ids = append(ids, v.ScheduleId)
	}
	for _, v := range alerts {
		ids = append(ids, v.ScheduleId)
	}
	identities, err := h.svc.ScheduleIdentities(ctx, grpcx.TenantID(ctx), ids)
	if err != nil {
		return err
	}
	for _, v := range schedules {
		if identity, ok := identities[v.Id]; ok {
			v.ContractNo = identity.ContractNo
			v.TransportBatchNo = identity.BatchNo
		}
	}
	for _, v := range arrivals {
		if identity, ok := identities[v.ScheduleId]; ok {
			v.ContractNo = identity.ContractNo
			v.ScheduleNo = identity.ScheduleNo
			v.TransportBatchNo = identity.BatchNo
			v.Content = identity.ReminderContent(v.Content)
		}
	}
	for _, v := range bills {
		if identity, ok := identities[v.ScheduleId]; ok {
			v.ContractNo = identity.ContractNo
			v.TransportBatchNo = identity.BatchNo
			v.Content = identity.ReminderContent(v.Content)
			if identity.ScheduleNo != "" {
				v.Title = strings.ReplaceAll(v.Title, identity.ScheduleNo, identity.Label())
			}
		}
	}
	for _, v := range alerts {
		if identity, ok := identities[v.ScheduleId]; ok {
			v.ContractNo = identity.ContractNo
			v.TransportBatchNo = identity.BatchNo
			v.Content = identity.ReminderContent(v.Content)
		}
	}
	return nil
}
