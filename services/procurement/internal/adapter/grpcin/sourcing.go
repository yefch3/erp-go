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
