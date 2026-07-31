package grpcin

import (
	"context"

	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/export/internal/app"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// ShipmentHandler exposes the shipping document. It shares the app service
// with contracts because confirming a sailing writes against them.
type ShipmentHandler struct {
	exv1.UnimplementedShipmentServiceServer
	svc *app.Service
}

func NewShipments(svc *app.Service) *ShipmentHandler { return &ShipmentHandler{svc: svc} }

func (h *ShipmentHandler) ListShipments(ctx context.Context, req *exv1.ListShipmentsRequest) (*exv1.ListShipmentsResponse, error) {
	rows, total, err := h.svc.ListShipments(ctx, grpcx.TenantID(ctx), app.ShipmentQuery{
		Status: req.GetStatus(), Keyword: req.GetKeyword(), ContractID: req.GetContractId(),
		Page: req.GetPage().GetPage(), Size: req.GetPage().GetPageSize(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.Shipment, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.Shipment{
			Id: r.ID, ShipmentNo: r.ShipmentNo, VesselName: r.VesselName,
			VoyageNo: r.VoyageNo, BlNo: r.BlNo, ContainerNo: r.ContainerNo,
			PortOfDischarge: r.PortOfDischarge, Etd: r.Etd, Eta: r.Eta,
			Status: r.Status, CreatedByName: r.CreatedByName,
			CreatedAt: ts(r.CreatedAt), ShippedAt: ts(r.ShippedAt), ArrivedAt: ts(r.ArrivedAt),
			LineCount: r.LineCount, ContractCount: r.ContractCount, ContractNos: r.ContractNos,
		})
	}
	return &exv1.ListShipmentsResponse{Shipments: out, Total: total}, nil
}

func (h *ShipmentHandler) GetShipment(ctx context.Context, req *exv1.GetShipmentRequest) (*exv1.GetShipmentResponse, error) {
	view, err := h.svc.GetShipmentDoc(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &exv1.GetShipmentResponse{
		Shipment: shipmentToProto(view.Shipment), Items: shipmentItemsToProto(view.Items),
	}, nil
}

func (h *ShipmentHandler) CreateShipment(ctx context.Context, req *exv1.CreateShipmentRequest) (*exv1.CreateShipmentResponse, error) {
	view, err := h.svc.CreateShipment(ctx, grpcx.TenantID(ctx),
		shipmentFromProto(req.GetShipment()), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.CreateShipmentResponse{
		Shipment: shipmentToProto(view.Shipment), Items: shipmentItemsToProto(view.Items),
	}, nil
}

func (h *ShipmentHandler) UpdateShipment(ctx context.Context, req *exv1.UpdateShipmentRequest) (*exv1.UpdateShipmentResponse, error) {
	view, err := h.svc.UpdateShipment(ctx, grpcx.TenantID(ctx), req.GetId(),
		shipmentFromProto(req.GetShipment()), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.UpdateShipmentResponse{
		Shipment: shipmentToProto(view.Shipment), Items: shipmentItemsToProto(view.Items),
	}, nil
}

func (h *ShipmentHandler) ConfirmSailing(ctx context.Context, req *exv1.ConfirmSailingRequest) (*exv1.ConfirmSailingResponse, error) {
	view, err := h.svc.ConfirmSailing(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.ConfirmSailingResponse{
		Shipment: shipmentToProto(view.Shipment), Items: shipmentItemsToProto(view.Items),
	}, nil
}

func (h *ShipmentHandler) MarkArrived(ctx context.Context, req *exv1.MarkArrivedRequest) (*exv1.MarkArrivedResponse, error) {
	view, err := h.svc.MarkArrived(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.MarkArrivedResponse{
		Shipment: shipmentToProto(view.Shipment), Items: shipmentItemsToProto(view.Items),
	}, nil
}

func (h *ShipmentHandler) CancelShipment(ctx context.Context, req *exv1.CancelShipmentRequest) (*exv1.CancelShipmentResponse, error) {
	view, err := h.svc.CancelShipment(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.CancelShipmentResponse{
		Shipment: shipmentToProto(view.Shipment), Items: shipmentItemsToProto(view.Items),
	}, nil
}

func (h *ShipmentHandler) ListContractVessels(ctx context.Context, req *exv1.ListContractVesselsRequest) (*exv1.ListContractVesselsResponse, error) {
	rows, err := h.svc.VesselsForContract(ctx, grpcx.TenantID(ctx), req.GetContractId())
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.Vessel, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.Vessel{
			ShipmentId: r.ID, ShipmentNo: r.ShipmentNo, VesselName: r.VesselName,
			VoyageNo: r.VoyageNo, BlNo: r.BlNo, ContainerNo: r.ContainerNo,
			PortOfDischarge: r.PortOfDischarge, Etd: r.Etd, Eta: r.Eta,
			Status: r.Status, Qty: r.Qty,
		})
	}
	return &exv1.ListContractVesselsResponse{Vessels: out}, nil
}

func shipmentFromProto(in *exv1.ShipmentInput) app.ShipmentInput {
	items := make([]app.ShipmentLineInput, 0, len(in.GetItems()))
	for _, i := range in.GetItems() {
		items = append(items, app.ShipmentLineInput{
			ContractID: i.GetContractId(), ContractItemID: i.GetContractItemId(), Qty: i.GetQty(),
		})
	}
	return app.ShipmentInput{
		VesselName: in.GetVesselName(), VoyageNo: in.GetVoyageNo(), BlNo: in.GetBlNo(),
		ContainerNo: in.GetContainerNo(), PortOfDischarge: in.GetPortOfDischarge(),
		ETD: in.GetEtd(), ETA: in.GetEta(), Remark: in.GetRemark(), Items: items,
	}
}

func shipmentToProto(r store.GetShipmentRow) *exv1.Shipment {
	return &exv1.Shipment{
		Id: r.ID, ShipmentNo: r.ShipmentNo, VesselName: r.VesselName,
		VoyageNo: r.VoyageNo, BlNo: r.BlNo, ContainerNo: r.ContainerNo,
		PortOfDischarge: r.PortOfDischarge, Etd: r.Etd, Eta: r.Eta,
		Status: r.Status, Remark: r.Remark, CreatedByName: r.CreatedByName,
		CreatedAt: ts(r.CreatedAt), ShippedAt: ts(r.ShippedAt), ArrivedAt: ts(r.ArrivedAt),
	}
}

func shipmentItemsToProto(rows []store.ItemsOfShipmentRow) []*exv1.ShipmentItem {
	out := make([]*exv1.ShipmentItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.ShipmentItem{
			Id: r.ID, LineNo: r.LineNo, ContractId: r.ContractID, ContractNo: r.ContractNo,
			CustomerName: r.CustomerName, ContractItemId: r.ContractItemID,
			ProductId: r.ProductID, SkuId: r.SkuID, ProductCode: r.ProductCode,
			ProductName: r.ProductName, Spec: r.Spec, Qty: r.Qty, UomCode: r.UomCode,
		})
	}
	return out
}
