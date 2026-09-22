package grpcin

import (
	"context"
	shippingv1 "github.com/sgao19/erp-go/gen/go/erp/shipping/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/shipping/internal/app"
)

func (h *Handler) SaveManualShippingOrder(ctx context.Context, req *shippingv1.SaveManualShippingOrderRequest) (*shippingv1.SaveManualShippingOrderResponse, error) {
	r := req.GetOrder()
	if r == nil {
		return nil, apierr.Invalid("MANUAL_ORDER_REQUIRED", "请填写物流实单资料")
	}
	// Only editable snapshots are accepted. IDs for contracts, customers, cargo
	// and workflow state supplied by a client are deliberately never imported.
	v := app.ContractHandoff{LinkedContractID: r.LinkedContractId, ID: r.Id, ManualOrderNo: r.ManualOrderNo, ContractNo: r.ContractNo, CustomerName: r.CustomerName, PortOfLoading: r.PortOfLoading, PortOfDischarge: r.PortOfDischarge, FinalForwarderID: r.FinalForwarderId, FinalForwarderName: r.FinalForwarderName, ActualCarrierID: r.ActualCarrierId, ActualCarrierName: r.ActualCarrierName, FinalServiceOption: r.FinalServiceOption, FinalCurrency: r.FinalCurrency, FinalFreightAmount: r.FinalFreightAmount, FinalETD: r.FinalEtd, FinalETA: r.FinalEta, PaymentTerms: r.PaymentTerms, ForwarderContractNo: r.ForwarderContractNo, Remark: r.Remark}
	for _, c := range r.CargoItems {
		if c == nil {
			continue
		}
		v.CargoItems = append(v.CargoItems, app.ContractShippingCargoItem{ProductCode: c.ProductCode, ProductName: c.ProductName, Specification: c.Specification, Quantity: c.Quantity, UomCode: c.UomCode, Remark: c.Remark})
	}
	out, err := h.svc.SaveManualShippingOrder(ctx, grpcx.TenantID(ctx), v, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.SaveManualShippingOrderResponse{Order: handoffToProto(out)}, nil
}

func (h *Handler) LinkManualShippingContract(ctx context.Context, r *shippingv1.LinkManualShippingContractRequest) (*shippingv1.LinkManualShippingContractResponse, error) {
	v, err := h.svc.LinkManualShippingContract(ctx, grpcx.TenantID(ctx), r.Id, r.ContractId, r.ContractNo, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &shippingv1.LinkManualShippingContractResponse{Order: handoffToProto(v)}, nil
}
