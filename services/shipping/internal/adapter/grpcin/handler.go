// Package grpcin adapts shipping gRPC requests onto the application layer.
package grpcin

import (
	"context"

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
	contractID, customerID := int64(0), int64(0)
	if s.ContractID != nil {
		contractID = *s.ContractID
	}
	if s.CustomerID != nil {
		customerID = *s.CustomerID
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
		CustomerId: customerID, CustomerName: s.CustomerName, CarrierForwarder: s.CarrierForwarder,
		VesselName: s.VesselName, VoyageNo: s.VoyageNo, PortOfLoading: s.PortOfLoading,
		PortOfDischarge: s.PortOfDischarge, Etd: etd, Atd: atd, Eta: eta, Ata: ata,
		ResponsibleEmployeeId: s.ResponsibleEmployeeID, ResponsibleName: s.ResponsibleName,
		Status: s.Status, Remark: s.Remark, CreatedBy: s.CreatedBy, CreatedByName: s.CreatedByName,
		CreatedAt: createdAt, UpdatedBy: s.UpdatedBy, UpdatedByName: s.UpdatedByName, UpdatedAt: updatedAt,
	}
}

func inputFromProto(in *shippingv1.ScheduleInput) app.ScheduleInput {
	return app.ScheduleInput{
		ContractID: in.GetContractId(), ContractNo: in.GetContractNo(), CustomerID: in.GetCustomerId(),
		CustomerName: in.GetCustomerName(), CarrierForwarder: in.GetCarrierForwarder(),
		VesselName: in.GetVesselName(), VoyageNo: in.GetVoyageNo(), PortOfLoading: in.GetPortOfLoading(),
		PortOfDischarge: in.GetPortOfDischarge(), ETD: in.GetEtd(), ATD: in.GetAtd(), ETA: in.GetEta(), ATA: in.GetAta(),
		ResponsibleEmployeeID: in.GetResponsibleEmployeeId(), ResponsibleName: in.GetResponsibleName(), Remark: in.GetRemark(),
	}
}

func listRowToProto(r store.ListSchedulesRow) *shippingv1.Schedule {
	return scheduleToProto(store.ShippingSchedule{
		ID: r.ID, TenantID: r.TenantID, ScheduleNo: r.ScheduleNo, ContractID: r.ContractID, ContractNo: r.ContractNo,
		CustomerID: r.CustomerID, CustomerName: r.CustomerName, CarrierForwarder: r.CarrierForwarder,
		VesselName: r.VesselName, VoyageNo: r.VoyageNo, PortOfLoading: r.PortOfLoading, PortOfDischarge: r.PortOfDischarge,
		Etd: r.Etd, Atd: r.Atd, Eta: r.Eta, Ata: r.Ata, ResponsibleEmployeeID: r.ResponsibleEmployeeID,
		ResponsibleName: r.ResponsibleName, Status: r.Status, Remark: r.Remark, CreatedBy: r.CreatedBy,
		CreatedByName: r.CreatedByName, CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy, UpdatedByName: r.UpdatedByName, UpdatedAt: r.UpdatedAt,
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
	s, changes, err := h.svc.GetSchedule(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	out := make([]*shippingv1.ScheduleChange, len(changes))
	for i, c := range changes {
		created := ""
		if c.CreatedAt.Valid {
			created = c.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00")
		}
		out[i] = &shippingv1.ScheduleChange{Id: c.ID, ChangeType: c.ChangeType, FieldName: c.FieldName, OldValue: c.OldValue, NewValue: c.NewValue, Reason: c.Reason, OperatorId: c.OperatorID, OperatorName: c.OperatorName, CreatedAt: created}
	}
	return &shippingv1.GetScheduleResponse{Schedule: scheduleToProto(s), Changes: out}, nil
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
