package grpcin

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

type SourcingHandler struct {
	prv1.UnimplementedSourcingServiceServer
	svc *app.Service
}

func NewSourcing(svc *app.Service) *SourcingHandler { return &SourcingHandler{svc: svc} }

func (h *SourcingHandler) CreateCase(ctx context.Context, req *prv1.CreateCaseRequest) (*prv1.CreateCaseResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.SourcingLineInput, 0, len(req.GetLines()))
	for _, line := range req.GetLines() {
		lines = append(lines, lineInput(line))
	}
	view, err := h.svc.CreateSourcingCase(ctx, grpcx.TenantID(ctx), app.NewSourcingCase{
		Title: req.GetTitle(), CustomerID: req.GetCustomerId(), CustomerName: req.GetCustomerName(),
		ContactName: req.GetContactName(), ContactEmail: req.GetContactEmail(),
		SourceMailID: req.GetSourceMailId(), SourceAttachmentID: req.GetSourceAttachmentId(), Lines: lines,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateCaseResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) ListCases(ctx context.Context, req *prv1.ListCasesRequest) (*prv1.ListCasesResponse, error) {
	rows, total, err := h.svc.ListSourcingCases(ctx, grpcx.TenantID(ctx), app.SourcingFilter{
		Status: req.GetStatus(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize())
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
	view, err := h.svc.GetSourcingCase(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetCaseResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) ConfirmLines(ctx context.Context, req *prv1.ConfirmLinesRequest) (*prv1.ConfirmLinesResponse, error) {
	view, err := h.svc.ConfirmSourcingLines(ctx, grpcx.TenantID(ctx), req.GetCaseId(), req.GetSourcingLineIds())
	if err != nil {
		return nil, err
	}
	return &prv1.ConfirmLinesResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) ReviewLine(ctx context.Context, req *prv1.ReviewLineRequest) (*prv1.ReviewLineResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	view, err := h.svc.ReviewSourcingLine(ctx, grpcx.TenantID(ctx), app.SourcingLineReview{
		CaseID: req.GetCaseId(), LineID: req.GetLineId(), ProductID: req.GetProductId(), SkuID: req.GetSkuId(),
		UomID: req.GetUomId(), Decision: req.GetDecision(), Extracted: lineInput(req.GetExtracted()),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ReviewLineResponse{SourcingCase: sourcingCaseView(view)}, nil
}

func (h *SourcingHandler) CreateFactoryRfq(ctx context.Context, req *prv1.CreateFactoryRfqRequest) (*prv1.CreateFactoryRfqResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	row, err := h.svc.CreateFactoryRFQ(ctx, grpcx.TenantID(ctx), app.NewFactoryRFQ{CaseID: req.GetCaseId(), SupplierID: req.GetSupplierId(),
		ContactEmail: req.GetContactEmail(), Currency: req.GetCurrency(), ResponseDueAt: req.GetResponseDueAt(), SourcingLineIDs: req.GetSourcingLineIds()}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateFactoryRfqResponse{FactoryRfq: factoryRFQ(row)}, nil
}

func (h *SourcingHandler) ListFactoryRfqs(ctx context.Context, req *prv1.ListFactoryRfqsRequest) (*prv1.ListFactoryRfqsResponse, error) {
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
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.SupplierQuoteLineInput, 0, len(req.GetLines()))
	for _, line := range req.GetLines() {
		lines = append(lines, app.SupplierQuoteLineInput{SourcingLineID: line.GetSourcingLineId(), Qty: line.GetQty(), UnitPrice: line.GetUnitPrice(), MOQ: line.GetMoq(), LeadTime: line.GetLeadTime(), Remark: line.GetRemark()})
	}
	row, err := h.svc.CreateSupplierQuote(ctx, grpcx.TenantID(ctx), app.NewSupplierQuote{FactoryRFQID: req.GetFactoryRfqId(), QuotedAt: req.GetQuotedAt(), ValidUntil: req.GetValidUntil(), Currency: req.GetCurrency(), PaymentTerms: req.GetPaymentTerms(), Delivery: req.GetDelivery(), Remark: req.GetRemark(), Source: req.GetSource(), Lines: lines}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateSupplierQuoteResponse{Id: row.ID, SupplierQuoteNo: row.SupplierQuoteNo}, nil
}

func (h *SourcingHandler) GetFactoryRfqWorkbook(ctx context.Context, req *prv1.GetFactoryRfqWorkbookRequest) (*prv1.GetFactoryRfqWorkbookResponse, error) {
	book, err := h.svc.GetFactoryRFQWorkbook(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetFactoryRfqWorkbookResponse{FileName: book.FileName, FileData: book.Data, RfqNo: book.RFQNo,
		SupplierName: book.SupplierName, ContactEmail: book.ContactEmail, Currency: book.Currency, ResponseDueAt: book.ResponseDueAt}, nil
}

func (h *SourcingHandler) ImportSupplierQuoteWorkbook(ctx context.Context, req *prv1.ImportSupplierQuoteWorkbookRequest) (*prv1.ImportSupplierQuoteWorkbookResponse, error) {
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
	if err := h.svc.MarkFactoryRFQSent(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &prv1.MarkFactoryRfqSentResponse{Status: "SENT"}, nil
}

func (h *SourcingHandler) ListSupplierQuoteComparison(ctx context.Context, req *prv1.ListSupplierQuoteComparisonRequest) (*prv1.ListSupplierQuoteComparisonResponse, error) {
	rows, err := h.svc.ListSupplierQuoteComparison(ctx, grpcx.TenantID(ctx), req.GetCaseId())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.SupplierQuoteComparisonLine, 0, len(rows))
	for _, row := range rows {
		out = append(out, &prv1.SupplierQuoteComparisonLine{QuoteId: row.QuoteID, SupplierQuoteNo: row.SupplierQuoteNo, FactoryRfqId: row.FactoryRfqID, SupplierId: row.SupplierID, SupplierName: row.SupplierName, Currency: row.Currency, QuotedAt: row.QuotedAt, ValidUntil: row.ValidUntil, PaymentTerms: row.PaymentTerms, Delivery: row.Delivery, QuoteRemark: row.Remark, Source: row.Source, SourcingLineId: row.SourcingLineID, Qty: row.LQty, UnitPrice: row.LUnitPrice, Amount: row.LAmount, Moq: row.Moq, LeadTime: row.LeadTime, LineRemark: row.LineRemark})
	}
	return &prv1.ListSupplierQuoteComparisonResponse{Lines: out}, nil
}

func factoryRFQ(row store.ListFactoryRFQsRow) *prv1.FactoryRfq {
	return &prv1.FactoryRfq{Id: row.ID, CaseId: row.CaseID, RfqNo: row.RfqNo, SupplierId: row.SupplierID, SupplierCode: row.SupplierCode, SupplierName: row.SupplierName, ContactEmail: row.ContactEmail, Currency: row.Currency, ResponseDueAt: row.ResponseDueAt, Status: row.Status, LineCount: row.LineCount, CreatedAt: ts(row.CreatedAt), SourcingLineIds: row.SourcingLineIds}
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
	}
}

func sourcingCaseView(view app.SourcingCaseView) *prv1.SourcingCase {
	out := sourcingCaseHead(view.Head)
	out.Lines = make([]*prv1.SourcingLine, 0, len(view.Lines))
	for _, line := range view.Lines {
		out.Lines = append(out.Lines, sourcingLine(line))
	}
	return out
}

func sourcingCaseHead(row store.GetSourcingCaseRow) *prv1.SourcingCase {
	return &prv1.SourcingCase{
		Id: row.ID, CaseNo: row.CaseNo, Title: row.Title, CustomerId: row.CustomerID,
		CustomerName: row.CustomerName, ContactName: row.ContactName, ContactEmail: row.ContactEmail,
		SourceMailId: row.SourceMailID, SourceAttachmentId: row.SourceAttachmentID,
		Status: row.Status, OwnerId: row.OwnerID, OwnerName: row.OwnerName,
		CreatedAt: ts(row.CreatedAt), UpdatedAt: ts(row.UpdatedAt),
	}
}

func sourcingCaseList(row store.ListSourcingCasesRow) *prv1.SourcingCase {
	return &prv1.SourcingCase{
		Id: row.ID, CaseNo: row.CaseNo, Title: row.Title, CustomerId: row.CustomerID,
		CustomerName: row.CustomerName, ContactName: row.ContactName, ContactEmail: row.ContactEmail,
		SourceMailId: row.SourceMailID, SourceAttachmentId: row.SourceAttachmentID,
		Status: row.Status, OwnerId: row.OwnerID, OwnerName: row.OwnerName,
		CreatedAt: ts(row.CreatedAt), UpdatedAt: ts(row.UpdatedAt),
	}
}

func sourcingLine(row store.ListSourcingLinesRow) *prv1.SourcingLine {
	return &prv1.SourcingLine{Id: row.ID, LineNo: row.LineNo, Decision: row.Decision,
		ProductId: row.ProductID, SkuId: row.SkuID, UomId: row.UomID,
		DecidedByName: row.DecidedByName, DecidedAt: row.DecidedAt,
		Extracted: &prv1.SourcingLineInput{
			RawText: row.RawText, Product: row.Product, MaterialStandard: row.MaterialStandard,
			Grade: row.Grade, Thickness: row.Thickness, Width: row.Width,
			LengthOrForm: row.LengthOrForm, SurfaceRequirement: row.SurfaceRequirement,
			Coating: row.Coating, Tolerance: row.Tolerance, CoilWeight: row.CoilWeight,
			CoilId: row.CoilID, Packaging: row.Packaging, Delivery: row.Delivery,
			PaymentTerms: row.PaymentTerms, Incoterm: row.Incoterm, Port: row.Port,
			QuantityUnit: row.QuantityUnit, Remarks: row.Remarks, Quantity: row.Quantity,
		}}
}
