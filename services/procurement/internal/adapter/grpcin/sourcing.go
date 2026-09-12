package grpcin

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// removeRepeatedProductName 兼容旧 RFQ 快照：移除重复产品名称，并清理空规格段。
func removeRepeatedProductName(productName, spec string) string {
	productName = strings.TrimSpace(productName)
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	if productName != "" {
		for _, separator := range []string{" / ", "/"} {
			prefix := productName + separator
			if strings.HasPrefix(spec, prefix) {
				spec = strings.TrimSpace(strings.TrimPrefix(spec, prefix))
				break
			}
		}
	}
	parts := strings.Split(spec, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, " / ")
}

type SourcingHandler struct {
	prv1.UnimplementedSourcingServiceServer
	svc *app.Service
}

func NewSourcing(svc *app.Service) *SourcingHandler { return &SourcingHandler{svc: svc} }

func sourcingOperator(ctx context.Context) app.Operator {
	op, _ := grpcx.OperatorFromContext(ctx)
	return app.Operator{ID: op.EmployeeID, Name: op.Name}
}

func (h *SourcingHandler) authorizeCase(ctx context.Context, caseID int64) error {
	return h.svc.AuthorizeSourcingCase(ctx, grpcx.TenantID(ctx), caseID, sourcingOperator(ctx))
}

func (h *SourcingHandler) authorizeRFQ(ctx context.Context, rfqID int64) error {
	return h.svc.AuthorizeFactoryRFQ(ctx, grpcx.TenantID(ctx), rfqID, sourcingOperator(ctx))
}

func (h *SourcingHandler) authorizeScenario(ctx context.Context, scenarioID int64) error {
	return h.svc.AuthorizeCostScenario(ctx, grpcx.TenantID(ctx), scenarioID, sourcingOperator(ctx))
}

func (h *SourcingHandler) authorizePlan(ctx context.Context, planID int64) error {
	return h.svc.AuthorizeProcurementPlan(ctx, grpcx.TenantID(ctx), planID, sourcingOperator(ctx))
}

