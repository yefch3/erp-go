package grpcin

import (
	"context"

	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

func (h *Handler) ManageExecutionAssignment(ctx context.Context, req *prv1.ManageExecutionAssignmentRequest) (*prv1.ManageExecutionAssignmentResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	var draftLines []app.ExecutionDraftInput
	for _, line := range req.GetDraftLines() {
		draftLines = append(draftLines, app.ExecutionDraftInput{
			AssignmentID: line.GetAssignmentId(), Price: line.GetNegotiatedPrice(), Date: line.GetExpectedDate(),
			Terms: line.GetPaymentTerms(), Remark: line.GetRemark(), FileIDs: line.GetFileIds(),
		})
	}
	result, err := h.svc.ManageExecutionAssignment(ctx, grpcx.TenantID(ctx), app.Operator{ID: op.EmployeeID, Name: op.Name}, app.ExecutionAssignmentInput{
		Action: req.GetAction(), ID: req.GetId(), RequirementID: req.GetRequirementId(), QuoteID: req.GetQuoteId(), BuyerID: req.GetBuyerId(),
		Quantity: req.GetQuantity(), Instructions: req.GetInstructions(), Price: req.GetNegotiatedPrice(), Date: req.GetExpectedDate(),
		Terms: req.GetPaymentTerms(), Remark: req.GetRemark(), FileName: req.GetFileName(), ContentType: req.GetContentType(),
		FileData: req.GetFileData(), FileIDs: req.GetFileIds(), FileID: req.GetFileId(),
		BatchKey: req.GetBatchKey(), DraftLines: draftLines, DraftPreviewToken: req.GetDraftPreviewToken(),
	})
	if err != nil {
		return nil, err
	}
	response := &prv1.ManageExecutionAssignmentResponse{Id: result.ID, OrderId: result.OrderID, FileName: result.FileName,
		ContentType: result.ContentType, FileData: result.FileData, DraftPreviewToken: result.DraftPreviewToken}
	for _, group := range result.DraftGroups {
		item := &prv1.ExecutionDraftGroup{BuyerId: group.BuyerID, BuyerName: group.BuyerName, OrderId: group.OrderID,
			SupplierName: group.SupplierName, Currency: group.Currency, ExpectedDate: group.Date, PaymentTerms: group.Terms}
		for _, line := range group.Lines {
			item.Lines = append(item.Lines, &prv1.ExecutionDraftPreviewLine{AssignmentId: line.AssignmentID, ProductName: line.ProductName,
				Specification: line.Specification, Quantity: line.Quantity, UomCode: line.UomCode, NegotiatedPrice: line.Price})
		}
		response.DraftGroups = append(response.DraftGroups, item)
	}
	for _, a := range result.Assignments {
		item := &prv1.ExecutionAssignment{
			Id: a.ID, RequirementId: a.RequirementID, QuoteId: a.QuoteID, BuyerId: a.BuyerID, BuyerName: a.BuyerName,
			Quantity: a.Quantity, Instructions: a.Instructions, SupplierName: a.SupplierName, Currency: a.Currency,
			InitialPrice: a.InitialPrice, NegotiatedPrice: a.NegotiatedPrice, ExpectedDate: a.ExpectedDate, PaymentTerms: a.PaymentTerms,
			Remark: a.Remark, PoId: a.OrderID, OrderStatus: a.OrderStatus, ProductName: a.ProductName, UomCode: a.UomCode,
			ContractNo: a.ContractNo, BatchKey: a.BatchKey, AssignedBy: a.AssignedBy, ChangedBy: a.ChangedBy, Specification: a.Specification,
		}
		for _, f := range a.Files {
			item.Files = append(item.Files, &prv1.ExecutionAssignmentFile{Id: f.ID, FileName: f.Name, ContentType: f.ContentType})
		}
		response.Assignments = append(response.Assignments, item)
	}
	return response, nil
}
