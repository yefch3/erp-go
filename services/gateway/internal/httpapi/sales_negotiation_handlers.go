package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
)

func (s *Server) createSalesPlan(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSalesPlanRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateSalesPlan(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSalesPlans(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListSalesPlans(r.Context(), &prv1.ListSalesPlansRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addCustomerFeedback(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AddCustomerFeedbackRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.AddCustomerFeedback(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listCustomerFeedback(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCustomerFeedback(r.Context(), &prv1.ListCustomerFeedbackRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSalesProcurementRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSalesProcurementReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateSalesProcurementRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createShippingRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateShippingReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateShippingRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippingReworks(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListShippingReworks(r.Context(), &prv1.ListShippingReworksRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listMyShippingReworks(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListMyShippingReworks(r.Context(), &prv1.ListMyShippingReworksRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) resolveShippingRework(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ResolveShippingReworkRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Sourcing.ResolveShippingRework(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) confirmCustomerSelection(w http.ResponseWriter, r *http.Request) {
	req := &prv1.ConfirmCustomerSelectionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.ConfirmCustomerSelection(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listCustomerSelections(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.ListCustomerSelections(r.Context(), &prv1.ListCustomerSelectionsRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) decideCustomerSelection(w http.ResponseWriter, r *http.Request) {
	req := &prv1.DecideCustomerSelectionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.DecideCustomerSelection(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

// createFormalQuotationFromSelection is the T8 -> T9 handoff. The customer
// accepted snapshot is immutable, so this endpoint never accepts prices from
// the browser and is safe to retry.
func (s *Server) createFormalQuotationFromSelection(w http.ResponseWriter, r *http.Request) {
	caseID := idFromPath(r)
	selectionID, err := strconv.ParseInt(chi.URLParam(r, "selectionId"), 10, 64)
	if err != nil || selectionID <= 0 {
		s.writeError(w, http.StatusBadRequest, "SC_SELECTION_ID", "客户选择版本无效")
		return
	}
	caseResp, err := s.Sourcing.GetCase(r.Context(), &prv1.GetCaseRequest{Id: caseID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	list, err := s.Sourcing.ListCustomerSelections(r.Context(), &prv1.ListCustomerSelectionsRequest{CaseId: caseID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	var selected *prv1.CustomerSelection
	for _, candidate := range list.GetSelections() {
		if candidate.GetId() == selectionID {
			selected = candidate
			break
		}
	}
	if selected == nil {
		s.writeError(w, http.StatusNotFound, "SC_SELECTION_NOT_FOUND", "客户选择版本不存在")
		return
	}
	if selected.GetStatus() != "CUSTOMER_CONFIRMED" {
		s.writeError(w, http.StatusConflict, "SC_SELECTION_NOT_FINAL", "只有最终复询完成且客户已接受的当前选择才能生成正式报价")
		return
	}
	currency := ""
	items := make([]*exv1.ItemInput, 0, len(selected.GetItems()))
	shipments := make([]*exv1.QuotationShipmentInput, 0, len(selected.GetShipments()))
	customerManagedGroups := map[string]bool{}
	incoterms, payments := map[string]bool{}, map[string]bool{}
	validUntil := ""
	shippingRechecks := map[int64]*prv1.FinalRecheckTask{}
	for _, task := range selected.GetRecheckTasks() {
		if task.GetStatus() != "RESOLVED" || task.GetFinalValidUntil() == "" {
			s.writeError(w, http.StatusConflict, "SC_FINAL_RECHECK_INCOMPLETE", "最终复询资料尚未完整，不能生成正式报价")
			return
		}
		if validUntil == "" || task.GetFinalValidUntil() < validUntil {
			validUntil = task.GetFinalValidUntil()
		}
		if task.GetTaskDomain() == "SHIPPING" && task.GetSelectionShipmentId() != 0 {
			shippingRechecks[task.GetSelectionShipmentId()] = task
		}
	}
	for _, item := range selected.GetItems() {
		if item.GetFinalCustomerCurrency() == "" || item.GetFinalCustomerUnitPrice() == "" || item.GetConfirmedQty() == "" {
			s.writeError(w, http.StatusConflict, "SC_FINAL_TERMS_INCOMPLETE", "客户最终产品价格或数量不完整")
			return
		}
		if currency == "" {
			currency = item.GetFinalCustomerCurrency()
		}
		if currency != item.GetFinalCustomerCurrency() {
			s.writeError(w, http.StatusConflict, "SC_FORMAL_QUOTE_CURRENCY", "一张正式报价只能使用一种对客币种")
			return
		}
		incoterms[item.GetFinalCustomerIncoterm()] = true
		payments[item.GetFinalCustomerPaymentTerms()] = true
		remark := fmt.Sprintf("供应商：%s；付款条件：%s；贸易条款：%s；客户要求日期：%s", item.GetSupplierName(), item.GetFinalCustomerPaymentTerms(), item.GetFinalCustomerIncoterm(), item.GetFinalCustomerRequiredDate())
		spec := strings.Trim(strings.Join([]string{"供应商 " + item.GetSupplierName(), item.GetProductSpec(), "付款 " + item.GetFinalCustomerPaymentTerms(), "贸易条款 " + item.GetFinalCustomerIncoterm(), "要求日期 " + item.GetFinalCustomerRequiredDate()}, "；"), "；")
		items = append(items, &exv1.ItemInput{ProductName: item.GetProductName(), Spec: spec, UomCode: item.GetUomCode(), Qty: item.GetConfirmedQty(), UnitPrice: item.GetFinalCustomerUnitPrice(), Remark: remark})
		if item.GetCustomerManagedShipping() {
			customerManagedGroups[item.GetShipmentGroupKey()] = true
		}
	}
	portsFrom, portsTo := map[string]bool{}, map[string]bool{}
	for _, shipment := range selected.GetShipments() {
		if shipment.GetFinalCustomerCurrency() == "" || shipment.GetFinalCustomerFreightAmount() == "" {
			s.writeError(w, http.StatusConflict, "SC_FINAL_FREIGHT_INCOMPLETE", "客户最终船运价格不完整")
			return
		}
		if currency == "" {
			currency = shipment.GetFinalCustomerCurrency()
		}
		if currency != shipment.GetFinalCustomerCurrency() {
			s.writeError(w, http.StatusConflict, "SC_FORMAL_QUOTE_CURRENCY", "产品和船运对客价格必须使用同一币种")
			return
		}
		portsFrom[shipment.GetPortOfLoading()], portsTo[shipment.GetPortOfDischarge()] = true, true
		departure, arrival := shipment.GetEstimatedDeparture(), shipment.GetEstimatedArrival()
		if task := shippingRechecks[shipment.GetId()]; task != nil {
			departure, arrival = task.GetFinalEstimatedDeparture(), task.GetFinalEstimatedArrival()
		}
		shipments = append(shipments, &exv1.QuotationShipmentInput{
			SourceCustomerSelectionShipmentId: shipment.GetId(), ShipmentGroupKey: shipment.GetShipmentGroupKey(),
			CarrierForwarder: shipment.GetCarrierForwarder(), ServiceOptionName: shipment.GetServiceOptionName(),
			Currency: shipment.GetFinalCustomerCurrency(), FreightAmount: shipment.GetFinalCustomerFreightAmount(), ChargeBasis: shipment.GetChargeBasis(),
			PortOfLoading: shipment.GetPortOfLoading(), PortOfDischarge: shipment.GetPortOfDischarge(),
			EstimatedDeparture: departure, EstimatedArrival: arrival,
			ValidUntil: validUntil, Remark: shipment.GetCustomerNote(),
		})
	}
	for group := range customerManagedGroups {
		shipments = append(shipments, &exv1.QuotationShipmentInput{ShipmentGroupKey: group, CustomerManaged: true, Currency: currency, FreightAmount: "0", Remark: "客户自理运输 / Customer managed shipping"})
	}
	if currency == "" || len(items) == 0 {
		s.writeError(w, http.StatusConflict, "SC_FORMAL_QUOTE_EMPTY", "客户选择没有可生成报价的内容")
		return
	}
	caseRow := caseResp.GetSourcingCase()
	created, err := s.Quotations.CreateQuotation(r.Context(), &exv1.CreateQuotationRequest{
		CustomerId: caseRow.GetCustomerId(), ContactId: caseRow.GetContactId(), Currency: currency,
		Incoterm: singleValue(incoterms, "按产品明细 / Per line"), PaymentMethod: singleValue(payments, "按产品明细 / Per line"),
		PortOfLoading: singleValue(portsFrom, "多个起运港 / Multiple"), PortOfDischarge: singleValue(portsTo, "多个目的港 / Multiple"),
		ValidUntil: validUntil, Remark: fmt.Sprintf("来源询价 %s；客户选择 %s V%d；销售线下确认于 %s", caseRow.GetCaseNo(), selected.GetSelectionNo(), selected.GetVersionNo(), selected.GetCustomerDecidedAt()), Items: items,
		Shipments:            shipments,
		SourceSourcingCaseId: caseID, SourceCustomerSelectionId: selectionID, SourceCustomerSelectionNo: selected.GetSelectionNo(), SourceCustomerSelectionVersion: selected.GetVersionNo(),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, created)
}

func singleValue(values map[string]bool, fallback string) string {
	if len(values) != 1 {
		return fallback
	}
	for value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}
