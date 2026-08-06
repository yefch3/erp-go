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
	if s.ContractID != nil {
		contractID = *s.ContractID
	}
	if s.CustomerID != nil {
		customerID = *s.CustomerID
	}
	if s.CarrierID != nil {
		carrierID = *s.CarrierID
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
	return &shippingv1.RouteNode{Id: n.ID, SequenceNo: n.SequenceNo, NodeType: n.NodeType, PortCode: n.PortCode,
		PortName: n.PortName, Timezone: n.Timezone, OriginalEtaAt: timeValue(n.OriginalEtaAt), LatestEtaAt: timeValue(n.LatestEtaAt),
		ActualArrivalAt: timeValue(n.ActualArrivalAt), OriginalEtdAt: timeValue(n.OriginalEtdAt), LatestEtdAt: timeValue(n.LatestEtdAt),
		ActualDepartureAt: timeValue(n.ActualDepartureAt), NodeStatus: n.NodeStatus, Remark: n.Remark}
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
	}
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
	})
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
	details, err := h.svc.GetScheduleDetails(ctx, grpcx.TenantID(ctx), req.GetId())
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

func (h *Handler) GetArrivalReminderRules(ctx context.Context, req *shippingv1.GetArrivalReminderRulesRequest) (*shippingv1.GetArrivalReminderRulesResponse, error) {
	days, err := h.svc.GetArrivalReminderRules(ctx, grpcx.TenantID(ctx), req.GetScheduleId())
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

func (h *Handler) GetShippingStatistics(ctx context.Context, _ *shippingv1.GetShippingStatisticsRequest) (*shippingv1.GetShippingStatisticsResponse, error) {
	r, err := h.svc.ShippingStatistics(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.GetShippingStatisticsResponse{InTransit: r.InTransit, ArrivingWithin_7Days: r.ArrivingWithin7Days, Delayed: r.Delayed, TemporaryCall: r.TemporaryCall}, nil
}

func (h *Handler) AddRouteNode(ctx context.Context, req *shippingv1.AddRouteNodeRequest) (*shippingv1.AddRouteNodeResponse, error) {
	nodes, version, err := h.svc.AddRouteNode(ctx, grpcx.TenantID(ctx), req.GetId(), app.RouteNodeInput{NodeType: req.GetNodeType(), PortCode: req.GetPortCode(), PortName: req.GetPortName(), Timezone: req.GetTimezone(), InsertAfterNodeID: req.GetInsertAfterNodeId(), LatestETAAt: req.GetLatestEtaAt(), LatestETDAt: req.GetLatestEtdAt(), Reason: req.GetReason(), Remark: req.GetRemark(), RouteVersion: req.GetRouteVersion()}, operator(ctx))
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
	key, url, expires, err := h.svc.PresignDocumentUpload(ctx, grpcx.TenantID(ctx), req.GetScheduleId(), req.GetFileName())
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
	rows, err := h.svc.ListDocuments(ctx, grpcx.TenantID(ctx), req.GetScheduleId())
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
