package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

func salesPlan(view app.SalesPlanView) *prv1.SalesPlan {
	h := view.Header
	items := make([]*prv1.SalesPlanItem, 0, len(view.Items))
	for _, row := range view.Items {
		items = append(items, &prv1.SalesPlanItem{Id: row.ID, SourcingLineId: row.SourcingLineID,
			ProcurementPlanItemId: row.ProcurementPlanItemID, ShippingPlanItemId: row.ShippingPlanItemID,
			OptionType: row.OptionType, Priority: row.Priority, ProductName: row.ProductName,
			QuotedQty: row.SpiQuotedQty, UomCode: row.UomCode, CustomerCurrency: row.CustomerCurrency,
			CustomerUnitPrice: row.SpiCustomerUnitPrice, PromisedDeliveryDate: row.PromisedDeliveryDate, LineNote: row.LineNote,
			SupplierId: row.SupplierID, SupplierName: row.SupplierName, FactoryId: row.FactoryID,
			FactoryName: row.FactoryName, PaymentTerms: row.PaymentTerms, Incoterm: row.Incoterm,
			SupplierValidUntil: row.SupplierValidUntil, ManagerSelectionType: row.ManagerSelectionType,
			ManagerReason: row.ManagerReason, ManagerRisk: row.ManagerRisk})
	}
	shippingOptions := make([]*prv1.SalesShippingOption, 0, len(view.ShippingOptions))
	for _, option := range view.ShippingOptions {
		h := option.Header
		lines := make([]*prv1.SalesShippingOptionLine, 0, len(option.Lines))
		for _, line := range option.Lines {
			lines = append(lines, &prv1.SalesShippingOptionLine{Id: line.ID, SourcingLineId: line.SourcingLineID,
				ShippingPlanItemId: line.ShippingPlanItemID, ShippingOptionLineId: line.ShippingOptionLineID, ProductName: line.ProductName,
				QuotedQty: line.QuotedQty, UomCode: line.UomCode})
		}
		shippingOptions = append(shippingOptions, &prv1.SalesShippingOption{Id: h.ID,
			ShippingOptionId: h.ShippingOptionID, CarrierForwarder: h.CarrierForwarder,
			ServiceOptionName: h.ServiceOptionName, ShippingEmployeeId: h.ShippingEmployeeID,
			ShippingEmployeeName: h.ShippingEmployeeName, CustomerCurrency: h.CustomerCurrency,
			CustomerFreightAmount: h.CustomerFreightAmount, ChargeBasis: h.ChargeBasis,
			PortOfLoading: h.PortOfLoading, PortOfDischarge: h.PortOfDischarge,
			EstimatedDeparture: h.EstimatedDeparture, EstimatedArrival: h.EstimatedArrival,
			ValidUntil: h.ValidUntil, CustomerNote: h.CustomerNote, Lines: lines})
	}
	return &prv1.SalesPlan{Id: h.ID, CaseId: h.CaseID, PlanNo: h.PlanNo, VersionNo: h.VersionNo,
		RequirementVersionNo: h.RequirementVersionNo, ProcurementPlanId: h.ProcurementPlanID,
		ShippingPlanId: h.ShippingPlanID, Status: h.Status, ValidUntil: h.ValidUntil,
		CustomerNote: h.CustomerNote, InternalNote: h.InternalNote, CreatedBy: h.CreatedBy,
		CreatedByName: h.CreatedByName, PresentedAt: ts(h.PresentedAt), CreatedAt: ts(h.CreatedAt), Items: items,
		ShippingOptions: shippingOptions}
}

func customerFeedback(row store.ListCustomerFeedbackRow) *prv1.CustomerFeedback {
	return &prv1.CustomerFeedback{Id: row.ID, CaseId: row.CaseID, SalesPlanId: row.SalesPlanID,
		ContactName: row.ContactName, Channel: row.Channel, Result: row.Result, Summary: row.Summary,
		ContactedAt: ts(row.ContactedAt), CreatedBy: row.CreatedBy, CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt)}
}

