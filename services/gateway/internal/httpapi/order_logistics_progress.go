package httpapi

import (
	"net/http"
	"strconv"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Only operational progress for cargo belonging to this authorized PO is
// exposed here. Shipping prices, supplier contracts and attachments stay out.
type orderLogisticsProgress struct {
	ID              string `json:"id"`
	ContractNo      string `json:"contractNo"`
	BatchNo         int32  `json:"batchNo"`
	Status          string `json:"status"`
	ScheduleID      string `json:"scheduleId"`
	ScheduleNo      string `json:"scheduleNo"`
	ScheduleStatus  string `json:"scheduleStatus"`
	PortOfLoading   string `json:"portOfLoading"`
	PortOfDischarge string `json:"portOfDischarge"`
	ETD             string `json:"etd"`
	ETA             string `json:"eta"`
	ATD             string `json:"atd"`
	ATA             string `json:"ata"`
}

func (s *Server) getOrderLogisticsProgress(w http.ResponseWriter, r *http.Request) {
	// GetOrder enforces tenant and order data scope before any shipping lookup.
	order, err := s.Orders.GetOrder(r.Context(), &prv1.GetOrderRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	refs := map[int64]map[int64]bool{}
	requirements := map[int64]bool{}
	for _, item := range order.GetItems() {
		id := item.GetRequirementId()
		if id <= 0 || requirements[id] {
			continue
		}
		requirements[id] = true
		req, e := s.Requirements.GetRequirement(r.Context(), &prv1.GetRequirementRequest{Id: id})
		if e != nil {
			s.writeGRPCError(w, e)
			return
		}
		source := req.GetRequirement()
		if source.GetContractId() <= 0 || source.GetContractItemId() <= 0 {
			continue
		}
		if refs[source.GetContractId()] == nil {
			refs[source.GetContractId()] = map[int64]bool{}
		}
		refs[source.GetContractId()][source.GetContractItemId()] = true
	}
	out := []orderLogisticsProgress{}
	if len(refs) == 0 {
		s.writeJSON(w, map[string]any{"items": out})
		return
	}
	list, e := s.Shipping.ListContractShippingHandoffs(r.Context(), &shippingv1.ListContractShippingHandoffsRequest{})
	if e != nil {
		s.writeGRPCError(w, e)
		return
	}
	for _, brief := range list.GetHandoffs() {
		lines := refs[brief.GetContractId()]
		if len(lines) == 0 {
			continue
		}
		// The list snapshot omits cargo. Resolve only related contracts and match
		// exact contract line IDs, rather than associating every PO on a contract.
		detail, e := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: brief.GetId()})
		if e != nil {
			s.writeGRPCError(w, e)
			return
		}
		handoff := detail.GetHandoff()
		matched := false
		if handoff.GetContractId() == brief.GetContractId() {
			for _, cargo := range handoff.GetCargoItems() {
				if lines[cargo.GetContractItemId()] {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}
		row := orderLogisticsProgress{ID: strconv.FormatInt(handoff.GetId(), 10), ContractNo: handoff.GetContractNo(), BatchNo: handoff.GetBatchNo(), Status: handoff.GetStatus(), ScheduleID: strconv.FormatInt(handoff.GetScheduleId(), 10), PortOfLoading: handoff.GetPortOfLoading(), PortOfDischarge: handoff.GetPortOfDischarge(), ETD: handoff.GetFinalEtd(), ETA: handoff.GetFinalEta()}
		if row.ETD == "" {
			row.ETD = handoff.GetEstimatedDeparture()
		}
		if row.ETA == "" {
			row.ETA = handoff.GetEstimatedArrival()
		}
		if handoff.GetScheduleId() > 0 {
			itemIDs := make([]int64, 0, len(lines))
			for id := range lines {
				itemIDs = append(itemIDs, id)
			}
			d, e := s.Shipping.GetCargoScheduleProgress(r.Context(), &shippingv1.GetCargoScheduleProgressRequest{ScheduleId: handoff.GetScheduleId(), ContractId: handoff.GetContractId(), ContractItemIds: itemIDs})
			if e != nil {
				s.writeGRPCError(w, e)
				return
			}
			schedule := d.GetSchedule()
			if schedule.GetContractId() != handoff.GetContractId() || schedule.GetId() != handoff.GetScheduleId() {
				s.writeGRPCError(w, status.Error(codes.FailedPrecondition, "关联船期与合同不一致"))
				return
			}
			row.ScheduleNo = schedule.GetScheduleNo()
			row.ScheduleStatus = schedule.GetStatus()
			row.ETD = schedule.GetEtd()
			row.ETA = schedule.GetEta()
			row.ATD = schedule.GetAtd()
			row.ATA = schedule.GetAta()
		}
		out = append(out, row)
	}
	s.writeJSON(w, map[string]any{"items": out})
}
