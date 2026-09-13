package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// portSnapshotName 生成船期使用的港口名称快照；中文名缺失时回退到英文名。
func portSnapshotName(port *mdv1.Port) string {
	if name := strings.TrimSpace(port.GetNameZh()); name != "" {
		return name
	}
	return strings.TrimSpace(port.GetNameEn())
}

// resolveActivePort 校验新选择的港口仍处于启用状态，并返回权威主数据。
func (s *Server) resolveActivePort(r *http.Request, id int64) (*mdv1.Port, error) {
	if id <= 0 {
		return nil, apierr.Invalid("SHIPPING_PORT_REQUIRED", "请选择有效的港口")
	}
	resp, err := s.Ports.GetPort(r.Context(), &mdv1.GetPortRequest{Id: id})
	if err != nil {
		return nil, err
	}
	port := resp.GetPort()
	if port.GetStatus() != "ACTIVE" {
		return nil, apierr.Conflict("SHIPPING_PORT_INACTIVE", "已停用港口不能用于新的船期或路线")
	}
	return port, nil
}

// resolveShippingMasterdata 以主数据 ID 为准，并把当时名称、代码和时区保存为历史快照。
func (s *Server) resolveShippingMasterdata(r *http.Request, in *shippingv1.ScheduleInput, requirePorts bool) error {
	if in == nil {
		return nil
	}
	// 从已委托合同建立船期时，客户和承运方由 shipping 服务在事务内读取
	// 委托记录并覆盖请求值。历史合同保存的是业务快照，即使对应主数据后来
	// 停用或迁移，也不能阻断履约；这里仅校验本次重新选择的港口。
	fromHandoff := in.GetContractHandoffId() > 0
	if !fromHandoff && in.GetCustomerId() > 0 {
		customer, err := s.resolveActiveCustomer(r.Context(), in.GetCustomerId())
		if err != nil {
			return err
		}
		in.CustomerName = customer.GetName()
	}
	if !fromHandoff && in.GetCarrierId() > 0 {
		supplier, err := s.resolveActiveSupplier(r.Context(), in.GetCarrierId())
		if err != nil {
			return err
		}
		// 船期的承运方必须真是船公司或货代（B3）：主数据里业务类型是
		// 唯一的角色事实，没勾就是不是——选一家钢厂当船公司应当被拒，
		// 而不是等年底延误率报表统计出一家轧钢厂。
		if !supplierHasRole(supplier, "CARRIER", "FORWARDER") {
			return apierr.Invalid("SHIPPING_CARRIER_ROLE",
				"所选公司不是船公司或货代——请先在供应商主数据里为它勾选相应业务类型")
		}
		in.CarrierForwarder = supplier.GetName()
	}
	if requirePorts || in.GetLoadingPortId() > 0 {
		loading, err := s.resolveActivePort(r, in.GetLoadingPortId())
		if err != nil {
			return err
		}
		in.PortOfLoading = portSnapshotName(loading)
		in.LoadingPortCode = loading.GetUnlocode()
		in.LoadingPortTimezone = loading.GetTimezone()
	}
	if requirePorts || in.GetDischargePortId() > 0 {
		discharge, err := s.resolveActivePort(r, in.GetDischargePortId())
		if err != nil {
			return err
		}
		in.PortOfDischarge = portSnapshotName(discharge)
		in.DischargePortCode = discharge.GetUnlocode()
		in.DischargePortTimezone = discharge.GetTimezone()
	}
	return nil
}