func (h *SourcingHandler) CreateCase(ctx context.Context, req *prv1.CreateCaseRequest) (*prv1.CreateCaseResponse, error) {
	if err := h.svc.AuthorizeInquiryCreate(ctx, sourcingOperator(ctx)); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.SourcingLineInput, 0, len(req.GetLines()))
	for _, line := range req.GetLines() {
		lines = append(lines, lineInput(line))
	}
	view, err := h.svc.CreateSourcingCase(ctx, grpcx.TenantID(ctx), app.NewSourcingCase{
		Title: req.GetTitle(), CustomerID: req.GetCustomerId(), CustomerName: req.GetCustomerName(),
		ContactID: req.GetContactId(), ContactName: req.GetContactName(), ContactEmail: req.GetContactEmail(),
		SourceMailID: req.GetSourceMailId(), SourceAttachmentID: req.GetSourceAttachmentId(), Lines: lines,
		SourceFileName: req.GetSourceFileName(), SourceContentType: req.GetSourceContentType(), SourceFileData: req.GetSourceFileData(),
		InquiryTemplateID: req.GetInquiryTemplateId(), InquiryTemplateCode: req.GetInquiryTemplateCode(),
		InquiryTemplateVersion: req.GetInquiryTemplateVersion(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateCaseResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) ListCases(ctx context.Context, req *prv1.ListCasesRequest) (*prv1.ListCasesResponse, error) {
	return nil, apierr.Conflict("INQUIRY_LIST_REPLACED", "请使用当前客户询盘或部门询价列表")
}

func (h *SourcingHandler) GetCase(ctx context.Context, req *prv1.GetCaseRequest) (*prv1.GetCaseResponse, error) {
	if err := h.authorizeCase(ctx, req.GetId()); err != nil {
		return nil, err
	}
	view, err := h.svc.GetSourcingCase(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetCaseResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) AddLine(ctx context.Context, req *prv1.AddLineRequest) (*prv1.AddLineResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ConfirmLines(ctx context.Context, req *prv1.ConfirmLinesRequest) (*prv1.ConfirmLinesResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) AcceptCase(ctx context.Context, req *prv1.AcceptCaseRequest) (*prv1.AcceptCaseResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListCaseParticipants(ctx context.Context, req *prv1.ListCaseParticipantsRequest) (*prv1.ListCaseParticipantsResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	items, err := h.svc.ListSourcingParticipants(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	return &prv1.ListCaseParticipantsResponse{Participants: sourcingParticipants(items)}, nil
}

func (h *SourcingHandler) JoinCase(ctx context.Context, req *prv1.JoinCaseRequest) (*prv1.JoinCaseResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) RequestPrimaryBuyer(ctx context.Context, req *prv1.RequestPrimaryBuyerRequest) (*prv1.RequestPrimaryBuyerResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) AssignPrimaryBuyer(ctx context.Context, req *prv1.AssignPrimaryBuyerRequest) (*prv1.AssignPrimaryBuyerResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ReturnCase(ctx context.Context, req *prv1.ReturnCaseRequest) (*prv1.ReturnCaseResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) WithdrawCase(ctx context.Context, req *prv1.WithdrawCaseRequest) (*prv1.WithdrawCaseResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ReviewLine(ctx context.Context, req *prv1.ReviewLineRequest) (*prv1.ReviewLineResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) CreateFactoryRfq(ctx context.Context, req *prv1.CreateFactoryRfqRequest) (*prv1.CreateFactoryRfqResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListCaseChanges(ctx context.Context, req *prv1.ListCaseChangesRequest) (*prv1.ListCaseChangesResponse, error) {
	return &prv1.ListCaseChangesResponse{}, nil
}

func (h *SourcingHandler) ListOverdueFactoryRfqs(ctx context.Context, req *prv1.ListOverdueFactoryRfqsRequest) (*prv1.ListOverdueFactoryRfqsResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, err := h.svc.ListOverdueFactoryRFQs(ctx, grpcx.TenantID(ctx), req.GetLimit(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.OverdueFactoryRfq, 0, len(rows))
	for _, row := range rows {
		out = append(out, &prv1.OverdueFactoryRfq{
			Id: row.ID, RfqNo: row.RfqNo, CaseId: row.CaseID, CaseNo: row.CaseNo,
			SupplierName: row.SupplierName, ResponseDueAt: row.ResponseDueAt,
			OverdueDays: row.OverdueDays,
		})
	}
	return &prv1.ListOverdueFactoryRfqsResponse{Items: out}, nil
}

func (h *SourcingHandler) UpdateFactoryRfq(ctx context.Context, req *prv1.UpdateFactoryRfqRequest) (*prv1.UpdateFactoryRfqResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListFactoryRfqs(ctx context.Context, req *prv1.ListFactoryRfqsRequest) (*prv1.ListFactoryRfqsResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	rows, err := h.svc.ListFactoryRFQs(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.FactoryRfq, 0, len(rows))
	for _, row := range rows {
		out = append(out, factoryRFQ(row))
	}
	return &prv1.ListFactoryRfqsResponse{FactoryRfqs: out}, nil
}

func (h *SourcingHandler) CreateSupplierQuote(ctx context.Context, req *prv1.CreateSupplierQuoteRequest) (*prv1.CreateSupplierQuoteResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) GetFactoryRfqWorkbook(ctx context.Context, req *prv1.GetFactoryRfqWorkbookRequest) (*prv1.GetFactoryRfqWorkbookResponse, error) {
	if err := h.authorizeRFQ(ctx, req.GetId()); err != nil {
		return nil, err
	}
	book, err := h.svc.GetFactoryRFQWorkbook(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetFactoryRfqWorkbookResponse{FileName: book.FileName, FileData: book.Data, RfqNo: book.RFQNo,
		SupplierName: book.SupplierName, ContactEmail: book.ContactEmail, Currency: book.Currency, ResponseDueAt: book.ResponseDueAt}, nil
}

func (h *SourcingHandler) ImportSupplierQuoteWorkbook(ctx context.Context, req *prv1.ImportSupplierQuoteWorkbookRequest) (*prv1.ImportSupplierQuoteWorkbookResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) MarkFactoryRfqSent(ctx context.Context, req *prv1.MarkFactoryRfqSentRequest) (*prv1.MarkFactoryRfqSentResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListSupplierQuoteComparison(ctx context.Context, req *prv1.ListSupplierQuoteComparisonRequest) (*prv1.ListSupplierQuoteComparisonResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	rows, err := h.svc.ListSupplierQuoteComparison(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SupplierQuoteComparisonLine, 0, len(rows))
	for _, v := range rows {
		row := v.Row
		out = append(out, &prv1.SupplierQuoteComparisonLine{
			QuoteId: row.QuoteID, QuoteLineId: row.QuoteLineID, SupplierQuoteNo: row.SupplierQuoteNo,
			FactoryRfqId: row.FactoryRfqID, SupplierId: row.SupplierID, SupplierName: row.SupplierName,
			Currency: row.Currency, QuotedAt: row.QuotedAt, ValidUntil: row.ValidUntil,
			PaymentTerms: row.PaymentTerms, Delivery: row.Delivery, Incoterm: row.Incoterm, QuoteRemark: row.Remark,
			Source: row.Source, VersionNo: row.VersionNo, ConfirmationStatus: row.ConfirmationStatus,
			EvidenceNote: row.EvidenceNote, SourcingLineId: row.SourcingLineID, Qty: row.LQty,
			UnitPrice: row.LUnitPrice, Amount: row.LAmount, Moq: row.Moq,
			LeadTime: row.LeadTime, LineRemark: row.LineRemark,
			ComparePrice: v.ComparePrice, CompareCurrency: v.CompareCurrency,
			CreatedBy: row.CreatedBy, CreatedByName: row.CreatedByName,
		})
	}
	return &prv1.ListSupplierQuoteComparisonResponse{Lines: out}, nil
}

func (h *SourcingHandler) CreateProcurementPlan(ctx context.Context, req *prv1.CreateProcurementPlanRequest) (*prv1.CreateProcurementPlanResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListProcurementPlans(ctx context.Context, req *prv1.ListProcurementPlansRequest) (*prv1.ListProcurementPlansResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	views, err := h.svc.ListProcurementPlans(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.ProcurementPlan, 0, len(views))
	for _, view := range views {
		out = append(out, procurementPlanView(view))
	}
	return &prv1.ListProcurementPlansResponse{ProcurementPlans: out}, nil
}

func (h *SourcingHandler) GetProcurementPlan(ctx context.Context, req *prv1.GetProcurementPlanRequest) (*prv1.GetProcurementPlanResponse, error) {
	if err := h.authorizePlan(ctx, req.GetId()); err != nil {
		return nil, err
	}
	view, err := h.svc.GetProcurementPlan(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetProcurementPlanResponse{ProcurementPlan: procurementPlanView(view)}, nil
}

func (h *SourcingHandler) SubmitProcurementPlanToSales(ctx context.Context, req *prv1.SubmitProcurementPlanToSalesRequest) (*prv1.SubmitProcurementPlanToSalesResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) CreateProcurementRework(ctx context.Context, req *prv1.CreateProcurementReworkRequest) (*prv1.CreateProcurementReworkResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListProcurementReworks(ctx context.Context, req *prv1.ListProcurementReworksRequest) (*prv1.ListProcurementReworksResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	rows, err := h.svc.ListProcurementReworks(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.ProcurementReworkRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, procurementRework(row))
	}
	return &prv1.ListProcurementReworksResponse{ReworkRequests: out}, nil
}

func (h *SourcingHandler) ResolveProcurementRework(ctx context.Context, req *prv1.ResolveProcurementReworkRequest) (*prv1.ResolveProcurementReworkResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func sourcingShippingRequest(row store.SourcingShippingRequest) *prv1.SourcingShippingRequest {
	return &prv1.SourcingShippingRequest{Id: row.ID, CaseId: row.CaseID, RequirementVersionNo: row.RequirementVersionNo,
		Status: row.Status, CaseNo: row.CaseNo, CaseTitle: row.CaseTitle, CustomerId: row.CustomerID, CustomerName: row.CustomerName,
		SalesEmployeeId: row.SalesEmployeeID, SalesEmployeeName: row.SalesEmployeeName, DestinationPort: row.DestinationPort,
		CargoSummary: row.CargoSummary, RequestedByName: row.RequestedByName, RequestedAt: ts(row.RequestedAt), UpdatedAt: ts(row.UpdatedAt)}
}

func sourcingShippingTask(row store.ListSourcingShippingRequestsRow) *prv1.SourcingShippingRequest {
	return &prv1.SourcingShippingRequest{Id: row.ID, CaseId: row.CaseID, RequirementVersionNo: row.RequirementVersionNo,
		Status: row.Status, CaseNo: row.CaseNo, CaseTitle: row.CaseTitle, CustomerId: row.CustomerID, CustomerName: row.CustomerName,
		SalesEmployeeId: row.SalesEmployeeID, SalesEmployeeName: row.SalesEmployeeName, DestinationPort: row.DestinationPort,
		CargoSummary: row.CargoSummary, RequestedByName: row.RequestedByName, RequestedAt: ts(row.RequestedAt), UpdatedAt: ts(row.UpdatedAt)}
}

func sourcingShippingOption(view app.SourcingShippingOption) *prv1.SourcingShippingOption {
	row := view.Option
	lines := make([]*prv1.SourcingShippingOptionLine, 0, len(view.Lines))
	for _, line := range view.Lines {
		lines = append(lines, &prv1.SourcingShippingOptionLine{Id: line.ID, OptionId: line.OptionID, SourcingLineId: line.SourcingLineID,
			LineNo: line.LineNo, Product: line.ProductSnapshot, Specification: line.SpecificationSnapshot, Quantity: line.Quantity,
			QuantityUnit: line.QuantityUnit, Currency: line.Currency, ChargeBasis: line.ChargeBasis, UnitRate: line.UnitRate,
			TotalFreight: line.TotalFreight, Note: line.Note})
	}
	return &prv1.SourcingShippingOption{Id: row.ID, RequestId: row.RequestID, CarrierForwarder: row.CarrierForwarder, ServiceOptionName: row.ServiceOptionName,
		PortOfLoading: row.PortOfLoading, PortOfDischarge: row.PortOfDischarge, QuotedAt: row.QuotedAt,
		EstimatedDeparture: row.EstimatedDeparture, EstimatedArrival: row.EstimatedArrival, ValidUntil: row.ValidUntil,
		Note: row.Note, CreatedBy: row.CreatedBy, CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt),
		VersionNo: row.VersionNo, Status: row.Status, Lines: lines}
}

func sourcingShippingParticipant(row store.ListSourcingShippingParticipantsRow) *prv1.SourcingShippingParticipant {
	return &prv1.SourcingShippingParticipant{Id: row.ID, RequestId: row.RequestID, EmployeeId: row.EmployeeID,
		EmployeeName: row.EmployeeName, Role: row.ParticipantRole, Status: row.Status,
		PrimaryRequestedAt: ts(row.PrimaryRequestedAt), JoinedAt: ts(row.JoinedAt)}
}

func sourcingShippingPlan(view app.ShippingPlanView) *prv1.SourcingShippingPlan {
	h := view.Header
	items := make([]*prv1.SourcingShippingPlanItem, 0, len(view.Items))
	for _, row := range view.Items {
		items = append(items, &prv1.SourcingShippingPlanItem{Id: row.ID, SourcingLineId: row.SourcingLineID,
			ShippingOptionLineId: row.ShippingOptionLineID, ShippingOptionId: row.ShippingOptionID, SelectionType: row.SelectionType, Priority: row.Priority,
			Reason: row.Reason, Risk: row.Risk, CarrierForwarder: row.CarrierForwarder, ServiceOptionName: row.ServiceOptionName,
			ShippingEmployeeId: row.ShippingEmployeeID, ShippingEmployeeName: row.ShippingEmployeeName,
			ProductName: row.ProductName, Currency: row.Currency, ChargeBasis: row.ChargeBasis,
			UnitRate: row.UnitRate, TotalFreight: row.TotalFreight, PortOfLoading: row.PortOfLoading,
			PortOfDischarge: row.PortOfDischarge, EstimatedDeparture: row.EstimatedDeparture,
			EstimatedArrival: row.EstimatedArrival, ValidUntil: row.ValidUntil, QuoteVersionNo: row.QuoteVersionNo})
	}
	return &prv1.SourcingShippingPlan{Id: h.ID, RequestId: h.RequestID, PlanNo: h.PlanNo, VersionNo: h.VersionNo,
		RequirementVersionNo: h.RequirementVersionNo, Status: h.Status, ManagerNote: h.ManagerNote,
		CreatedByName: h.CreatedByName, ConfirmedAt: ts(h.ConfirmedAt), SubmittedToSalesByName: h.SubmittedToSalesByName,
		SubmittedToSalesAt: ts(h.SubmittedToSalesAt), TargetSalesId: h.TargetSalesID, TargetSalesName: h.TargetSalesName, Items: items}
}

func shippingCollaboration(view app.SourcingShippingCollaboration) (*prv1.SourcingShippingRequest, []*prv1.SourcingShippingOption, []*prv1.SourcingShippingCargoItem) {
	options := make([]*prv1.SourcingShippingOption, 0, len(view.Options))
	for _, row := range view.Options {
		options = append(options, sourcingShippingOption(row))
	}
	cargo := make([]*prv1.SourcingShippingCargoItem, 0, len(view.CargoItems))
	for _, row := range view.CargoItems {
		cargo = append(cargo, &prv1.SourcingShippingCargoItem{SourcingLineId: row.SourcingLineID, LineNo: row.LineNo, Product: row.Product,
			MaterialStandard: row.MaterialStandard, Grade: row.Grade, Thickness: row.Thickness, Width: row.Width,
			LengthOrForm: row.LengthOrForm, SurfaceRequirement: row.SurfaceRequirement, Packaging: row.Packaging,
			Delivery: row.Delivery, Quantity: row.Quantity, QuantityUnit: row.QuantityUnit})
	}
	return sourcingShippingRequest(view.Request), options, cargo
}

func (h *SourcingHandler) GetSourcingShippingCollaboration(ctx context.Context, req *prv1.GetSourcingShippingCollaborationRequest) (*prv1.GetSourcingShippingCollaborationResponse, error) {
	view, err := h.svc.GetSourcingShippingCollaboration(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	request, options, cargo := shippingCollaboration(view)
	participants := make([]*prv1.SourcingShippingParticipant, 0, len(view.Participants))
	for _, row := range view.Participants {
		participants = append(participants, sourcingShippingParticipant(row))
	}
	plans := make([]*prv1.SourcingShippingPlan, 0, len(view.Plans))
	for _, row := range view.Plans {
		plans = append(plans, sourcingShippingPlan(row))
	}
	return &prv1.GetSourcingShippingCollaborationResponse{ShippingRequest: request, Options: options, CargoItems: cargo, Participants: participants, Plans: plans}, nil
}

func (h *SourcingHandler) JoinSourcingShippingTask(ctx context.Context, req *prv1.JoinSourcingShippingTaskRequest) (*prv1.JoinSourcingShippingTaskResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) RequestPrimaryShipping(ctx context.Context, req *prv1.RequestPrimaryShippingRequest) (*prv1.RequestPrimaryShippingResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) AssignPrimaryShipping(ctx context.Context, req *prv1.AssignPrimaryShippingRequest) (*prv1.AssignPrimaryShippingResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) CreateSourcingShippingPlan(ctx context.Context, req *prv1.CreateSourcingShippingPlanRequest) (*prv1.CreateSourcingShippingPlanResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListSourcingShippingPlans(ctx context.Context, req *prv1.ListSourcingShippingPlansRequest) (*prv1.ListSourcingShippingPlansResponse, error) {
	rows, err := h.svc.ListSourcingShippingPlans(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SourcingShippingPlan, 0, len(rows))
	for _, row := range rows {
		out = append(out, sourcingShippingPlan(row))
	}
	return &prv1.ListSourcingShippingPlansResponse{Plans: out}, nil
}

func (h *SourcingHandler) SubmitSourcingShippingPlanToSales(ctx context.Context, req *prv1.SubmitSourcingShippingPlanToSalesRequest) (*prv1.SubmitSourcingShippingPlanToSalesResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListSourcingShippingTasks(ctx context.Context, req *prv1.ListSourcingShippingTasksRequest) (*prv1.ListSourcingShippingTasksResponse, error) {
	page, size := int32(1), int32(20)
	if req.GetPage() != nil {
		page, size = req.GetPage().GetPage(), req.GetPage().GetPageSize()
	}
	rows, total, page, size, err := h.svc.ListSourcingShippingTasks(ctx, grpcx.TenantID(ctx), req.GetStatus(), req.GetKeyword(), page, size)
	if err != nil {
		return nil, err
	}
	tasks := make([]*prv1.SourcingShippingRequest, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, sourcingShippingTask(row))
	}
	return &prv1.ListSourcingShippingTasksResponse{Tasks: tasks, Meta: &commonv1.PageMeta{Total: total, Page: page, PageSize: size}}, nil
}

func (h *SourcingHandler) StartSourcingShippingTask(ctx context.Context, req *prv1.StartSourcingShippingTaskRequest) (*prv1.StartSourcingShippingTaskResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) AddSourcingShippingOption(ctx context.Context, req *prv1.AddSourcingShippingOptionRequest) (*prv1.AddSourcingShippingOptionResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) CreateCostScenario(ctx context.Context, req *prv1.CreateCostScenarioRequest) (*prv1.CreateCostScenarioResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) ListCostScenarios(ctx context.Context, req *prv1.ListCostScenariosRequest) (*prv1.ListCostScenariosResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	rows, err := h.svc.ListCostScenarios(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.CostScenario, 0, len(rows))
	for _, row := range rows {
		out = append(out, costScenarioList(row))
	}
	return &prv1.ListCostScenariosResponse{CostScenarios: out}, nil
}

func (h *SourcingHandler) GetCostScenario(ctx context.Context, req *prv1.GetCostScenarioRequest) (*prv1.GetCostScenarioResponse, error) {
	if err := h.authorizeScenario(ctx, req.GetId()); err != nil {
		return nil, err
	}
	view, err := h.svc.GetCostScenario(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetCostScenarioResponse{CostScenario: costScenarioView(view)}, nil
}

func (h *SourcingHandler) ConfirmCostScenario(ctx context.Context, req *prv1.ConfirmCostScenarioRequest) (*prv1.ConfirmCostScenarioResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) SubmitCostToSales(ctx context.Context, req *prv1.SubmitCostToSalesRequest) (*prv1.SubmitCostToSalesResponse, error) {
	return nil, apierr.Conflict("INQUIRY_WORKFLOW_RETIRED", "原询价操作已退出，请进入客户询盘、采购询价或物流询价处理")
}

func (h *SourcingHandler) PrepareCustomerQuotation(ctx context.Context, req *prv1.PrepareCustomerQuotationRequest) (*prv1.PrepareCustomerQuotationResponse, error) {
	if err := h.authorizeScenario(ctx, req.GetId()); err != nil {
		return nil, err
	}
	draft, err := h.svc.PrepareCustomerQuotation(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	out := &prv1.PrepareCustomerQuotationResponse{
		CostScenarioId: draft.Scenario.ID, CostScenarioNo: draft.Scenario.ScenarioNo,
		SourcingCaseId: draft.Case.ID, CustomerId: draft.Case.CustomerID, ContactEmail: draft.Case.ContactEmail,
		Currency: draft.Scenario.Currency, Incoterm: draft.Terms.Incoterm, PortOfDischarge: draft.Terms.Port,
		PaymentMethod: draft.Terms.PaymentTerms, ExistingQuotationId: draft.Scenario.CustomerQuotationID,
		ExistingQuoteNo: draft.Scenario.CustomerQuoteNo,
	}
	for _, line := range draft.Lines {
		out.Lines = append(out.Lines, &prv1.CustomerQuotationLine{
			CostScenarioLineId: line.ID, ProductId: line.ProductID, SkuId: line.SkuID,
			Spec: removeRepeatedProductName(line.ProductName, line.SpecSnapshot), Qty: line.Qty, UnitPrice: line.CustomerUnitPrice,
			Remark: line.SupplierName, ProductName: line.ProductName, UomCode: line.UomCode,
		})
	}
	return out, nil
}

func (h *SourcingHandler) LinkCustomerQuotation(ctx context.Context, req *prv1.LinkCustomerQuotationRequest) (*prv1.LinkCustomerQuotationResponse, error) {
	if err := h.authorizeScenario(ctx, req.GetId()); err != nil {
		return nil, err
	}
	if err := h.svc.LinkCustomerQuotation(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetQuotationId(), req.GetQuoteNo()); err != nil {
		return nil, err
	}
	return &prv1.LinkCustomerQuotationResponse{QuotationId: req.GetQuotationId(), QuoteNo: req.GetQuoteNo()}, nil
}

func procurementPlanView(view app.ProcurementPlanView) *prv1.ProcurementPlan {
	h := view.Header
	out := &prv1.ProcurementPlan{
		Id: h.ID, CaseId: h.CaseID, PlanNo: h.PlanNo, VersionNo: h.VersionNo,
		RequirementVersionNo: h.RequirementVersionNo, Status: h.Status, ManagerNote: h.ManagerNote,
		CreatedByName: h.CreatedByName, ConfirmedByName: h.ConfirmedByName, ConfirmedAt: h.ConfirmedAt,
		SubmittedToSalesByName: h.SubmittedToSalesByName, SubmittedToSalesAt: h.SubmittedToSalesAt,
		TargetSalesId: h.TargetSalesID, TargetSalesName: h.TargetSalesName, CreatedAt: h.CreatedAt,
	}
	for _, item := range view.Items {
		out.Items = append(out.Items, &prv1.ProcurementPlanItem{
			Id: item.ID, SourcingLineId: item.SourcingLineID, SupplierQuoteLineId: item.SupplierQuoteLineID,
			SelectionType: item.SelectionType, Priority: item.Priority, Reason: item.Reason, Risk: item.Risk,
			SupplierId: item.SupplierID, SupplierName: item.SupplierName, FactoryId: item.FactoryID,
			FactoryName: item.FactoryName, BuyerId: item.BuyerID, BuyerName: item.BuyerName,
			ProductName: item.ProductName, Currency: item.Currency, UnitPrice: item.UnitPrice,
			AvailableQty: item.AvailableQty, UomCode: item.UomCode, Moq: item.Moq, LeadTime: item.LeadTime,
			PaymentTerms: item.PaymentTerms, Incoterm: item.Incoterm, ValidUntil: item.ValidUntil,
			QuoteVersionNo: item.QuoteVersionNo,
		})
	}
	return out
}

func procurementRework(row store.ListProcurementReworkRequestsRow) *prv1.ProcurementReworkRequest {
	return &prv1.ProcurementReworkRequest{
		Id: row.ID, CaseId: row.CaseID, PlanId: row.PlanID, SourcingLineId: row.SourcingLineID,
		SupplierQuoteLineId: row.SupplierQuoteLineID, RequestType: row.RequestType, ScopeType: row.ScopeType,
		AssignedBuyerId: row.AssignedBuyerID, AssignedBuyerName: row.AssignedBuyerName,
		SupplierId: row.SupplierID, SupplierName: row.SupplierName, ProductName: row.ProductName,
		Reason: row.Reason, Status: row.Status, CreatedByName: row.CreatedByName, CreatedAt: ts(row.CreatedAt),
		ResolvedByName: row.ResolvedByName, ResolvedAt: ts(row.ResolvedAt), ResolutionNote: row.ResolutionNote,
		FinalRecheckTaskId: row.FinalRecheckTaskID, CustomerIntentQty: row.CustomerIntentQty,
	}
}

func costScenarioList(row store.ListCostScenariosRow) *prv1.CostScenario {
	return &prv1.CostScenario{
		Id: row.ID, CaseId: row.CaseID, ScenarioNo: row.ScenarioNo, Currency: row.Currency,
		AllocationBasis: row.AllocationBasis, MarginType: row.MarginType, MarginValue: row.MarginValue,
		FxRate: row.FxRate, FxRateAt: ts(row.FxRateAt), FxSource: row.FxSource, FxBaseCurrency: row.FxBaseCurrency,
		ProductTotal: row.ProductTotal, ChargeTotal: row.ChargeTotal, LandedTotal: row.LandedTotal,
		MarginTotal: row.MarginTotal, CustomerTotal: row.CustomerTotal, Status: row.Status,
		VersionNo:           int32(row.VersionNo),
		CustomerQuotationId: row.CustomerQuotationID, CustomerQuoteNo: row.CustomerQuoteNo,
		CreatedByName: row.CreatedByName, ConfirmedByName: row.ConfirmedByName,
		ConfirmedAt: ts(row.ConfirmedAt), CreatedAt: ts(row.CreatedAt),
		RequirementVersionNo:   int32(row.RequirementVersionNo),
		SubmittedToSalesByName: row.SubmittedToSalesByName, SubmittedToSalesAt: ts(row.SubmittedToSalesAt),
	}
}

func costScenarioView(view app.CostScenarioView) *prv1.CostScenario {
	h := view.Header
	out := &prv1.CostScenario{
		Id: h.ID, CaseId: h.CaseID, ScenarioNo: h.ScenarioNo, Currency: h.Currency,
		AllocationBasis: h.AllocationBasis, MarginType: h.MarginType, MarginValue: h.MarginValue,
		FxRate: h.FxRate, FxRateAt: ts(h.FxRateAt), FxSource: h.FxSource, FxBaseCurrency: h.FxBaseCurrency,
		ProductTotal: h.ProductTotal, ChargeTotal: h.ChargeTotal, LandedTotal: h.LandedTotal,
		MarginTotal: h.MarginTotal, CustomerTotal: h.CustomerTotal, Status: h.Status,
		VersionNo:           int32(h.VersionNo),
		CustomerQuotationId: h.CustomerQuotationID, CustomerQuoteNo: h.CustomerQuoteNo,
		CreatedByName: h.CreatedByName, ConfirmedByName: h.ConfirmedByName,
		ConfirmedAt: ts(h.ConfirmedAt), CreatedAt: ts(h.CreatedAt),
		ConfirmReason:          h.ConfirmReason,
		RequirementVersionNo:   int32(h.RequirementVersionNo),
		SubmittedToSalesByName: h.SubmittedToSalesByName, SubmittedToSalesAt: ts(h.SubmittedToSalesAt),
	}
	for _, charge := range view.Charges {
		out.Charges = append(out.Charges, &prv1.CostCharge{
			Id: charge.ID, ChargeType: charge.ChargeType, Basis: charge.Basis, Description: charge.Description,
			OriginPort: charge.OriginPort, DestinationPort: charge.DestinationPort, ContainerType: charge.ContainerType,
			Amount: charge.Amount, Currency: charge.Currency, ConvertedAmount: charge.ConvertedAmount,
			SourceFxRate: charge.SourceFxRate, TargetFxRate: charge.TargetFxRate, FxRateAt: ts(charge.FxRateAt),
			FxSource: charge.FxSource, EffectiveAt: charge.EffectiveAt, ValidUntil: charge.ValidUntil,
			Source: charge.Source, Remark: charge.Remark,
		})
	}
	for _, line := range view.Lines {
		out.Lines = append(out.Lines, &prv1.CostScenarioLine{
			Id: line.ID, SourcingLineId: line.SourcingLineID, SupplierQuoteLineId: line.SupplierQuoteLineID,
			SupplierName: line.SupplierName, ProductId: line.ProductID, SkuId: line.SkuID,
			ProductName: line.ProductName, SpecSnapshot: line.SpecSnapshot, Qty: line.Qty, UomCode: line.UomCode,
			SourceCurrency: line.SourceCurrency, SourceUnitPrice: line.SourceUnitPrice,
			SourceFxRate: line.SourceFxRate, TargetFxRate: line.TargetFxRate,
			ProductCost: line.ProductCost, AllocatedCharge: line.AllocatedCharge, LandedCost: line.LandedCost,
			MarginAmount: line.MarginAmount, CustomerUnitPrice: line.CustomerUnitPrice, CustomerAmount: line.CustomerAmount,
		})
	}
	return out
}

func factoryRFQ(row store.ListFactoryRFQsRow) *prv1.FactoryRfq {
	return &prv1.FactoryRfq{
		Id: row.ID, CaseId: row.CaseID, RfqNo: row.RfqNo, SupplierId: row.SupplierID,
		SupplierCode: row.SupplierCode, SupplierName: row.SupplierName, FactoryId: row.FactoryID,
		FactoryCode: row.FactoryCode, FactoryName: row.FactoryName, ContactEmail: row.ContactEmail,
		Currency: row.Currency, ResponseDueAt: row.ResponseDueAt, Status: row.Status,
		LineCount: row.LineCount, CreatedAt: ts(row.CreatedAt), SourcingLineIds: row.SourcingLineIds,
		InquiryChannel: row.InquiryChannel, ContactName: row.ContactName, ContactValue: row.ContactValue,
		ContactedAt: row.ContactedAt, InquiryNote: row.InquiryNote, RoundNo: row.RoundNo,
		CreatedBy: row.CreatedBy, CreatedByName: row.CreatedByName,
	}
}

func lineInput(in *prv1.SourcingLineInput) app.SourcingLineInput {
	return app.SourcingLineInput{
		RawText: in.GetRawText(), Product: in.GetProduct(), MaterialStandard: in.GetMaterialStandard(),
		Grade: in.GetGrade(), Thickness: in.GetThickness(), Width: in.GetWidth(),
		LengthOrForm: in.GetLengthOrForm(), SurfaceRequirement: in.GetSurfaceRequirement(),
		Coating: in.GetCoating(), Tolerance: in.GetTolerance(), CoilWeight: in.GetCoilWeight(),
		CoilID: in.GetCoilId(), Packaging: in.GetPackaging(), Delivery: in.GetDelivery(),
		PaymentTerms: in.GetPaymentTerms(), Incoterm: in.GetIncoterm(), Port: in.GetPort(),
		QuantityUnit: in.GetQuantityUnit(), Remarks: in.GetRemarks(), Quantity: in.GetQuantity(),
		CustomFields: in.GetCustomFields(),
	}
}

func sourcingCaseView(view app.SourcingCaseView) *prv1.SourcingCase {
	out := sourcingCaseHead(view.Head)
	if view.Head.SourceFileKey != "" {
		out.SourceFileUrl = "/api/inquiry-files?" + url.Values{"view": {"AUTO"}, "id": {strconv.FormatInt(view.Head.ID, 10)}, "key": {view.Head.SourceFileKey}}.Encode()
	}
	out.Lines = make([]*prv1.SourcingLine, 0, len(view.Lines))
	for _, line := range view.Lines {
		out.Lines = append(out.Lines, sourcingLine(line))
	}
	return out
}

func sourcingParticipants(items []app.SourcingParticipant) []*prv1.SourcingParticipant {
	out := make([]*prv1.SourcingParticipant, 0, len(items))
	for _, item := range items {
		out = append(out, &prv1.SourcingParticipant{
			Id: item.ID, EmployeeId: item.EmployeeID, EmployeeName: item.EmployeeName,
			Role: item.Role, Status: item.Status, PrimaryRequestedAt: participantTime(item.PrimaryRequestedAt),
			JoinedAt: item.JoinedAt.Format(time.RFC3339), UpdatedAt: item.UpdatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func participantTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func sourcingCaseHead(row store.GetSourcingCaseRow) *prv1.SourcingCase {
	acceptedBy, returnedBy := int64(0), int64(0)
	if row.AcceptedBy != nil {
		acceptedBy = *row.AcceptedBy
	}
	if row.ReturnedBy != nil {
		returnedBy = *row.ReturnedBy
	}
	return &prv1.SourcingCase{
		Id: row.ID, CaseNo: row.CaseNo, Title: row.Title, CustomerId: row.CustomerID,
		CustomerName: row.CustomerName, ContactId: row.ContactID, ContactName: row.ContactName, ContactEmail: row.ContactEmail,
		SourceMailId: row.SourceMailID, SourceAttachmentId: row.SourceAttachmentID,
		Status: row.Status, OwnerId: row.OwnerID, OwnerName: row.OwnerName,
		CreatedAt: ts(row.CreatedAt), UpdatedAt: ts(row.UpdatedAt),
		SourceFileName:    row.SourceFileName,
		InquiryTemplateId: row.InquiryTemplateID, InquiryTemplateCode: row.InquiryTemplateCode,
		InquiryTemplateVersion: row.InquiryTemplateVersion,
		HandoffStatus:          row.HandoffStatus, RequirementVersionNo: row.RequirementVersionNo,
		AcceptedBy: acceptedBy, AcceptedByName: row.AcceptedByName, AcceptedAt: ts(row.AcceptedAt),
		ReturnedBy: returnedBy, ReturnedByName: row.ReturnedByName, ReturnedAt: ts(row.ReturnedAt),
		ReturnReason: row.ReturnReason, ReturnFields: row.ReturnFields,
	}
}

func sourcingLine(row store.ListSourcingLinesRow) *prv1.SourcingLine {
	customFields := map[string]string{}
	_ = json.Unmarshal(row.CustomFields, &customFields)
	return &prv1.SourcingLine{Id: row.ID, LineNo: row.LineNo, Decision: row.Decision,
		ProductId: row.ProductID, SkuId: row.SkuID, UomId: row.UomID,
		DecidedByName: row.DecidedByName, DecidedAt: row.DecidedAt, RevisionNo: row.RevisionNo,
		Extracted: &prv1.SourcingLineInput{
			RawText: row.RawText, Product: row.Product, MaterialStandard: row.MaterialStandard,
			Grade: row.Grade, Thickness: row.Thickness, Width: row.Width,
			LengthOrForm: row.LengthOrForm, SurfaceRequirement: row.SurfaceRequirement,
			Coating: row.Coating, Tolerance: row.Tolerance, CoilWeight: row.CoilWeight,
			CoilId: row.CoilID, Packaging: row.Packaging, Delivery: row.Delivery,
			PaymentTerms: row.PaymentTerms, Incoterm: row.Incoterm, Port: row.Port,
			QuantityUnit: row.QuantityUnit, Remarks: row.Remarks, Quantity: row.Quantity,
			CustomFields: customFields,
		}}
}
