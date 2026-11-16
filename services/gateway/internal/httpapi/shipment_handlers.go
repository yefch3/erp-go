package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
)

func (s *Server) listShipments(w http.ResponseWriter, r *http.Request) {
	contractID, _ := strconv.ParseInt(r.URL.Query().Get("contract_id"), 10, 64)
	resp, err := s.Shipments.ListShipments(r.Context(), &exv1.ListShipmentsRequest{
		Page:       pageFromQuery(r),
		Status:     r.URL.Query().Get("status"),
		Keyword:    r.URL.Query().Get("keyword"),
		ContractId: contractID,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShipment(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.GetShipment(r.Context(), &exv1.GetShipmentRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createShipment(w http.ResponseWriter, r *http.Request) {
	req := &exv1.CreateShipmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Shipments.CreateShipment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShipment(w http.ResponseWriter, r *http.Request) {
	req := &exv1.UpdateShipmentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipments.UpdateShipment(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmSailing(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.ConfirmSailing(r.Context(), &exv1.ConfirmSailingRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markShipmentArrived(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.MarkArrived(r.Context(), &exv1.MarkArrivedRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelShipment(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipments.CancelShipment(r.Context(), &exv1.CancelShipmentRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listContractVessels(w http.ResponseWriter, r *http.Request) {
	contractID := idFromPath(r)
	// This endpoint is reached from the contract drawer and now uses the
	// contract-read permission. Re-run the contract service's ownership/data
	// scope check before returning shipment facts, otherwise knowing a contract
	// id would be enough to read another sales owner's vessel information.
	if _, err := s.Contracts.GetContract(r.Context(), &exv1.GetContractRequest{Id: contractID}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Shipments.ListContractVessels(r.Context(),
		&exv1.ListContractVesselsRequest{ContractId: contractID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// The old shipment records do not include formal shipping schedules.
	// Contract visibility is checked above; each schedule is also checked by
	// shipping before exposing its operational snapshot, without freight costs.
	handoffs, err := s.Shipping.ListContractShippingHandoffs(r.Context(), &shippingv1.ListContractShippingHandoffsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	schedules := make([]contractShippingSchedule, 0)
	for _, h := range handoffs.GetHandoffs() {
		if h.GetContractId() != contractID || h.GetScheduleId() == 0 {
			continue
		}
		detail, err := s.Shipping.GetSchedule(r.Context(), &shippingv1.GetScheduleRequest{Id: h.GetScheduleId()})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		v := detail.GetSchedule()
		if v.GetContractId() != contractID {
			continue
		}
		handoffDetail, err := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: h.GetId()})
		if err != nil {
			s.writeGRPCError(w, err)
			return
		}
		cargoHandoff := handoffDetail.GetHandoff()
		if cargoHandoff.GetContractId() != contractID || cargoHandoff.GetScheduleId() != v.GetId() {
			continue
		}
		row := contractShippingSchedule{ID: strconv.FormatInt(v.GetId(), 10), ScheduleNo: v.GetScheduleNo(), BatchNo: h.GetBatchNo(), VesselName: v.GetVesselName(), VoyageNo: v.GetVoyageNo(), CarrierForwarder: v.GetCarrierForwarder(), BookingNo: v.GetBookingNo(), BillOfLadingNo: v.GetBillOfLadingNo(), PortOfLoading: v.GetPortOfLoading(), PortOfDischarge: v.GetPortOfDischarge(), ETD: v.GetEtd(), ETA: v.GetEta(), ATD: v.GetAtd(), ATA: v.GetAta(), Status: v.GetStatus(), CargoItems: make([]contractShippingCargo, 0)}
		for _, item := range cargoHandoff.GetCargoItems() {
			row.CargoItems = append(row.CargoItems, contractShippingCargo{ProductName: item.GetProductName(), Specification: item.GetSpecification(), Quantity: item.GetQuantity(), UomCode: item.GetUomCode()})
		}
		schedules = append(schedules, row)
	}
	sort.Slice(schedules, func(i, j int) bool { return schedules[i].BatchNo < schedules[j].BatchNo })
	data, err := pj.Marshal(resp)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_MARSHAL", "响应编码失败")
		return
	}
	var legacy map[string]json.RawMessage
	if err = json.Unmarshal(data, &legacy); err != nil {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_MARSHAL", "响应编码失败")
		return
	}
	s.writeJSON(w, map[string]any{"vessels": legacy["vessels"], "schedules": schedules})
}

type contractShippingCargo struct {
	ProductName   string `json:"productName"`
	Specification string `json:"specification"`
	Quantity      string `json:"quantity"`
	UomCode       string `json:"uomCode"`
}

type contractShippingSchedule struct {
	ID               string                  `json:"id"`
	ScheduleNo       string                  `json:"scheduleNo"`
	BatchNo          int32                   `json:"batchNo"`
	VesselName       string                  `json:"vesselName"`
	VoyageNo         string                  `json:"voyageNo"`
	CarrierForwarder string                  `json:"carrierForwarder"`
	BookingNo        string                  `json:"bookingNo"`
	BillOfLadingNo   string                  `json:"billOfLadingNo"`
	PortOfLoading    string                  `json:"portOfLoading"`
	PortOfDischarge  string                  `json:"portOfDischarge"`
	ETD              string                  `json:"etd"`
	ETA              string                  `json:"eta"`
	ATD              string                  `json:"atd"`
	ATA              string                  `json:"ata"`
	Status           string                  `json:"status"`
	CargoItems       []contractShippingCargo `json:"cargoItems"`
}