func (s *Server) getShippingStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetModuleStatus(r.Context(), &shippingv1.GetModuleStatusRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippingSchedules(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.Shipping.ListSchedules(r.Context(), &shippingv1.ListSchedulesRequest{
		Keyword: q.Get("keyword"), Status: q.Get("status"), PortOfLoading: q.Get("port_of_loading"), PortOfDischarge: q.Get("port_of_discharge"),
		EtdFrom: q.Get("etd_from"), EtdTo: q.Get("etd_to"), EtaFrom: q.Get("eta_from"), EtaTo: q.Get("eta_to"), Page: pageFromQuery(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listContractShippingHandoffs(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.ListContractShippingHandoffs(r.Context(), &shippingv1.ListContractShippingHandoffsRequest{Status: r.URL.Query().Get("status")})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getContractShippingHandoff(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) listContractShippingRequoteOptions(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.ListContractShippingRequoteOptions(r.Context(), &shippingv1.ListContractShippingRequoteOptionsRequest{HandoffId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) submitContractShippingRequote(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.SubmitContractShippingRequoteRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.SubmitContractShippingRequote(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) saveContractShippingRequoteDraft(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.SaveContractShippingRequoteDraftRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.SaveContractShippingRequoteDraft(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) deleteContractShippingRequoteOption(w http.ResponseWriter, r *http.Request) {
	optionID, _ := strconv.ParseInt(chi.URLParam(r, "optionID"), 10, 64)
	resp, err := s.Shipping.DeleteContractShippingRequoteOption(r.Context(), &shippingv1.DeleteContractShippingRequoteOptionRequest{HandoffId: idFromPath(r), OptionId: optionID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) selectContractShippingRequoteDraft(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.SelectContractShippingRequoteDraftRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.HandoffId = idFromPath(r)
	resp, err := s.Shipping.SelectContractShippingRequoteDraft(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) presignContractShippingContract(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.PresignContractShippingContractUploadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.PresignContractShippingContractUpload(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) saveContractShippingContract(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.SaveContractShippingContractRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.SaveContractShippingContract(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	h := resp.GetHandoff()
	if _, err = s.Orders.CreateExternalPayable(r.Context(), &prv1.CreateExternalPayableRequest{BusinessType: "LOGISTICS", SourceBusinessId: req.GetId(), ExportContractNo: h.GetContractNo(), BusinessDocumentNo: h.GetForwarderContractNo(), PayeeId: h.GetFinalForwarderId(), PayeeName: h.GetFinalForwarderName(), Currency: h.GetFinalCurrency(), Amount: h.GetFinalFreightAmount(), DueDate: h.GetFinalEtd(), PaymentTerms: h.GetPaymentTerms(), SignedContractKey: h.GetSignedContractKey(), SignedContractName: h.GetSignedContractName()}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) verifyContractShippingContract(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r)
	payable, err := s.Orders.GetExternalPayable(r.Context(), &prv1.GetExternalPayableRequest{BusinessType: "LOGISTICS", SourceBusinessId: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if _, err = s.Orders.VerifyOrderContract(r.Context(), &prv1.VerifyOrderContractRequest{Id: payable.GetRow().GetPoId()}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Shipping.VerifyContractShippingContract(r.Context(), &shippingv1.VerifyContractShippingContractRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
func (s *Server) requestContractShippingPayment(w http.ResponseWriter, r *http.Request) {
	id := idFromPath(r)
	detail, err := s.Shipping.GetContractShippingHandoff(r.Context(), &shippingv1.GetContractShippingHandoffRequest{Id: id})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	h := detail.GetHandoff()
	if h.GetStatus() != "CONTRACT_VERIFIED" {
		s.writeGRPCError(w, status.Error(codes.FailedPrecondition, "货代合同经财务上级或老板核验后才能申请付款"))
		return
	}
	payable, err := s.Orders.CreateExternalPayable(r.Context(), &prv1.CreateExternalPayableRequest{BusinessType: "LOGISTICS", SourceBusinessId: id, ExportContractNo: h.GetContractNo(), BusinessDocumentNo: h.GetForwarderContractNo(), PayeeId: h.GetFinalForwarderId(), PayeeName: h.GetFinalForwarderName(), Currency: h.GetFinalCurrency(), Amount: h.GetFinalFreightAmount(), DueDate: h.GetFinalEtd(), PaymentTerms: h.GetPaymentTerms(), SignedContractKey: h.GetSignedContractKey(), SignedContractName: h.GetSignedContractName()})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	if _, err = s.Shipping.MarkContractShippingPaymentRequested(r.Context(), &shippingv1.MarkContractShippingPaymentRequestedRequest{Id: id}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, payable)
}
func (s *Server) getContractShippingPaymentStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Orders.GetExternalPayable(r.Context(), &prv1.GetExternalPayableRequest{BusinessType: "LOGISTICS", SourceBusinessId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listSourcingShippingTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resp, err := s.Sourcing.ListSourcingShippingTasks(r.Context(), &prv1.ListSourcingShippingTasksRequest{
		Status: q.Get("status"), Keyword: q.Get("keyword"), Page: pageFromQuery(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingSourcingTask(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.GetSourcingShippingCollaboration(r.Context(), &prv1.GetSourcingShippingCollaborationRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getCaseShippingCollaboration(w http.ResponseWriter, r *http.Request) {
	caseID := idFromPath(r)
	// Reuse the sourcing service's owner/data-scope fence before exposing a
	// shipping summary through either the sales or procurement case route.
	if _, err := s.Sourcing.GetCase(r.Context(), &prv1.GetCaseRequest{Id: caseID}); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Sourcing.GetSourcingShippingCollaboration(r.Context(), &prv1.GetSourcingShippingCollaborationRequest{CaseId: caseID})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	// Sales and procurement receive only the manager-approved handoff. Raw
	// carrier quotes and shipping-team participation remain inside the shipping
	// submodule, matching the procurement manager-plan boundary.
	resp.Options = nil
	resp.Participants = nil
	approved := resp.Plans[:0]
	for _, plan := range resp.Plans {
		if plan.GetStatus() == "SUBMITTED_TO_SALES" {
			approved = append(approved, plan)
		}
	}
	resp.Plans = approved
	s.writeProto(w, resp)
}

func (s *Server) startSourcingShippingTask(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.StartSourcingShippingTask(r.Context(), &prv1.StartSourcingShippingTaskRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addSourcingShippingOption(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AddSourcingShippingOptionRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.CaseId = idFromPath(r)
	// 售前询价尚未订舱，不绑定正式船期。船运公司、预计开船时间和逐货物价格
	// 是本次人工询价的报价快照；客户选择后才进入正式船期管理。
	resp, err := s.Sourcing.AddSourcingShippingOption(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) joinSourcingShippingTask(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.JoinSourcingShippingTask(r.Context(), &prv1.JoinSourcingShippingTaskRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) requestPrimaryShipping(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.RequestPrimaryShipping(r.Context(), &prv1.RequestPrimaryShippingRequest{CaseId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) assignPrimaryShipping(w http.ResponseWriter, r *http.Request) {
	req := &prv1.AssignPrimaryShippingRequest{CaseId: idFromPath(r)}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Sourcing.AssignPrimaryShipping(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createSourcingShippingPlan(w http.ResponseWriter, r *http.Request) {
	req := &prv1.CreateSourcingShippingPlanRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	// protojson.Unmarshal resets the target message before decoding. Set values
	// sourced from the URL only after decoding the request body.
	req.CaseId = idFromPath(r)
	resp, err := s.Sourcing.CreateSourcingShippingPlan(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) submitSourcingShippingPlan(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Sourcing.SubmitSourcingShippingPlanToSales(r.Context(), &prv1.SubmitSourcingShippingPlanToSalesRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingSchedule(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetSchedule(r.Context(), &shippingv1.GetScheduleRequest{Id: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) createShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.CreateScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if err := s.resolveShippingMasterdata(r, req.GetSchedule(), true); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	resp, err := s.Shipping.CreateSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	if err := s.resolveShippingMasterdata(r, req.GetSchedule(), false); err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingScheduleStatus(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateScheduleStatusRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateScheduleStatus(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cancelShippingSchedule(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.CancelScheduleRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.CancelSchedule(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingStatistics(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetShippingStatistics(r.Context(), &shippingv1.GetShippingStatisticsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func shippingReminderID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "reminderID"), 10, 64)
	return id
}

// 提单签发提醒（E2）。围栏在服务端（按登录人），这里只转参数。
func (s *Server) listBLReminders(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 32)
	resp, err := s.Shipping.ListBlReminders(r.Context(), &shippingv1.ListBlRemindersRequest{
		UnreadOnly: r.URL.Query().Get("unread") == "1",
		Limit:      int32(limit),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markBLRemindersRead(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.MarkBlRemindersReadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Shipping.MarkBlRemindersRead(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippingArrivalNotifications(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.ListArrivalNotifications(r.Context(), &shippingv1.ListArrivalNotificationsRequest{
		UnreadOnly: r.URL.Query().Get("unread_only") == "true",
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markShippingArrivalReminderRead(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.MarkArrivalReminderRead(r.Context(), &shippingv1.MarkArrivalReminderReadRequest{
		ReminderId: shippingReminderID(r),
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) cleanupExpiredShippingArrivalReminders(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.CleanupExpiredArrivalReminders(r.Context(), &shippingv1.CleanupExpiredArrivalRemindersRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingArrivalReminderRules(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetArrivalReminderRules(r.Context(), &shippingv1.GetArrivalReminderRulesRequest{ScheduleId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingArrivalReminderRules(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateArrivalReminderRulesRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ScheduleId = idFromPath(r)
	resp, err := s.Shipping.UpdateArrivalReminderRules(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) getShippingReminderPreference(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.GetUserReminderPreference(r.Context(), &shippingv1.GetUserReminderPreferenceRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingReminderPreference(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateUserReminderPreferenceRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Shipping.UpdateUserReminderPreference(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) syncShippingHolidayCalendars(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.SyncUserHolidayCalendars(r.Context(), &shippingv1.SyncUserHolidayCalendarsRequest{})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listShippingOperationalAlerts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.ListOperationalAlerts(r.Context(), &shippingv1.ListOperationalAlertsRequest{OpenOnly: r.URL.Query().Get("open_only") != "false"})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) listScheduleOperationalAlerts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.ListScheduleOperationalAlerts(r.Context(), &shippingv1.ListScheduleOperationalAlertsRequest{ScheduleId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) markShippingOperationalAlertsRead(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.MarkOperationalAlertsReadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	resp, err := s.Shipping.MarkOperationalAlertsRead(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) resolveShippingOperationalAlert(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.ResolveOperationalAlertRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id, _ = strconv.ParseInt(chi.URLParam(r, "alertID"), 10, 64)
	resp, err := s.Shipping.ResolveOperationalAlert(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) addShippingRouteNode(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.AddRouteNodeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	port, err := s.resolveActivePort(r, req.GetPortId())
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	req.PortCode = port.GetUnlocode()
	req.PortName = portSnapshotName(port)
	req.Timezone = port.GetTimezone()
	resp, err := s.Shipping.AddRouteNode(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) removeShippingRouteNode(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.RemoveRouteNodeRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	req.RouteNodeId, _ = strconv.ParseInt(chi.URLParam(r, "nodeID"), 10, 64)
	resp, err := s.Shipping.RemoveRouteNode(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) reorderShippingRoute(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.ReorderRouteRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.ReorderRoute(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) updateShippingProgress(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.UpdateProgressRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.Id = idFromPath(r)
	resp, err := s.Shipping.UpdateProgress(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func shippingDocumentID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "documentID"), 10, 64)
	return id
}

func (s *Server) listShippingDocuments(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Shipping.ListDocuments(r.Context(), &shippingv1.ListDocumentsRequest{ScheduleId: idFromPath(r)})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) presignShippingDocument(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.PresignDocumentUploadRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ScheduleId = idFromPath(r)
	resp, err := s.Shipping.PresignDocumentUpload(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) registerShippingDocument(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.RegisterDocumentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ScheduleId = idFromPath(r)
	resp, err := s.Shipping.RegisterDocument(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) shippingDocumentAccess(w http.ResponseWriter, r *http.Request, mode string) {
	resp, err := s.Shipping.GetDocumentAccess(r.Context(), &shippingv1.GetDocumentAccessRequest{
		ScheduleId: idFromPath(r), DocumentId: shippingDocumentID(r), Mode: mode,
	})
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}

func (s *Server) previewShippingDocument(w http.ResponseWriter, r *http.Request) {
	s.shippingDocumentAccess(w, r, "PREVIEW")
}

func (s *Server) downloadShippingDocument(w http.ResponseWriter, r *http.Request) {
	s.shippingDocumentAccess(w, r, "DOWNLOAD")
}

func (s *Server) invalidateShippingDocument(w http.ResponseWriter, r *http.Request) {
	req := &shippingv1.InvalidateDocumentRequest{}
	if !s.decodeBody(w, r, req) {
		return
	}
	req.ScheduleId = idFromPath(r)
	req.DocumentId = shippingDocumentID(r)
	resp, err := s.Shipping.InvalidateDocument(r.Context(), req)
	if err != nil {
		s.writeGRPCError(w, err)
		return
	}
	s.writeProto(w, resp)
}