func shippingRework(row store.ListShippingReworksRow) *prv1.ShippingReworkRequest {
	return &prv1.ShippingReworkRequest{Id: row.ID, CaseId: row.CaseID, SalesPlanId: row.SalesPlanID,
		SourcingLineId: row.SourcingLineID, ShippingOptionLineId: row.ShippingOptionLineID,
		RequestType: row.RequestType, ScopeType: row.ScopeType, AssignedShippingId: row.AssignedShippingID,
		AssignedShippingName: row.AssignedShippingName, CarrierForwarder: row.CarrierForwarder,
		ProductName: row.ProductName, Reason: row.Reason, Status: row.Status, CreatedBy: row.CreatedBy,
		CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt), ResolvedBy: row.ResolvedBy,
		ResolvedByName: row.ResolvedByName, ResolvedAt: ts(row.ResolvedAt), ResolutionNote: row.ResolutionNote}
}

func myShippingRework(row store.ListMyShippingReworksRow) *prv1.ShippingReworkRequest {
	return &prv1.ShippingReworkRequest{Id: row.ID, CaseId: row.CaseID, SalesPlanId: row.SalesPlanID,
		SourcingLineId: row.SourcingLineID, ShippingOptionLineId: row.ShippingOptionLineID,
		RequestType: row.RequestType, ScopeType: row.ScopeType, AssignedShippingId: row.AssignedShippingID,
		AssignedShippingName: row.AssignedShippingName, CarrierForwarder: row.CarrierForwarder,
		ProductName: row.ProductName, Reason: row.Reason, Status: row.Status, CreatedBy: row.CreatedBy,
		CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt), ResolvedBy: row.ResolvedBy,
		ResolvedByName: row.ResolvedByName, ResolvedAt: ts(row.ResolvedAt), ResolutionNote: row.ResolutionNote,
		CaseNo: row.CaseNo, CaseTitle: row.CaseTitle, FinalRecheckTaskId: row.FinalRecheckTaskID}
}

