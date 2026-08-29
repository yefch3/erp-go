package grpcin

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
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

func (h *SourcingHandler) CreateCase(ctx context.Context, req *prv1.CreateCaseRequest) (*prv1.CreateCaseResponse, error) {
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
	op := sourcingOperator(ctx)
	rows, total, err := h.svc.ListSourcingCases(ctx, grpcx.TenantID(ctx), app.SourcingFilter{
		Status: req.GetStatus(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize(), op)
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SourcingCase, 0, len(rows))
	for _, row := range rows {
		out = append(out, sourcingCaseList(row))
	}
	return &prv1.ListCasesResponse{SourcingCases: out, Meta: &commonv1.PageMeta{Total: total}}, nil
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
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	view, err := h.svc.AddSourcingLine(ctx, grpcx.TenantID(ctx), req.GetCaseId(), lineInput(req.GetExtracted()), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.AddLineResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) ConfirmLines(ctx context.Context, req *prv1.ConfirmLinesRequest) (*prv1.ConfirmLinesResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	view, err := h.svc.ConfirmSourcingLines(ctx, grpcx.TenantID(ctx), req.GetCaseId(), req.GetSourcingLineIds(), req.GetReason(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ConfirmLinesResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) AcceptCase(ctx context.Context, req *prv1.AcceptCaseRequest) (*prv1.AcceptCaseResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	view, err := h.svc.AcceptSourcingCase(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.AcceptCaseResponse{SourcingCase: sourcingCaseView(view)}, nil
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
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	items, err := h.svc.JoinSourcingCase(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.JoinCaseResponse{Participants: sourcingParticipants(items)}, nil
}

func (h *SourcingHandler) RequestPrimaryBuyer(ctx context.Context, req *prv1.RequestPrimaryBuyerRequest) (*prv1.RequestPrimaryBuyerResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	items, view, err := h.svc.RequestPrimarySourcingCase(ctx, grpcx.TenantID(ctx), req.GetCaseId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.RequestPrimaryBuyerResponse{Participants: sourcingParticipants(items), SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) AssignPrimaryBuyer(ctx context.Context, req *prv1.AssignPrimaryBuyerRequest) (*prv1.AssignPrimaryBuyerResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	items, view, err := h.svc.AssignPrimarySourcingCase(ctx, grpcx.TenantID(ctx), req.GetCaseId(), req.GetEmployeeId(), req.GetReason(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.AssignPrimaryBuyerResponse{Participants: sourcingParticipants(items), SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) ReturnCase(ctx context.Context, req *prv1.ReturnCaseRequest) (*prv1.ReturnCaseResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	view, err := h.svc.ReturnSourcingCase(ctx, grpcx.TenantID(ctx), req.GetCaseId(), req.GetMissingFields(), req.GetReason(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.ReturnCaseResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) ReviewLine(ctx context.Context, req *prv1.ReviewLineRequest) (*prv1.ReviewLineResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	view, err := h.svc.ReviewSourcingLine(ctx, grpcx.TenantID(ctx), app.SourcingLineReview{
		CaseID: req.GetCaseId(), LineID: req.GetLineId(), ProductID: req.GetProductId(), SkuID: req.GetSkuId(),
		UomID: req.GetUomId(), Decision: req.GetDecision(), Reason: req.GetReason(), Extracted: lineInput(req.GetExtracted()),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ReviewLineResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) CreateFactoryRfq(ctx context.Context, req *prv1.CreateFactoryRfqRequest) (*prv1.CreateFactoryRfqResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.CreateFactoryRFQ(ctx, grpcx.TenantID(ctx), app.NewFactoryRFQ{CaseID: req.GetCaseId(), SupplierID: req.GetSupplierId(),
		FactoryID: req.GetFactoryId(), FactoryCode: req.GetFactoryCode(), FactoryName: req.GetFactoryName(),
		ContactEmail: req.GetContactEmail(), Currency: req.GetCurrency(), ResponseDueAt: req.GetResponseDueAt(), SourcingLineIDs: req.GetSourcingLineIds(),
		InquiryChannel: req.GetInquiryChannel(), ContactName: req.GetContactName(), ContactValue: req.GetContactValue(),
		ContactedAt: req.GetContactedAt(), InquiryNote: req.GetInquiryNote(), RoundNo: req.GetRoundNo()}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateFactoryRfqResponse{FactoryRfq: factoryRFQ(row)}, nil
}

func (h *SourcingHandler) ListCaseChanges(ctx context.Context, req *prv1.ListCaseChangesRequest) (*prv1.ListCaseChangesResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	rows, err := h.svc.ListSourcingChanges(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SourcingCaseChange, 0, len(rows))
	for _, row := range rows {
		out = append(out, &prv1.SourcingCaseChange{Id: row.ID, Section: row.Section, Action: row.Action,
			EntityId: row.EntityID, Summary: row.Summary, BeforeJson: string(row.BeforeJson), AfterJson: string(row.AfterJson),
			Reason: row.Reason, OperatorId: row.OperatorID, OperatorName: row.OperatorName, CreatedAt: ts(row.CreatedAt)})
	}
	return &prv1.ListCaseChangesResponse{Changes: out}, nil
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
	if err := h.authorizeRFQ(ctx, req.GetId()); err != nil {
		return nil, err
	}
	row, err := h.svc.UpdateFactoryRFQ(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetInquiryChannel(), req.GetContactName(), req.GetContactEmail(), req.GetContactValue(), req.GetContactedAt(), req.GetInquiryNote(), req.GetResponseDueAt(), req.GetReason(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.UpdateFactoryRfqResponse{FactoryRfq: factoryRFQ(row)}, nil
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
	if err := h.authorizeRFQ(ctx, req.GetFactoryRfqId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.SupplierQuoteLineInput, 0, len(req.GetLines()))
	for _, line := range req.GetLines() {
		lines = append(lines, app.SupplierQuoteLineInput{SourcingLineID: line.GetSourcingLineId(), Qty: line.GetQty(), UnitPrice: line.GetUnitPrice(), MOQ: line.GetMoq(), LeadTime: line.GetLeadTime(), Remark: line.GetRemark()})
	}
	row, err := h.svc.CreateSupplierQuote(ctx, grpcx.TenantID(ctx), app.NewSupplierQuote{FactoryRFQID: req.GetFactoryRfqId(), QuotedAt: req.GetQuotedAt(), ValidUntil: req.GetValidUntil(), Currency: req.GetCurrency(), PaymentTerms: req.GetPaymentTerms(), Delivery: req.GetDelivery(), Remark: req.GetRemark(), Source: req.GetSource(), ConfirmationStatus: req.GetConfirmationStatus(), EvidenceNote: req.GetEvidenceNote(), Lines: lines}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSupplierQuoteResponse{Id: row.ID, SupplierQuoteNo: row.SupplierQuoteNo, VersionNo: row.VersionNo}, nil
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
	if err := h.authorizeRFQ(ctx, req.GetFactoryRfqId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.ImportSupplierQuoteWorkbook(ctx, grpcx.TenantID(ctx), req.GetFileData(), app.NewSupplierQuote{
		FactoryRFQID: req.GetFactoryRfqId(), QuotedAt: req.GetQuotedAt(), ValidUntil: req.GetValidUntil(), Currency: req.GetCurrency(),
		PaymentTerms: req.GetPaymentTerms(), Delivery: req.GetDelivery(), Remark: req.GetRemark(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ImportSupplierQuoteWorkbookResponse{Id: row.ID, SupplierQuoteNo: row.SupplierQuoteNo}, nil
}

func (h *SourcingHandler) MarkFactoryRfqSent(ctx context.Context, req *prv1.MarkFactoryRfqSentRequest) (*prv1.MarkFactoryRfqSentResponse, error) {
	if err := h.authorizeRFQ(ctx, req.GetId()); err != nil {
		return nil, err
	}
	if err := h.svc.MarkFactoryRFQSent(ctx, grpcx.TenantID(ctx), req.GetId(), sourcingOperator(ctx)); err != nil {
		return nil, err
	}
	return &prv1.MarkFactoryRfqSentResponse{Status: "SENT"}, nil
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
			PaymentTerms: row.PaymentTerms, Delivery: row.Delivery, QuoteRemark: row.Remark,
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

func (h *SourcingHandler) CreateCostScenario(ctx context.Context, req *prv1.CreateCostScenarioRequest) (*prv1.CreateCostScenarioResponse, error) {
	if err := h.authorizeCase(ctx, req.GetCaseId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	in := app.NewCostScenario{
		CaseID: req.GetCaseId(), Currency: req.GetCurrency(), AllocationBasis: req.GetAllocationBasis(),
		MarginType: req.GetMarginType(), MarginValue: req.GetMarginValue(),
	}
	for _, selection := range req.GetSelections() {
		in.Selections = append(in.Selections, app.CostSelectionInput{
			SourcingLineID: selection.GetSourcingLineId(), SupplierQuoteLineID: selection.GetSupplierQuoteLineId(),
		})
	}
	for _, charge := range req.GetCharges() {
		in.Charges = append(in.Charges, app.CostChargeInput{
			ChargeType: charge.GetChargeType(), Basis: charge.GetBasis(), Description: charge.GetDescription(),
			OriginPort: charge.GetOriginPort(), DestinationPort: charge.GetDestinationPort(), ContainerType: charge.GetContainerType(),
			Amount: charge.GetAmount(), Currency: charge.GetCurrency(), EffectiveAt: charge.GetEffectiveAt(),
			ValidUntil: charge.GetValidUntil(), Source: charge.GetSource(), Remark: charge.GetRemark(),
		})
	}
	view, err := h.svc.CreateCostScenario(ctx, grpcx.TenantID(ctx), in, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateCostScenarioResponse{CostScenario: costScenarioView(view)}, nil
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
	if err := h.authorizeScenario(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	view, err := h.svc.ConfirmCostScenario(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetReason(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ConfirmCostScenarioResponse{CostScenario: costScenarioView(view)}, nil
}

func (h *SourcingHandler) SubmitCostToSales(ctx context.Context, req *prv1.SubmitCostToSalesRequest) (*prv1.SubmitCostToSalesResponse, error) {
	if err := h.authorizeScenario(ctx, req.GetId()); err != nil {
		return nil, err
	}
	view, err := h.svc.SubmitCostToSales(ctx, grpcx.TenantID(ctx), req.GetId(), sourcingOperator(ctx))
	if err != nil {
		return nil, err
	}
	return &prv1.SubmitCostToSalesResponse{CostScenario: costScenarioView(view)}, nil
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
	out.SourceFileUrl = view.SourceFileURL
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

func sourcingCaseList(row store.ListSourcingCasesRow) *prv1.SourcingCase {
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
		SourceFileName: row.SourceFileName,
		HandoffStatus:  row.HandoffStatus, RequirementVersionNo: row.RequirementVersionNo,
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
