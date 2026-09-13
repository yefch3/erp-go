// Package grpcin adapts shipping gRPC requests onto the application layer.
package grpcin

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
	"github.com/sgao19/erp-go/services/shipping/internal/store"
)

type Handler struct {
	shippingv1.UnimplementedShippingServiceServer
	svc *app.Service
}

func operator(ctx context.Context) app.Operator {
	op, _ := grpcx.OperatorFromContext(ctx)
	return app.Operator{ID: op.EmployeeID, Name: op.Name}
}

func scheduleToProto(s store.ShippingSchedule) *shippingv1.Schedule {
	contractID, customerID, carrierID := int64(0), int64(0), int64(0)
	loadingPortID, dischargePortID := int64(0), int64(0)
	if s.ContractID != nil {
		contractID = *s.ContractID
	}
	if s.CustomerID != nil {
		customerID = *s.CustomerID
	}
	if s.CarrierID != nil {
		carrierID = *s.CarrierID
	}
	if s.LoadingPortID != nil {
		loadingPortID = *s.LoadingPortID
	}
	if s.DischargePortID != nil {
		dischargePortID = *s.DischargePortID
	}
	etd, atd, eta, ata := "", "", "", ""
	if s.Etd.Valid {
		etd = s.Etd.Time.Format("2006-01-02")
	}
	if s.Atd.Valid {
		atd = s.Atd.Time.Format("2006-01-02")
	}
	if s.Eta.Valid {
		eta = s.Eta.Time.Format("2006-01-02")
	}
	if s.Ata.Valid {
		ata = s.Ata.Time.Format("2006-01-02")
	}
	createdAt, updatedAt := "", ""
	if s.CreatedAt.Valid {
		createdAt = s.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	if s.UpdatedAt.Valid {
		updatedAt = s.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	return &shippingv1.Schedule{
		Id: s.ID, ScheduleNo: s.ScheduleNo, ContractId: contractID, ContractNo: s.ContractNo,
		CustomerId: customerID, CustomerName: s.CustomerName, CarrierId: carrierID, CarrierForwarder: s.CarrierForwarder,
		VesselName: s.VesselName, VoyageNo: s.VoyageNo, PortOfLoading: s.PortOfLoading,
		PortOfDischarge: s.PortOfDischarge, Etd: etd, Atd: atd, Eta: eta, Ata: ata,
		ResponsibleEmployeeId: s.ResponsibleEmployeeID, ResponsibleName: s.ResponsibleName,
		Status: s.Status, Remark: s.Remark, CreatedBy: s.CreatedBy, CreatedByName: s.CreatedByName,
		CreatedAt: createdAt, UpdatedBy: s.UpdatedBy, UpdatedByName: s.UpdatedByName, UpdatedAt: updatedAt,
		OriginalEta: dateValue(s.OriginalEta), EtaRevision: s.EtaRevision, RouteVersion: s.RouteVersion,
		DelayDays: s.DelayDays, HasTemporaryCall: s.HasTemporaryCall, CurrentProgress: s.CurrentProgress,
		LoadingPortId: loadingPortID, LoadingPortCode: s.LoadingPortCode,
		LoadingPortTimezone: s.LoadingPortTimezone, DischargePortId: dischargePortID,
		DischargePortCode: s.DischargePortCode, DischargePortTimezone: s.DischargePortTimezone,
		BookingNo: s.BookingNo, BillOfLadingNo: s.BillOfLadingNo,
		WarehouseEntryDate: dateValue(s.WarehouseEntryDate), CustomsDeclarationDate: dateValue(s.CustomsDeclarationDate),
		FreightCurrency: s.FreightCurrency, FreightAmount: s.FreightAmount,
	}
}

func dateValue(v pgtype.Date) string {
	if !v.Valid {
		return ""
	}
	return v.Time.Format("2006-01-02")
}
func timeValue(v pgtype.Timestamptz) string {
	if !v.Valid {
		return ""
	}
	return v.Time.Format("2006-01-02T15:04:05Z07:00")
}

func routeNodeToProto(n store.ShippingRouteNode) *shippingv1.RouteNode {
	portID := int64(0)
	if n.PortID != nil {
		portID = *n.PortID
	}
	return &shippingv1.RouteNode{Id: n.ID, SequenceNo: n.SequenceNo, NodeType: n.NodeType, PortCode: n.PortCode,
		PortName: n.PortName, Timezone: n.Timezone, OriginalEtaAt: timeValue(n.OriginalEtaAt), LatestEtaAt: timeValue(n.LatestEtaAt),
		ActualArrivalAt: timeValue(n.ActualArrivalAt), OriginalEtdAt: timeValue(n.OriginalEtdAt), LatestEtdAt: timeValue(n.LatestEtdAt),
		ActualDepartureAt: timeValue(n.ActualDepartureAt), NodeStatus: n.NodeStatus, Remark: n.Remark, PortId: portID}
}

func routeNodesToProto(nodes []store.ShippingRouteNode) []*shippingv1.RouteNode {
	out := make([]*shippingv1.RouteNode, len(nodes))
	for i, n := range nodes {
		out[i] = routeNodeToProto(n)
	}
	return out
}

func delayToProto(d store.ShippingDelayEvent) *shippingv1.DelayEvent {
	var affected, from, to int64
	if d.AffectedNodeID != nil {
		affected = *d.AffectedNodeID
	}
	if d.FromNodeID != nil {
		from = *d.FromNodeID
	}
	if d.ToNodeID != nil {
		to = *d.ToNodeID
	}
	return &shippingv1.DelayEvent{Id: d.ID, ImpactType: d.ImpactType, AffectedNodeId: affected, FromNodeId: from, ToNodeId: to,
		ReasonCode: d.ReasonCode, Reason: d.Reason, Note: d.Note, OldEta: dateValue(d.OldEta), NewEta: dateValue(d.NewEta),
		ChangeDays: d.ChangeDays, CumulativeDelayDays: d.CumulativeDelayDays, Status: d.Status, OperatorId: d.OperatorID,
		OperatorName: d.OperatorName, CreatedAt: timeValue(d.CreatedAt)}
}

func delaysToProto(delays []store.ShippingDelayEvent) []*shippingv1.DelayEvent {
	out := make([]*shippingv1.DelayEvent, len(delays))
	for i, d := range delays {
		out[i] = delayToProto(d)
	}
	return out
}

func reminderToProto(r store.ShippingArrivalReminder) *shippingv1.ArrivalReminder {
	return &shippingv1.ArrivalReminder{
		Id: r.ID, ScheduleId: r.ScheduleID, EtaRevision: r.EtaRevision,
		TargetEta: dateValue(r.TargetEta), DueAt: timeValue(r.DueAt), Status: r.Status,
		SentAt: timeValue(r.SentAt), Title: r.Title, Content: r.Content,
		DetailUrl: r.DetailUrl, ReadAt: timeValue(r.ReadAt), AttemptCount: r.AttemptCount,
		LastError: r.LastError,
	}
}

func remindersToProto(rows []store.ShippingArrivalReminder) []*shippingv1.ArrivalReminder {
	out := make([]*shippingv1.ArrivalReminder, len(rows))
	for i, row := range rows {
		out[i] = reminderToProto(row)
	}
	return out
}

func documentToProto(d store.ShippingDocument) *shippingv1.ShippingDocument {
	var voidedBy int64
	var voidedByName, voidReason string
	if d.VoidedBy != nil {
		voidedBy = *d.VoidedBy
	}
	if d.VoidedByName != nil {
		voidedByName = *d.VoidedByName
	}
	if d.VoidReason != nil {
		voidReason = *d.VoidReason
	}
	return &shippingv1.ShippingDocument{
		Id: d.ID, ScheduleId: d.ScheduleID, DocumentGroupKey: d.DocumentGroupKey,
		Category: d.Category, Version: d.Version, FileName: d.FileName,
		FileSize: d.FileSize, ContentType: d.ContentType, Remark: d.Remark,
		Status: d.Status, UploadedBy: d.UploadedBy, UploadedByName: d.UploadedByName,
		UploadedAt: timeValue(d.UploadedAt), VoidedBy: voidedBy,
		VoidedByName: voidedByName, VoidedAt: timeValue(d.VoidedAt), VoidReason: voidReason,
	}
}

func documentsToProto(rows []store.ShippingDocument) []*shippingv1.ShippingDocument {
	out := make([]*shippingv1.ShippingDocument, len(rows))
	for i, row := range rows {
		out[i] = documentToProto(row)
	}
	return out
}

func inputFromProto(in *shippingv1.ScheduleInput) app.ScheduleInput {
	return app.ScheduleInput{
		ContractID: in.GetContractId(), ContractNo: in.GetContractNo(), CustomerID: in.GetCustomerId(), CarrierID: in.GetCarrierId(),
		CustomerName: in.GetCustomerName(), CarrierForwarder: in.GetCarrierForwarder(),
		VesselName: in.GetVesselName(), VoyageNo: in.GetVoyageNo(), PortOfLoading: in.GetPortOfLoading(),
		PortOfDischarge: in.GetPortOfDischarge(), ETD: in.GetEtd(), ATD: in.GetAtd(), ETA: in.GetEta(), ATA: in.GetAta(),
		ResponsibleEmployeeID: in.GetResponsibleEmployeeId(), ResponsibleName: in.GetResponsibleName(), Remark: in.GetRemark(),
		LoadingPortID: in.GetLoadingPortId(), LoadingPortCode: in.GetLoadingPortCode(), LoadingPortTimezone: in.GetLoadingPortTimezone(),
		DischargePortID: in.GetDischargePortId(), DischargePortCode: in.GetDischargePortCode(), DischargePortTimezone: in.GetDischargePortTimezone(),
		ContractHandoffID: in.GetContractHandoffId(),
		BookingNo:         in.GetBookingNo(), BillOfLadingNo: in.GetBillOfLadingNo(),
		WarehouseEntryDate: in.GetWarehouseEntryDate(), CustomsDeclarationDate: in.GetCustomsDeclarationDate(),
		FreightCurrency: in.GetFreightCurrency(), FreightAmount: in.GetFreightAmount(),
	}
}

func (h *Handler) ListContractShippingHandoffs(ctx context.Context, req *shippingv1.ListContractShippingHandoffsRequest) (*shippingv1.ListContractShippingHandoffsResponse, error) {
	rows, err := h.svc.ListD4ContractHandoffs(ctx, grpcx.TenantID(ctx), req.GetStatus())
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.ContractShippingHandoff, 0, len(rows))
	for _, row := range rows {
		out = append(out, handoffToProto(row))
	}
	return &shippingv1.ListContractShippingHandoffsResponse{Handoffs: out}, nil
}

func handoffToProto(row app.ContractHandoff) *shippingv1.ContractShippingHandoff {
	out := &shippingv1.ContractShippingHandoff{Id: row.ID, ContractId: row.ContractID, ContractNo: row.ContractNo, ContractVersionId: row.ContractVersionID, VersionNo: row.VersionNo, CustomerId: row.CustomerID, CustomerName: row.CustomerName, BatchNo: row.BatchNo, ShipmentGroupKey: row.ShipmentGroupKey, CarrierForwarder: row.CarrierForwarder, ServiceOptionName: row.ServiceOptionName, CustomerManaged: row.CustomerManaged, Currency: row.Currency, FreightAmount: row.FreightAmount, ChargeBasis: row.ChargeBasis, PortOfLoading: row.PortOfLoading, PortOfDischarge: row.PortOfDischarge, EstimatedDeparture: row.EstimatedDeparture, EstimatedArrival: row.EstimatedArrival, ValidUntil: row.ValidUntil, Remark: row.Remark, Status: row.Status, ScheduleId: row.ScheduleID, CreatedAt: row.CreatedAt, FinalForwarderId: row.FinalForwarderID, FinalForwarderName: row.FinalForwarderName, ActualCarrierId: row.ActualCarrierID, ActualCarrierName: row.ActualCarrierName, FinalServiceOption: row.FinalServiceOption, FinalCurrency: row.FinalCurrency, FinalFreightAmount: row.FinalFreightAmount, FinalEtd: row.FinalETD, FinalEta: row.FinalETA, PaymentTerms: row.PaymentTerms, ForwarderContractNo: row.ForwarderContractNo, ApprovalInstanceId: row.ApprovalInstanceID, ReturnReason: row.ReturnReason, OperatorName: row.OperatorName, SignedContractName: row.SignedContractName, SignedContractUrl: row.SignedContractURL, SignedContractUploadedAt: row.SignedContractUploadedAt, ContractVerifiedAt: row.ContractVerifiedAt, ContractVerifiedByName: row.ContractVerifiedByName, PaymentRequestedAt: row.PaymentRequestedAt, UpdatedAt: row.UpdatedAt, SignedContractKey: row.SignedContractKey}
	for _, item := range row.CargoItems {
		out.CargoItems = append(out.CargoItems, &shippingv1.ContractShippingCargoItem{Id: item.ID, ContractItemId: item.ContractItemID, LineNo: item.LineNo, ProductCode: item.ProductCode, ProductName: item.ProductName, Specification: item.Specification, Quantity: item.Quantity, UomCode: item.UomCode, Remark: item.Remark})
	}
	return out
}
func (h *Handler) GetContractShippingHandoff(ctx context.Context, req *shippingv1.GetContractShippingHandoffRequest) (*shippingv1.GetContractShippingHandoffResponse, error) {
	v, err := h.svc.GetContractHandoff(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetContractShippingHandoffResponse{Handoff: handoffToProto(v)}, nil
}
func requoteOptionToProto(row app.ShippingRequoteOption) *shippingv1.ShippingRequoteOption {
	return &shippingv1.ShippingRequoteOption{Id: row.ID, HandoffId: row.HandoffID, ForwarderId: row.ForwarderID, ForwarderName: row.ForwarderName, ActualCarrierId: row.ActualCarrierID, ActualCarrierName: row.ActualCarrierName, ServiceOption: row.ServiceOption, Currency: row.Currency, FreightAmount: row.FreightAmount, Etd: row.ETD, Eta: row.ETA, PaymentTerms: row.PaymentTerms, Remark: row.Remark, CreatedBy: row.CreatedBy, CreatedByName: row.CreatedByName, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func (h *Handler) ListContractShippingRequoteOptions(ctx context.Context, req *shippingv1.ListContractShippingRequoteOptionsRequest) (*shippingv1.ListContractShippingRequoteOptionsResponse, error) {
	rows, err := h.svc.ListShippingRequoteOptions(ctx, grpcx.TenantID(ctx), req.GetHandoffId())
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.ShippingRequoteOption, 0, len(rows))
	for _, row := range rows {
		out = append(out, requoteOptionToProto(row))
	}
	return &shippingv1.ListContractShippingRequoteOptionsResponse{Options: out}, nil
}
func (h *Handler) SubmitContractShippingRequote(ctx context.Context, req *shippingv1.SubmitContractShippingRequoteRequest) (*shippingv1.SubmitContractShippingRequoteResponse, error) {
	v, err := h.svc.SubmitFinalRequote(ctx, grpcx.TenantID(ctx), req.GetId(), app.FinalRequoteInput{OptionID: req.GetOptionId(), FinalForwarderID: req.GetFinalForwarderId(), FinalForwarderName: req.GetFinalForwarderName(), ActualCarrierID: req.GetActualCarrierId(), ActualCarrierName: req.GetActualCarrierName(), FinalServiceOption: req.GetFinalServiceOption(), FinalCurrency: req.GetFinalCurrency(), FinalFreightAmount: req.GetFinalFreightAmount(), FinalETD: req.GetFinalEtd(), FinalETA: req.GetFinalEta(), PaymentTerms: req.GetPaymentTerms(), ForwarderContractNo: req.GetForwarderContractNo(), Remark: req.GetRemark()}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.SubmitContractShippingRequoteResponse{Handoff: handoffToProto(v)}, nil
}
func (h *Handler) SaveContractShippingRequoteDraft(ctx context.Context, req *shippingv1.SaveContractShippingRequoteDraftRequest) (*shippingv1.SaveContractShippingRequoteDraftResponse, error) {
	v, err := h.svc.SaveFinalRequoteDraft(ctx, grpcx.TenantID(ctx), req.GetId(), app.FinalRequoteInput{OptionID: req.GetOptionId(), FinalForwarderID: req.GetFinalForwarderId(), FinalForwarderName: req.GetFinalForwarderName(), ActualCarrierID: req.GetActualCarrierId(), ActualCarrierName: req.GetActualCarrierName(), FinalServiceOption: req.GetFinalServiceOption(), FinalCurrency: req.GetFinalCurrency(), FinalFreightAmount: req.GetFinalFreightAmount(), FinalETD: req.GetFinalEtd(), FinalETA: req.GetFinalEta(), PaymentTerms: req.GetPaymentTerms(), ForwarderContractNo: req.GetForwarderContractNo(), Remark: req.GetRemark()}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.SaveContractShippingRequoteDraftResponse{Handoff: handoffToProto(v)}, nil
}
func (h *Handler) DeleteContractShippingRequoteOption(ctx context.Context, req *shippingv1.DeleteContractShippingRequoteOptionRequest) (*shippingv1.DeleteContractShippingRequoteOptionResponse, error) {
	if err := h.svc.DeleteShippingRequoteOption(ctx, grpcx.TenantID(ctx), req.GetHandoffId(), req.GetOptionId()); err != nil {
		return nil, err
	}
	return &shippingv1.DeleteContractShippingRequoteOptionResponse{}, nil
}
func (h *Handler) SelectContractShippingRequoteDraft(ctx context.Context, req *shippingv1.SelectContractShippingRequoteDraftRequest) (*shippingv1.SelectContractShippingRequoteDraftResponse, error) {
	v, err := h.svc.SelectFinalRequoteDraft(ctx, grpcx.TenantID(ctx), req.GetHandoffId(), req.GetOptionId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.SelectContractShippingRequoteDraftResponse{Handoff: handoffToProto(v)}, nil
}
func (h *Handler) PresignContractShippingContractUpload(ctx context.Context, req *shippingv1.PresignContractShippingContractUploadRequest) (*shippingv1.PresignContractShippingContractUploadResponse, error) {
	v, err := h.svc.PresignShippingContract(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &shippingv1.PresignContractShippingContractUploadResponse{FileKey: v.Key, UploadUrl: v.URL, ExpiresSeconds: v.Expires}, nil
}
func (h *Handler) SaveContractShippingContract(ctx context.Context, req *shippingv1.SaveContractShippingContractRequest) (*shippingv1.SaveContractShippingContractResponse, error) {
	v, err := h.svc.SaveShippingContract(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetFileKey(), req.GetFileName(), req.GetContractNo())
	if err != nil {
		return nil, err
	}
	return &shippingv1.SaveContractShippingContractResponse{Handoff: handoffToProto(v)}, nil
}
func (h *Handler) VerifyContractShippingContract(ctx context.Context, req *shippingv1.VerifyContractShippingContractRequest) (*shippingv1.VerifyContractShippingContractResponse, error) {
	v, err := h.svc.VerifyShippingContract(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.VerifyContractShippingContractResponse{Handoff: handoffToProto(v)}, nil
}
func (h *Handler) MarkContractShippingPaymentRequested(ctx context.Context, req *shippingv1.MarkContractShippingPaymentRequestedRequest) (*shippingv1.MarkContractShippingPaymentRequestedResponse, error) {
	v, err := h.svc.MarkShippingPaymentRequested(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &shippingv1.MarkContractShippingPaymentRequestedResponse{Handoff: handoffToProto(v)}, nil
}

func listRowToProto(r store.ListSchedulesRow) *shippingv1.Schedule {
	return scheduleToProto(store.ShippingSchedule{
		ID: r.ID, TenantID: r.TenantID, ScheduleNo: r.ScheduleNo, ContractID: r.ContractID, ContractNo: r.ContractNo,
		CustomerID: r.CustomerID, CustomerName: r.CustomerName, CarrierID: r.CarrierID, CarrierForwarder: r.CarrierForwarder,
		VesselName: r.VesselName, VoyageNo: r.VoyageNo, PortOfLoading: r.PortOfLoading, PortOfDischarge: r.PortOfDischarge,
		Etd: r.Etd, Atd: r.Atd, Eta: r.Eta, Ata: r.Ata, ResponsibleEmployeeID: r.ResponsibleEmployeeID,
		ResponsibleName: r.ResponsibleName, Status: r.Status, Remark: r.Remark, CreatedBy: r.CreatedBy,
		CreatedByName: r.CreatedByName, CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy, UpdatedByName: r.UpdatedByName, UpdatedAt: r.UpdatedAt,
		OriginalEta: r.OriginalEta, EtaRevision: r.EtaRevision, RouteVersion: r.RouteVersion,
		DelayDays: r.DelayDays, HasTemporaryCall: r.HasTemporaryCall, CurrentProgress: r.CurrentProgress,
		LoadingPortID: r.LoadingPortID, LoadingPortCode: r.LoadingPortCode, LoadingPortTimezone: r.LoadingPortTimezone,
		DischargePortID: r.DischargePortID, DischargePortCode: r.DischargePortCode, DischargePortTimezone: r.DischargePortTimezone,
		BookingNo: r.BookingNo, BillOfLadingNo: r.BillOfLadingNo,
		WarehouseEntryDate: r.WarehouseEntryDate, CustomsDeclarationDate: r.CustomsDeclarationDate,
	})
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) GetModuleStatus(ctx context.Context, _ *shippingv1.GetModuleStatusRequest) (*shippingv1.GetModuleStatusResponse, error) {
	if err := h.svc.ModuleStatus(ctx); err != nil {
		return nil, err
	}
	return &shippingv1.GetModuleStatusResponse{
		Module:        "shipping",
		Status:        "READY",
		SchemaVersion: app.SchemaVersion,
	}, nil
}

func (h *Handler) ListSchedules(ctx context.Context, req *shippingv1.ListSchedulesRequest) (*shippingv1.ListSchedulesResponse, error) {
	page, size := req.GetPage().GetPage(), req.GetPage().GetPageSize()
	rows, total, page, size, err := h.svc.ListSchedules(ctx, grpcx.TenantID(ctx), app.ListFilter{
		Keyword: req.GetKeyword(), Status: req.GetStatus(), PortOfLoading: req.GetPortOfLoading(), PortOfDischarge: req.GetPortOfDischarge(),
		ETDFrom: req.GetEtdFrom(), ETDTo: req.GetEtdTo(), ETAFrom: req.GetEtaFrom(), ETATo: req.GetEtaTo(), Page: page, PageSize: size,
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.Schedule, len(rows))
	for i, r := range rows {
		out[i] = listRowToProto(r)
	}
	return &shippingv1.ListSchedulesResponse{Schedules: out, Meta: &commonv1.PageMeta{Total: total, Page: page, PageSize: size}}, nil
}

func (h *Handler) GetSchedule(ctx context.Context, req *shippingv1.GetScheduleRequest) (*shippingv1.GetScheduleResponse, error) {
	details, err := h.svc.GetScheduleDetails(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.ScheduleChange, len(details.Changes))
	for i, c := range details.Changes {
		created := ""
		if c.CreatedAt.Valid {
			created = c.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		}
		out[i] = &shippingv1.ScheduleChange{Id: c.ID, ChangeType: c.ChangeType, FieldName: c.FieldName, OldValue: c.OldValue, NewValue: c.NewValue, Reason: c.Reason, OperatorId: c.OperatorID, OperatorName: c.OperatorName, CreatedAt: created}
	}
	reminders := remindersToProto(details.Reminders)
	return &shippingv1.GetScheduleResponse{Schedule: scheduleToProto(details.Schedule), Changes: out, RouteNodes: routeNodesToProto(details.Route), DelayEvents: delaysToProto(details.Delays), Reminders: reminders}, nil
}

// ListBlReminders 是本人的提单提醒收件箱（E2）——按登录人隔离，
// 提醒本来就是发给具体某个人的，不需要额外围栏。
func (h *Handler) ListBlReminders(ctx context.Context, req *shippingv1.ListBlRemindersRequest) (*shippingv1.ListBlRemindersResponse, error) {
	op := operator(ctx)
	rows, unread, err := h.svc.BLInbox(ctx, grpcx.TenantID(ctx), op.ID, req.GetUnreadOnly(), req.GetLimit())
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.BlReminder, 0, len(rows))
	for _, r := range rows {
		out = append(out, &shippingv1.BlReminder{
			Id: r.ID, ScheduleId: r.ScheduleID, ScheduleNo: r.ScheduleNo,
			VesselName: r.VesselName, VoyageNo: r.VoyageNo, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, PeriodNo: r.PeriodNo, DepartedOn: r.DepartedOn,
			Title: r.Title, Content: r.Content, DetailUrl: r.DetailURL,
			CreatedAt: r.CreatedAt, Unread: r.Unread,
		})
	}
	return &shippingv1.ListBlRemindersResponse{Items: out, UnreadTotal: unread}, nil
}

func (h *Handler) MarkBlRemindersRead(ctx context.Context, req *shippingv1.MarkBlRemindersReadRequest) (*shippingv1.MarkBlRemindersReadResponse, error) {
	op := operator(ctx)
	n, err := h.svc.MarkBLRemindersRead(ctx, grpcx.TenantID(ctx), op.ID, req.GetIds())
	if err != nil {
		return nil, err
	}
	return &shippingv1.MarkBlRemindersReadResponse{Marked: n}, nil
}

func (h *Handler) ListArrivalNotifications(ctx context.Context, req *shippingv1.ListArrivalNotificationsRequest) (*shippingv1.ListArrivalNotificationsResponse, error) {
	op := operator(ctx)
	page, err := h.svc.ListArrivalNotifications(ctx, grpcx.TenantID(ctx), op.ID, req.GetUnreadOnly())
	if err != nil {
		return nil, err
	}
	return &shippingv1.ListArrivalNotificationsResponse{Reminders: remindersToProto(page.Reminders), UnreadCount: page.UnreadCount}, nil
}

func (h *Handler) MarkArrivalReminderRead(ctx context.Context, req *shippingv1.MarkArrivalReminderReadRequest) (*shippingv1.MarkArrivalReminderReadResponse, error) {
	op := operator(ctx)
	row, err := h.svc.MarkArrivalReminderRead(ctx, grpcx.TenantID(ctx), op.ID, req.GetReminderId())
	if err != nil {
		return nil, err
	}
	return &shippingv1.MarkArrivalReminderReadResponse{Reminder: reminderToProto(row)}, nil
}

func (h *Handler) CleanupExpiredArrivalReminders(ctx context.Context, _ *shippingv1.CleanupExpiredArrivalRemindersRequest) (*shippingv1.CleanupExpiredArrivalRemindersResponse, error) {
	op := operator(ctx)
	count, err := h.svc.CleanupExpiredArrivalReminders(ctx, grpcx.TenantID(ctx), op.ID)
	if err != nil {
		return nil, err
	}
	return &shippingv1.CleanupExpiredArrivalRemindersResponse{DeletedCount: count}, nil
}

func (h *Handler) GetArrivalReminderRules(ctx context.Context, req *shippingv1.GetArrivalReminderRulesRequest) (*shippingv1.GetArrivalReminderRulesResponse, error) {
	days, err := h.svc.GetArrivalReminderRules(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetArrivalReminderRulesResponse{LeadDays: days}, nil
}

func (h *Handler) UpdateArrivalReminderRules(ctx context.Context, req *shippingv1.UpdateArrivalReminderRulesRequest) (*shippingv1.UpdateArrivalReminderRulesResponse, error) {
	days, err := h.svc.UpdateArrivalReminderRules(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), req.GetLeadDays(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.UpdateArrivalReminderRulesResponse{LeadDays: days}, nil
}

func reminderPreferenceToProto(value app.UserReminderPreference) *shippingv1.UserReminderPreference {
	return &shippingv1.UserReminderPreference{
		LeadDays: value.LeadDays, Timezone: value.Timezone, HolidayCountryCodes: value.HolidayCountryCodes,
		CalendarSyncStatus: value.CalendarSyncStatus, LastSyncAt: value.LastSyncAt,
		LastSuccessAt: value.LastSuccessAt, LastError: value.LastError,
	}
}

func (h *Handler) GetUserReminderPreference(ctx context.Context, _ *shippingv1.GetUserReminderPreferenceRequest) (*shippingv1.GetUserReminderPreferenceResponse, error) {
	preference, err := h.svc.GetUserReminderPreference(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetUserReminderPreferenceResponse{Preference: reminderPreferenceToProto(preference)}, nil
}

func (h *Handler) UpdateUserReminderPreference(ctx context.Context, req *shippingv1.UpdateUserReminderPreferenceRequest) (*shippingv1.UpdateUserReminderPreferenceResponse, error) {
	preference, err := h.svc.UpdateUserReminderPreference(ctx, grpcx.TenantID(ctx), req.GetLeadDays(), req.GetTimezone(), req.GetHolidayCountryCodes(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.UpdateUserReminderPreferenceResponse{Preference: reminderPreferenceToProto(preference)}, nil
}

func (h *Handler) SyncUserHolidayCalendars(ctx context.Context, _ *shippingv1.SyncUserHolidayCalendarsRequest) (*shippingv1.SyncUserHolidayCalendarsResponse, error) {
	preference, err := h.svc.SyncUserHolidayCalendars(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.SyncUserHolidayCalendarsResponse{Preference: reminderPreferenceToProto(preference)}, nil
}

func operationalAlertToProto(value app.OperationalAlert) *shippingv1.OperationalAlert {
	return &shippingv1.OperationalAlert{Id: value.ID, ScheduleId: value.ScheduleID, RecipientEmployeeId: value.RecipientEmployeeID, ScheduleNo: value.ScheduleNo, ContractNo: value.ContractNo, AlertType: value.AlertType, RecipientRole: value.RecipientRole, Title: value.Title, Content: value.Content, OldValue: value.OldValue, NewValue: value.NewValue, DueDate: value.DueDate, ReadAt: value.ReadAt, ResolvedAt: value.ResolvedAt, ResolutionNote: value.ResolutionNote, ResolvedByName: value.ResolvedByName, CreatedAt: value.CreatedAt}
}

func (h *Handler) ListOperationalAlerts(ctx context.Context, req *shippingv1.ListOperationalAlertsRequest) (*shippingv1.ListOperationalAlertsResponse, error) {
	items, unread, err := h.svc.ListOperationalAlerts(ctx, grpcx.TenantID(ctx), req.GetOpenOnly(), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.OperationalAlert, 0, len(items))
	for _, item := range items {
		out = append(out, operationalAlertToProto(item))
	}
	return &shippingv1.ListOperationalAlertsResponse{Items: out, UnreadCount: unread}, nil
}

func (h *Handler) ListScheduleOperationalAlerts(ctx context.Context, req *shippingv1.ListScheduleOperationalAlertsRequest) (*shippingv1.ListScheduleOperationalAlertsResponse, error) {
	items, err := h.svc.ListScheduleOperationalAlerts(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.OperationalAlert, 0, len(items))
	for _, item := range items {
		out = append(out, operationalAlertToProto(item))
	}
	return &shippingv1.ListScheduleOperationalAlertsResponse{Items: out}, nil
}

func (h *Handler) MarkOperationalAlertsRead(ctx context.Context, req *shippingv1.MarkOperationalAlertsReadRequest) (*shippingv1.MarkOperationalAlertsReadResponse, error) {
	marked, err := h.svc.MarkOperationalAlertsRead(ctx, grpcx.TenantID(ctx), req.GetIds(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.MarkOperationalAlertsReadResponse{Marked: marked}, nil
}

func (h *Handler) ResolveOperationalAlert(ctx context.Context, req *shippingv1.ResolveOperationalAlertRequest) (*shippingv1.ResolveOperationalAlertResponse, error) {
	item, err := h.svc.ResolveOperationalAlert(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetResolutionNote(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.ResolveOperationalAlertResponse{Alert: operationalAlertToProto(item)}, nil
}

func (h *Handler) GetShippingStatistics(ctx context.Context, _ *shippingv1.GetShippingStatisticsRequest) (*shippingv1.GetShippingStatisticsResponse, error) {
	r, err := h.svc.ShippingStatistics(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetShippingStatisticsResponse{InTransit: r.InTransit, ArrivingWithin_7Days: r.ArrivingWithin7Days, Delayed: r.Delayed, TemporaryCall: r.TemporaryCall}, nil
}

func (h *Handler) AddRouteNode(ctx context.Context, req *shippingv1.AddRouteNodeRequest) (*shippingv1.AddRouteNodeResponse, error) {
	nodes, version, err := h.svc.AddRouteNode(ctx, grpcx.TenantID(ctx), req.GetId(), app.RouteNodeInput{PortID: req.GetPortId(), NodeType: req.GetNodeType(), PortCode: req.GetPortCode(), PortName: req.GetPortName(), Timezone: req.GetTimezone(), InsertAfterNodeID: req.GetInsertAfterNodeId(), LatestETAAt: req.GetLatestEtaAt(), LatestETDAt: req.GetLatestEtdAt(), Reason: req.GetReason(), Remark: req.GetRemark(), RouteVersion: req.GetRouteVersion()}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.AddRouteNodeResponse{RouteNodes: routeNodesToProto(nodes), RouteVersion: version}, nil
}

func (h *Handler) RemoveRouteNode(ctx context.Context, req *shippingv1.RemoveRouteNodeRequest) (*shippingv1.RemoveRouteNodeResponse, error) {
	nodes, version, err := h.svc.RemoveRouteNode(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetRouteNodeId(), req.GetReason(), req.GetRouteVersion(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.RemoveRouteNodeResponse{RouteNodes: routeNodesToProto(nodes), RouteVersion: version}, nil
}

func (h *Handler) ReorderRoute(ctx context.Context, req *shippingv1.ReorderRouteRequest) (*shippingv1.ReorderRouteResponse, error) {
	nodes, version, err := h.svc.ReorderRoute(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetNodeIds(), req.GetReason(), req.GetRouteVersion(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.ReorderRouteResponse{RouteNodes: routeNodesToProto(nodes), RouteVersion: version}, nil
}

func (h *Handler) UpdateProgress(ctx context.Context, req *shippingv1.UpdateProgressRequest) (*shippingv1.UpdateProgressResponse, error) {
	s, nodes, delays, err := h.svc.UpdateProgress(ctx, grpcx.TenantID(ctx), req.GetId(), app.ProgressInput{
		RouteNodeID: req.GetRouteNodeId(), Action: req.GetAction(), ActualTime: req.GetActualTime(), LatestETA: req.GetLatestEta(),
		ReasonCode: req.GetReasonCode(), Reason: req.GetReason(), ImpactType: req.GetImpactType(), AffectedNodeID: req.GetAffectedNodeId(),
		FromNodeID: req.GetFromNodeId(), ToNodeID: req.GetToNodeId(), Note: req.GetNote(), RouteVersion: req.GetRouteVersion(),
		LatestETAAt: req.GetLatestEtaAt(), LatestETDAt: req.GetLatestEtdAt(), ActualArrivalAt: req.GetActualArrivalAt(), ActualDepartureAt: req.GetActualDepartureAt(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.UpdateProgressResponse{Schedule: scheduleToProto(s), RouteNodes: routeNodesToProto(nodes), DelayEvents: delaysToProto(delays)}, nil
}

func (h *Handler) PresignDocumentUpload(ctx context.Context, req *shippingv1.PresignDocumentUploadRequest) (*shippingv1.PresignDocumentUploadResponse, error) {
	key, url, expires, err := h.svc.PresignDocumentUpload(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), req.GetFileName(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.PresignDocumentUploadResponse{FileKey: key, UploadUrl: url, ExpiresInSeconds: expires}, nil
}

func (h *Handler) RegisterDocument(ctx context.Context, req *shippingv1.RegisterDocumentRequest) (*shippingv1.RegisterDocumentResponse, error) {
	doc, err := h.svc.RegisterDocument(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), req.GetFileKey(), req.GetFileName(), req.GetCategory(), req.GetRemark(), req.GetReplacesDocumentId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.RegisterDocumentResponse{Document: documentToProto(doc)}, nil
}

func (h *Handler) ListDocuments(ctx context.Context, req *shippingv1.ListDocumentsRequest) (*shippingv1.ListDocumentsResponse, error) {
	rows, err := h.svc.ListDocuments(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.ListDocumentsResponse{Documents: documentsToProto(rows)}, nil
}

func (h *Handler) GetDocumentAccess(ctx context.Context, req *shippingv1.GetDocumentAccessRequest) (*shippingv1.GetDocumentAccessResponse, error) {
	url, err := h.svc.DocumentAccess(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), req.GetDocumentId(), req.GetMode(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetDocumentAccessResponse{Url: url, ExpiresInSeconds: 120}, nil
}

func (h *Handler) InvalidateDocument(ctx context.Context, req *shippingv1.InvalidateDocumentRequest) (*shippingv1.InvalidateDocumentResponse, error) {
	doc, err := h.svc.InvalidateDocument(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), req.GetDocumentId(), req.GetReason(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.InvalidateDocumentResponse{Document: documentToProto(doc)}, nil
}

func (h *Handler) CreateSchedule(ctx context.Context, req *shippingv1.CreateScheduleRequest) (*shippingv1.CreateScheduleResponse, error) {
	s, err := h.svc.CreateSchedule(ctx, grpcx.TenantID(ctx), inputFromProto(req.GetSchedule()), operator(ctx), req.GetConfirmDuplicate())
	if err != nil {
		return nil, err
	}
	return &shippingv1.CreateScheduleResponse{Schedule: scheduleToProto(s)}, nil
}

func (h *Handler) UpdateSchedule(ctx context.Context, req *shippingv1.UpdateScheduleRequest) (*shippingv1.UpdateScheduleResponse, error) {
	s, err := h.svc.UpdateSchedule(ctx, grpcx.TenantID(ctx), req.GetId(), inputFromProto(req.GetSchedule()), req.GetDateChangeReason(), operator(ctx), req.GetConfirmDuplicate())
	if err != nil {
		return nil, err
	}
	return &shippingv1.UpdateScheduleResponse{Schedule: scheduleToProto(s)}, nil
}

func (h *Handler) UpdateScheduleStatus(ctx context.Context, req *shippingv1.UpdateScheduleStatusRequest) (*shippingv1.UpdateScheduleStatusResponse, error) {
	s, err := h.svc.UpdateScheduleStatus(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetStatus(), req.GetReason(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.UpdateScheduleStatusResponse{Schedule: scheduleToProto(s)}, nil
}

func (h *Handler) CancelSchedule(ctx context.Context, req *shippingv1.CancelScheduleRequest) (*shippingv1.CancelScheduleResponse, error) {
	s, err := h.svc.CancelSchedule(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetReason(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.CancelScheduleResponse{Schedule: scheduleToProto(s)}, nil
}

// ContractShippingSnapshot 一次答完一页合同的船期状态（D2）。
func (h *Handler) ContractShippingSnapshot(ctx context.Context, req *shippingv1.ContractShippingSnapshotRequest) (*shippingv1.ContractShippingSnapshotResponse, error) {
	byContract, err := h.svc.ContractShippingSnapshot(ctx, grpcx.TenantID(ctx), req.GetContractNos())
	if err != nil {
		return nil, err
	}
	items := make([]*shippingv1.ContractShippingSnapshotItem, 0, len(byContract))
	for _, s := range byContract {
		items = append(items, &shippingv1.ContractShippingSnapshotItem{
			ContractNo: s.ContractNo, ScheduleId: s.ScheduleID, ScheduleNo: s.ScheduleNo,
			VesselName: s.VesselName, VoyageNo: s.VoyageNo, Status: s.Status,
			Etd: s.ETD, Atd: s.ATD, Eta: s.ETA, Ata: s.ATA,
			DelayDays: s.DelayDays, LegCount: s.LegCount,
		})
	}
	return &shippingv1.ContractShippingSnapshotResponse{Items: items}, nil
}