func customerSelection(view app.CustomerSelectionView) *prv1.CustomerSelection {
	h := view.Header
	items := make([]*prv1.CustomerSelectionItem, 0, len(view.Items))
	for _, row := range view.Items {
		items = append(items, &prv1.CustomerSelectionItem{Id: row.ID, SalesPlanItemId: row.SalesPlanItemID,
			SourcingLineId: row.SourcingLineID, ProcurementPlanItemId: row.ProcurementPlanItemID,
			SupplierQuoteLineId: row.SupplierQuoteLineID, ShippingPlanItemId: row.ShippingPlanItemID,
			ShippingOptionLineId: row.ShippingOptionLineID, ProductName: row.ProductName,
			ConfirmedQty: row.ConfirmedQty, UomCode: row.UomCode, CustomerCurrency: row.CustomerCurrency,
			CustomerUnitPrice: row.CustomerUnitPrice, PromisedDeliveryDate: row.PromisedDeliveryDate, LineNote: row.LineNote,
			SupplierId: row.SupplierID, SupplierName: row.SupplierName, FactoryId: row.FactoryID,
			FactoryName: row.FactoryName, ShipmentGroupKey: row.ShipmentGroupKey,
			CustomerManagedShipping: row.CustomerManagedShipping, FinalCustomerCurrency: row.FinalCustomerCurrency, FinalCustomerUnitPrice: row.FinalCustomerUnitPrice})
	}
	shipments := make([]*prv1.CustomerSelectionShipment, 0, len(view.Shipments))
	for _, shipment := range view.Shipments {
		row := shipment.Header
		shipments = append(shipments, &prv1.CustomerSelectionShipment{Id: row.ID,
			SalesShippingOptionId: row.SalesShippingOptionID, ShippingOptionId: row.ShippingOptionID,
			ShipmentGroupKey: row.ShipmentGroupKey, CarrierForwarder: row.CarrierForwarder,
			ServiceOptionName: row.ServiceOptionName, ShippingEmployeeId: row.ShippingEmployeeID,
			ShippingEmployeeName: row.ShippingEmployeeName, CustomerCurrency: row.CustomerCurrency,
			CustomerFreightAmount: row.CustomerFreightAmount, ChargeBasis: row.ChargeBasis,
			PortOfLoading: row.PortOfLoading, PortOfDischarge: row.PortOfDischarge,
			EstimatedDeparture: row.EstimatedDeparture, EstimatedArrival: row.EstimatedArrival,
			ValidUntil: row.ValidUntil, CustomerNote: row.CustomerNote, SelectionItemIds: shipment.SelectionItemIDs,
			FinalCustomerCurrency: row.FinalCustomerCurrency, FinalCustomerFreightAmount: row.FinalCustomerFreightAmount})
	}
	tasks := make([]*prv1.FinalRecheckTask, 0, len(view.Tasks))
	for _, row := range view.Tasks {
		tasks = append(tasks, &prv1.FinalRecheckTask{Id: row.ID, SelectionItemId: row.SelectionItemID,
			SelectionShipmentId: row.SelectionShipmentID,
			TaskDomain:          row.TaskDomain, ProcurementReworkId: row.ProcurementReworkID,
			ShippingReworkId: row.ShippingReworkID, Status: row.Status, ResolvedAt: row.ResolvedAt,
			ResultNote: row.ResultNote, ResolvedBy: row.ResolvedBy, ResolvedByName: row.ResolvedByName,
			FinalCurrency: row.FinalCurrency, FinalUnitPrice: row.FinalUnitPrice, FinalAvailableQty: row.FinalAvailableQty,
			FinalLeadTime: row.FinalLeadTime, FinalDeliveryDate: row.FinalDeliveryDate, FinalPaymentTerms: row.FinalPaymentTerms,
			FinalIncoterm: row.FinalIncoterm, FinalValidUntil: row.FinalValidUntil, FinalFreightAmount: row.FinalFreightAmount,
			FinalEstimatedDeparture: row.FinalEstimatedDeparture, FinalEstimatedArrival: row.FinalEstimatedArrival})
	}
	return &prv1.CustomerSelection{Id: h.ID, CaseId: h.CaseID, SalesPlanId: h.SalesPlanID,
		SelectionNo: h.SelectionNo, VersionNo: h.VersionNo, RequirementVersionNo: h.RequirementVersionNo,
		Status: h.Status, CustomerContact: h.CustomerContact, ConfirmationNote: h.ConfirmationNote,
		CustomerConfirmedAt: ts(h.CustomerConfirmedAt), CreatedBy: h.CreatedBy, CreatedByName: h.CreatedByName,
		CreatedAt: ts(h.CreatedAt), FinalRecheckedAt: h.FinalRecheckedAt, InvalidatedAt: h.InvalidatedAt,
		InvalidatedReason: h.InvalidatedReason, Items: items, Shipments: shipments, RecheckTasks: tasks,
		CustomerDecidedAt: h.CustomerDecidedAt, CustomerDecisionNote: h.CustomerDecisionNote}
}

func (h *SourcingHandler) CreateSalesPlan(ctx context.Context, req *prv1.CreateSalesPlanRequest) (*prv1.CreateSalesPlanResponse, error) {
	in := app.NewSalesPlan{CaseID: req.GetCaseId(), ProcurementPlanID: req.GetProcurementPlanId(),
		ShippingPlanID: req.GetShippingPlanId(), ValidUntil: req.GetValidUntil(),
		CustomerNote: req.GetCustomerNote(), InternalNote: req.GetInternalNote()}
	for _, row := range req.GetItems() {
		in.Items = append(in.Items, app.SalesPlanItemInput{SourcingLineID: row.GetSourcingLineId(),
			ProcurementPlanItemID: row.GetProcurementPlanItemId(), ShippingPlanItemID: row.GetShippingPlanItemId(),
			OptionType: row.GetOptionType(), Priority: row.GetPriority(), CustomerCurrency: row.GetCustomerCurrency(),
			CustomerUnitPrice: row.GetCustomerUnitPrice(), PromisedDeliveryDate: row.GetPromisedDeliveryDate(), LineNote: row.GetLineNote()})
	}
	for _, row := range req.GetShippingOptions() {
		in.ShippingOptions = append(in.ShippingOptions, app.SalesShippingOptionInput{
			ShippingPlanItemIDs: row.GetShippingPlanItemIds(), CustomerCurrency: row.GetCustomerCurrency(),
			CustomerFreightAmount: row.GetCustomerFreightAmount(), CustomerNote: row.GetCustomerNote()})
	}
	view, err := h.svc.CreateSalesPlan(ctx, grpcx.TenantID(ctx), in, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSalesPlanResponse{SalesPlan: salesPlan(view)}, nil
}

func (h *SourcingHandler) ListSalesPlans(ctx context.Context, req *prv1.ListSalesPlansRequest) (*prv1.ListSalesPlansResponse, error) {
	rows, err := h.svc.ListSalesPlans(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SalesPlan, 0, len(rows))
	for _, row := range rows {
		out = append(out, salesPlan(row))
	}
	return &prv1.ListSalesPlansResponse{SalesPlans: out}, nil
}

func (h *SourcingHandler) AddCustomerFeedback(ctx context.Context, req *prv1.AddCustomerFeedbackRequest) (*prv1.AddCustomerFeedbackResponse, error) {
	rows, err := h.svc.AddCustomerFeedback(ctx, grpcx.TenantID(ctx), app.CustomerFeedbackInput{CaseID: req.GetCaseId(),
		SalesPlanID: req.GetSalesPlanId(), ContactName: req.GetContactName(), Channel: req.GetChannel(),
		Result: req.GetResult(), Summary: req.GetSummary(), ContactedAt: req.GetContactedAt()}, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.CustomerFeedback, 0, len(rows))
	for _, row := range rows {
		out = append(out, customerFeedback(row))
	}
	return &prv1.AddCustomerFeedbackResponse{Feedback: out}, nil
}

func (h *SourcingHandler) ListCustomerFeedback(ctx context.Context, req *prv1.ListCustomerFeedbackRequest) (*prv1.ListCustomerFeedbackResponse, error) {
	rows, err := h.svc.ListCustomerFeedback(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.CustomerFeedback, 0, len(rows))
	for _, row := range rows {
		out = append(out, customerFeedback(row))
	}
	return &prv1.ListCustomerFeedbackResponse{Feedback: out}, nil
}

func (h *SourcingHandler) CreateSalesProcurementRework(ctx context.Context, req *prv1.CreateSalesProcurementReworkRequest) (*prv1.CreateSalesProcurementReworkResponse, error) {
	row, err := h.svc.CreateSalesProcurementRework(ctx, grpcx.TenantID(ctx), req.GetSalesPlanId(), app.NewProcurementRework{
		CaseID: req.GetCaseId(), SourcingLineID: req.GetSourcingLineId(), SupplierQuoteLineID: req.GetSupplierQuoteLineId(),
		RequestType: req.GetRequestType(), Reason: req.GetReason()}, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSalesProcurementReworkResponse{ReworkRequest: procurementRework(row)}, nil
}

func (h *SourcingHandler) CreateShippingRework(ctx context.Context, req *prv1.CreateShippingReworkRequest) (*prv1.CreateShippingReworkResponse, error) {
	row, err := h.svc.CreateShippingRework(ctx, grpcx.TenantID(ctx), app.ShippingReworkInput{CaseID: req.GetCaseId(),
		SalesPlanID: req.GetSalesPlanId(), SourcingLineID: req.GetSourcingLineId(), ShippingOptionLineID: req.GetShippingOptionLineId(),
		RequestType: req.GetRequestType(), Reason: req.GetReason()}, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.CreateShippingReworkResponse{ReworkRequest: shippingRework(row)}, nil
}

func (h *SourcingHandler) ListShippingReworks(ctx context.Context, req *prv1.ListShippingReworksRequest) (*prv1.ListShippingReworksResponse, error) {
	rows, err := h.svc.ListShippingReworks(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.ShippingReworkRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, shippingRework(row))
	}
	return &prv1.ListShippingReworksResponse{ReworkRequests: out}, nil
}

func (h *SourcingHandler) ListMyShippingReworks(ctx context.Context, _ *prv1.ListMyShippingReworksRequest) (*prv1.ListMyShippingReworksResponse, error) {
	rows, err := h.svc.ListMyShippingReworks(ctx, grpcx.TenantID(ctx), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.ShippingReworkRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, myShippingRework(row))
	}
	return &prv1.ListMyShippingReworksResponse{ReworkRequests: out}, nil
}

func (h *SourcingHandler) ResolveShippingRework(ctx context.Context, req *prv1.ResolveShippingReworkRequest) (*prv1.ResolveShippingReworkResponse, error) {
	in := app.ShippingReworkResolution{Note: req.GetResolutionNote(), Currency: req.GetFinalCurrency(), FreightAmount: req.GetFinalFreightAmount(), EstimatedDeparture: req.GetFinalEstimatedDeparture(), EstimatedArrival: req.GetFinalEstimatedArrival(), ValidUntil: req.GetFinalValidUntil()}
	if err := h.svc.ResolveShippingRework(ctx, grpcx.TenantID(ctx), req.GetId(), in, sourcingOperator(ctx)); err != nil {
		return nil, err
	}
	return &prv1.ResolveShippingReworkResponse{}, nil
}

func (h *SourcingHandler) ConfirmCustomerSelection(ctx context.Context, req *prv1.ConfirmCustomerSelectionRequest) (*prv1.ConfirmCustomerSelectionResponse, error) {
	in := app.ConfirmCustomerSelectionInput{
		CaseID: req.GetCaseId(), SalesPlanID: req.GetSalesPlanId(), SalesPlanItemIDs: req.GetSalesPlanItemIds(),
		CustomerContact: req.GetCustomerContact(), ConfirmationNote: req.GetConfirmationNote(),
		CustomerConfirmedAt: req.GetCustomerConfirmedAt()}
	for _, row := range req.GetShipmentChoices() {
		in.ShipmentChoices = append(in.ShipmentChoices, app.CustomerShipmentChoiceInput{
			ShipmentGroupKey: row.GetShipmentGroupKey(), SalesShippingOptionID: row.GetSalesShippingOptionId(), CustomerManaged: row.GetCustomerManaged()})
	}
	view, err := h.svc.ConfirmCustomerSelection(ctx, grpcx.TenantID(ctx), in, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ConfirmCustomerSelectionResponse{Selection: customerSelection(view)}, nil
}

func (h *SourcingHandler) DecideCustomerSelection(ctx context.Context, req *prv1.DecideCustomerSelectionRequest) (*prv1.DecideCustomerSelectionResponse, error) {
	in := app.DecideCustomerSelectionInput{CaseID: req.GetCaseId(), SelectionID: req.GetSelectionId(), Accepted: req.GetAccepted(), CustomerContact: req.GetCustomerContact(), DecisionNote: req.GetDecisionNote(), DecidedAt: req.GetCustomerDecidedAt()}
	for _, row := range req.GetItemPrices() {
		in.ItemPrices = append(in.ItemPrices, app.FinalCustomerItemPriceInput{SelectionItemID: row.GetSelectionItemId(), Currency: row.GetCurrency(), UnitPrice: row.GetUnitPrice()})
	}
	for _, row := range req.GetShipmentPrices() {
		in.ShipmentPrices = append(in.ShipmentPrices, app.FinalCustomerShipmentPriceInput{SelectionShipmentID: row.GetSelectionShipmentId(), Currency: row.GetCurrency(), FreightAmount: row.GetFreightAmount()})
	}
	view, err := h.svc.DecideCustomerSelection(ctx, grpcx.TenantID(ctx), in, sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.DecideCustomerSelectionResponse{Selection: customerSelection(view)}, nil
}

func (h *SourcingHandler) ListCustomerSelections(ctx context.Context, req *prv1.ListCustomerSelectionsRequest) (*prv1.ListCustomerSelectionsResponse, error) {
	rows, err := h.svc.ListCustomerSelections(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.CustomerSelection, 0, len(rows))
	for _, row := range rows {
		out = append(out, customerSelection(row))
	}
	return &prv1.ListCustomerSelectionsResponse{Selections: out}, nil
}
