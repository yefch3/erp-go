package grpcin

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/deadletter"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// OrderHandler exposes purchase orders. Separate from the requirement handler
// because they are separate services in the proto: reading what has to be
// bought and committing company money are different permissions.
type OrderHandler struct {
	prv1.UnimplementedPurchaseOrderServiceServer
	svc  *app.Service
	dead *deadletter.Console
}

func NewOrders(svc *app.Service) *OrderHandler { return &OrderHandler{svc: svc} }

// authorizeOrder fences every handler that addresses one order. At the
// adapter because that is where a request stops being anonymous plumbing and
// starts being a person — the same place the sourcing handlers gate.
func (h *OrderHandler) authorizeOrder(ctx context.Context, orderID int64) error {
	op, _ := grpcx.OperatorFromContext(ctx)
	return h.svc.AuthorizeOrder(ctx, grpcx.TenantID(ctx), orderID, app.Operator{ID: op.EmployeeID, Name: op.Name})
}

func (h *OrderHandler) PreviewOrderImport(ctx context.Context, req *prv1.PreviewOrderImportRequest) (*prv1.PreviewOrderImportResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows := make([]app.OrderImportRow, 0, len(req.GetRows()))
	for _, row := range req.GetRows() {
		rows = append(rows, app.OrderImportRow{
			RowNo: row.GetRowNo(), Product: row.GetProduct(), MaterialStandard: row.GetMaterialStandard(),
			Grade: row.GetGrade(), Thickness: row.GetThickness(), Width: row.GetWidth(),
			QuantityUnit: row.GetQuantityUnit(), Quantity: row.GetQuantity(), UnitPrice: row.GetUnitPrice(),
			RequirementID: row.GetRequirementId(), ProductID: row.GetProductId(), SKUID: row.GetSkuId(), UomID: row.GetUomId(),
		})
	}
	result, err := h.svc.PreviewOrderImport(ctx, grpcx.TenantID(ctx), app.PreviewOrderImportInput{
		SourceType: req.GetSourceType(), SourceMailID: req.GetSourceMailId(),
		SourceAttachmentID: req.GetSourceAttachmentId(), SourceFileName: req.GetSourceFileName(),
		FileSHA256: req.GetFileSha256(), Rows: rows,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	outRows := make([]*prv1.OrderImportPreviewRow, 0, len(result.Rows))
	for _, row := range result.Rows {
		candidates := make([]*prv1.OrderImportCandidate, 0, len(row.Candidates))
		for _, candidate := range row.Candidates {
			candidates = append(candidates, &prv1.OrderImportCandidate{
				RequirementId: candidate.RequirementID, ContractNo: candidate.ContractNo,
				ProductName: candidate.ProductName, ProductCode: candidate.ProductCode,
				Spec: candidate.Spec, UomCode: candidate.UomCode, RequiredQty: candidate.RequiredQty,
				OrderedQty: candidate.OrderedQty, OpenQty: candidate.OpenQty, Status: candidate.Status,
			})
		}
		outRows = append(outRows, &prv1.OrderImportPreviewRow{
			RowNo: row.RowNo, Product: row.Product, Quantity: row.Quantity,
			QuantityUnit: row.QuantityUnit, UnitPrice: row.UnitPrice,
			Candidates: candidates, Result: row.Result, Message: row.Message,
			SuggestedRequirementId: row.SuggestedRequirementID,
		})
	}
	return &prv1.PreviewOrderImportResponse{
		ImportToken: result.ImportToken, ExpiresAt: result.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"), Rows: outRows,
	}, nil
}

func (h *OrderHandler) PreviewPurchaseTemplateImport(ctx context.Context, req *prv1.PreviewPurchaseTemplateImportRequest) (*prv1.PreviewPurchaseTemplateImportResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	result, err := h.svc.PreviewPurchaseTemplateImport(ctx, grpcx.TenantID(ctx), req.GetFileData(), req.GetSourceFileName(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	groups := make([]*prv1.PurchaseTemplateImportGroup, 0, len(result.Groups))
	for _, group := range result.Groups {
		lines := make([]*prv1.PurchaseTemplateImportLine, 0, len(group.Lines))
		for _, line := range group.Lines {
			lines = append(lines, &prv1.PurchaseTemplateImportLine{RowNo: line.RowNo, RequirementId: line.RequirementID, ProductName: line.ProductName, Qty: line.Qty, UomCode: line.UomCode, UnitPrice: line.UnitPrice, Moq: line.MOQ})
		}
		groups = append(groups, &prv1.PurchaseTemplateImportGroup{ImportToken: group.ImportToken, SupplierId: group.Supplier.ID, SupplierCode: group.Supplier.Code, SupplierName: group.Supplier.Name, Currency: group.Currency, ExpectedDate: group.ExpectedDate, PaymentTerms: group.PaymentTerms, Lines: lines})
	}
	return &prv1.PreviewPurchaseTemplateImportResponse{Groups: groups, TemplateVersion: result.Version, ErrorFileName: result.ErrorFileName, ErrorFileData: result.ErrorFileData}, nil
}

func (h *OrderHandler) ConfirmOrderImport(ctx context.Context, req *prv1.ConfirmOrderImportRequest) (*prv1.ConfirmOrderImportResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.ConfirmOrderImportLine, 0, len(req.GetLines()))
	for _, line := range req.GetLines() {
		lines = append(lines, app.ConfirmOrderImportLine{
			RowNo: line.GetRowNo(), RequirementID: line.GetRequirementId(), Qty: line.GetQty(), UnitPrice: line.GetUnitPrice(),
		})
	}
	result, err := h.svc.ConfirmOrderImport(ctx, grpcx.TenantID(ctx), app.ConfirmOrderImportInput{
		ImportToken: req.GetImportToken(), SupplierID: req.GetSupplierId(), Currency: req.GetCurrency(),
		ExpectedDate: req.GetExpectedDate(), Remark: req.GetRemark(), Lines: lines,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ConfirmOrderImportResponse{
		Id: result.ID, PoNo: result.PONo, Status: result.Status, AlreadyCreated: result.AlreadyCreated,
	}, nil
}

func (h *OrderHandler) ListOrders(ctx context.Context, req *prv1.ListOrdersRequest) (*prv1.ListOrdersResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	rows, total, err := h.svc.ListOrders(ctx, grpcx.TenantID(ctx), app.OrderFilter{
		Status: req.GetStatus(), Keyword: req.GetKeyword(), Unsent: req.GetUnsent(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.PurchaseOrder, 0, len(rows))
	for _, r := range rows {
		item := &prv1.PurchaseOrder{
			Id: r.ID, PoNo: r.PoNo, SupplierId: r.SupplierID, SupplierName: r.SupplierName,
			Currency: r.Currency, TotalAmount: r.TotalAmount, ExpectedDate: r.ExpectedDate,
			PayableDueDate: r.PayableDueDate,
			Status:         r.Status, BuyerName: r.BuyerName, Remark: r.Remark,
			RejectReason: r.RejectReason, CancelReason: r.CancelReason,
			ItemCount: r.ItemCount, TotalQty: r.TotalQty, ReceivedQty: r.ReceivedQty,
			CreatedAt: ts(r.CreatedAt), SendStatus: r.SendStatus, SentTo: r.SentTo,
			SentAt: ts(r.SentAt), SentBy: r.SentByName, SendError: r.SendError,
			ClosedAt: r.ClosedAt, ClosedBy: r.ClosedByName,
			CloseNote: r.CloseNote, ShortfallAction: r.ShortfallAction,
			ConfirmStatus:   r.ConfirmStatus,
			FulfillmentMode: r.FulfillmentMode, DeliveryLocationType: r.DeliveryLocationType,
			DeliveryPortId: r.DeliveryPortID, DeliveryPortCode: r.DeliveryPortCode,
			DeliveryPortName: r.DeliveryPortName, WarehouseId: r.WarehouseID,
			WarehouseName: r.WarehouseName, DeliveryAddress: r.DeliveryAddress,
			SourceQuotationId: r.SourceQuotationID, SourceQuotationNo: r.SourceQuotationNo,
			SourceCostScenarioId: r.SourceCostScenarioID, FactoryId: r.FactoryID,
			FactoryCode: r.FactoryCode, FactoryName: r.FactoryName,
		}
		if meta, metaErr := h.svc.OrderContractState(ctx, grpcx.TenantID(ctx), r.ID); metaErr == nil {
			applyOrderContractState(item, meta)
		}
		out = append(out, item)
	}
	return &prv1.ListOrdersResponse{Orders: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *OrderHandler) GetOrder(ctx context.Context, req *prv1.GetOrderRequest) (*prv1.GetOrderResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	tenantID := grpcx.TenantID(ctx)
	head, err := h.svc.GetOrder(ctx, tenantID, req.GetId())
	if err != nil {
		return nil, err
	}
	items, err := h.svc.OrderItems(ctx, tenantID, head.ID)
	if err != nil {
		return nil, err
	}
	receipts, err := h.svc.OrderReceipts(ctx, tenantID, head.ID)
	if err != nil {
		return nil, err
	}
	outItems := make([]*prv1.PurchaseOrderItem, 0, len(items))
	for _, it := range items {
		outItems = append(outItems, &prv1.PurchaseOrderItem{
			Id: it.ID, RequirementId: it.RequirementID, ProductId: it.ProductID,
			SkuId: it.SkuID, ProductCode: it.ProductCode, ProductName: it.ProductName,
			Spec: it.Spec, UomCode: it.UomCode, Qty: it.Qty, UnitPrice: it.UnitPrice,
			Amount: it.Amount, ReceivedQty: it.ReceivedQty,
			ContractNo: it.ContractNo, CustomerName: it.CustomerName, Source: it.Source,
			ContractOwner: it.ContractOwner,
		})
	}
	outReceipts := make([]*prv1.PurchaseReceipt, 0, len(receipts))
	for _, r := range receipts {
		outReceipts = append(outReceipts, &prv1.PurchaseReceipt{
			Id: r.ID, ReceiptNo: r.ReceiptNo, WarehouseId: r.WarehouseID,
			OperatorName: r.OperatorName, Remark: r.Remark,
			TotalQty: r.TotalQty, ReceivedAt: ts(r.ReceivedAt),
		})
	}
	outOrder := &prv1.PurchaseOrder{
		Id: head.ID, PoNo: head.PoNo, SupplierId: head.SupplierID,
		SupplierCode: head.SupplierCode, SupplierName: head.SupplierName,
		Currency: head.Currency, TotalAmount: head.TotalAmount,
		ExpectedDate: head.ExpectedDate, PayableDueDate: head.PayableDueDate,
		Status:    head.Status,
		BuyerName: head.BuyerName, Remark: head.Remark,
		RejectReason: head.RejectReason, CancelReason: head.CancelReason,
		ApprovalInstanceId: head.ApprovalInstanceID, CreatedAt: ts(head.CreatedAt),
		SendStatus: head.SendStatus, SentTo: head.SentTo, SentAt: ts(head.SentAt),
		SentBy: head.SentByName, SendError: head.SendError,
		ClosedAt: head.ClosedAt, ClosedBy: head.ClosedByName,
		CloseNote: head.CloseNote, ShortfallAction: head.ShortfallAction,
		ConfirmStatus:   head.ConfirmStatus,
		FulfillmentMode: head.FulfillmentMode, DeliveryLocationType: head.DeliveryLocationType,
		DeliveryPortId: head.DeliveryPortID, DeliveryPortCode: head.DeliveryPortCode,
		DeliveryPortName: head.DeliveryPortName, WarehouseId: head.WarehouseID,
		WarehouseName: head.WarehouseName, DeliveryAddress: head.DeliveryAddress,
		SourceQuotationId: head.SourceQuotationID, SourceQuotationNo: head.SourceQuotationNo,
		SourceCostScenarioId: head.SourceCostScenarioID, FactoryId: head.FactoryID,
		FactoryCode: head.FactoryCode, FactoryName: head.FactoryName,
	}
	if meta, metaErr := h.svc.OrderContractState(ctx, tenantID, head.ID); metaErr == nil {
		applyOrderContractState(outOrder, meta)
	}
	return &prv1.GetOrderResponse{Order: outOrder, Items: outItems, Receipts: outReceipts}, nil
}

func applyOrderContractState(out *prv1.PurchaseOrder, v app.OrderContractState) {
	out.BusinessType = v.BusinessType
	out.SourceBusinessId = v.SourceBusinessID
	out.ExportContractNo = v.ExportContractNo
	out.BusinessDocumentNo = v.BusinessDocumentNo
	out.PaymentTerms = v.PaymentTerms
	out.SignedContractName = v.SignedContractName
	out.SignedContractUrl = v.SignedContractURL
	out.SignedContractUploadedAt = v.SignedContractUploadedAt
	out.ContractVerifiedAt = v.ContractVerifiedAt
	out.ContractVerifiedByName = v.ContractVerifiedByName
	out.PaymentRequestedAt = v.PaymentRequestedAt
	out.PaymentRequestedByName = v.PaymentRequestedByName
}

func (h *OrderHandler) PresignOrderContractUpload(ctx context.Context, req *prv1.PresignOrderContractUploadRequest) (*prv1.PresignOrderContractUploadResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	v, err := h.svc.PresignOrderContract(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &prv1.PresignOrderContractUploadResponse{FileKey: v.Key, UploadUrl: v.URL, ExpiresSeconds: v.Expires}, nil
}
func (h *OrderHandler) SaveOrderContract(ctx context.Context, req *prv1.SaveOrderContractRequest) (*prv1.SaveOrderContractResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.SaveOrderContract(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetContractNo(), req.GetPaymentTerms(), req.GetFileKey(), req.GetFileName(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := &prv1.PurchaseOrder{Id: req.GetId()}
	applyOrderContractState(out, v)
	return &prv1.SaveOrderContractResponse{Order: out}, nil
}
func (h *OrderHandler) VerifyOrderContract(ctx context.Context, req *prv1.VerifyOrderContractRequest) (*prv1.VerifyOrderContractResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.VerifyOrderContract(ctx, grpcx.TenantID(ctx), req.GetId(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := &prv1.PurchaseOrder{Id: req.GetId()}
	applyOrderContractState(out, v)
	return &prv1.VerifyOrderContractResponse{Order: out}, nil
}
func (h *OrderHandler) RequestOrderPayment(ctx context.Context, req *prv1.RequestOrderPaymentRequest) (*prv1.RequestOrderPaymentResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.RequestOrderPayment(ctx, grpcx.TenantID(ctx), req.GetId(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	out := &prv1.PurchaseOrder{Id: req.GetId()}
	applyOrderContractState(out, v)
	return &prv1.RequestOrderPaymentResponse{Order: out}, nil
}

func (h *OrderHandler) CreateExternalPayable(ctx context.Context, req *prv1.CreateExternalPayableRequest) (*prv1.CreateExternalPayableResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	v, err := h.svc.CreateExternalPayable(ctx, grpcx.TenantID(ctx), app.ExternalPayableInput{BusinessType: req.GetBusinessType(), SourceBusinessID: req.GetSourceBusinessId(), PayeeID: req.GetPayeeId(), ExportContractNo: req.GetExportContractNo(), BusinessDocumentNo: req.GetBusinessDocumentNo(), PayeeName: req.GetPayeeName(), Currency: req.GetCurrency(), Amount: req.GetAmount(), DueDate: req.GetDueDate(), PaymentTerms: req.GetPaymentTerms(), SignedContractKey: req.GetSignedContractKey(), SignedContractName: req.GetSignedContractName()}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateExternalPayableResponse{Row: reconRowProto(v)}, nil
}
func (h *OrderHandler) GetExternalPayable(ctx context.Context, req *prv1.GetExternalPayableRequest) (*prv1.GetExternalPayableResponse, error) {
	v, err := h.svc.GetExternalPayable(ctx, grpcx.TenantID(ctx), req.GetBusinessType(), req.GetSourceBusinessId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetExternalPayableResponse{Row: reconRowProto(v)}, nil
}

func (h *OrderHandler) CreateOrder(ctx context.Context, req *prv1.CreateOrderRequest) (*prv1.CreateOrderResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.OrderLine, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		lines = append(lines, app.OrderLine{
			RequirementID: l.GetRequirementId(), Qty: l.GetQty(), UnitPrice: l.GetUnitPrice(),
		})
	}
	row, err := h.svc.CreateOrder(ctx, grpcx.TenantID(ctx), app.CreateOrderInput{
		SupplierID: req.GetSupplierId(), SupplierCode: req.GetSupplierCode(),
		SupplierName: req.GetSupplierName(), Currency: req.GetCurrency(),
		ExpectedDate: req.GetExpectedDate(), PayableDueDate: req.GetPayableDueDate(),
		Remark: req.GetRemark(), Lines: lines,
		FulfillmentMode: req.GetFulfillmentMode(), DeliveryLocationType: req.GetDeliveryLocationType(),
		DeliveryPortID: req.GetDeliveryPortId(), DeliveryPortCode: req.GetDeliveryPortCode(), DeliveryPortName: req.GetDeliveryPortName(),
		WarehouseID: req.GetWarehouseId(), WarehouseName: req.GetWarehouseName(), DeliveryAddress: req.GetDeliveryAddress(),
		SourceChangeReason: req.GetSourceChangeReason(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateOrderResponse{Id: row.ID, PoNo: row.PoNo, Status: row.Status}, nil
}

func (h *OrderHandler) UpdateOrder(ctx context.Context, req *prv1.UpdateOrderRequest) (*prv1.UpdateOrderResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.OrderLine, 0, len(req.GetLines()))
	for _, line := range req.GetLines() {
		lines = append(lines, app.OrderLine{
			RequirementID: line.GetRequirementId(), Qty: line.GetQty(), UnitPrice: line.GetUnitPrice(),
		})
	}
	row, err := h.svc.UpdateOrder(ctx, grpcx.TenantID(ctx), req.GetId(), app.CreateOrderInput{
		SupplierID: req.GetSupplierId(), SupplierCode: req.GetSupplierCode(),
		SupplierName: req.GetSupplierName(), Currency: req.GetCurrency(),
		ExpectedDate: req.GetExpectedDate(), PayableDueDate: req.GetPayableDueDate(),
		Remark: req.GetRemark(), Lines: lines,
		FulfillmentMode: req.GetFulfillmentMode(), DeliveryLocationType: req.GetDeliveryLocationType(),
		DeliveryPortID: req.GetDeliveryPortId(), DeliveryPortCode: req.GetDeliveryPortCode(), DeliveryPortName: req.GetDeliveryPortName(),
		WarehouseID: req.GetWarehouseId(), WarehouseName: req.GetWarehouseName(), DeliveryAddress: req.GetDeliveryAddress(),
		SourceChangeReason: req.GetSourceChangeReason(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.UpdateOrderResponse{Id: row.ID, PoNo: row.PoNo, Status: row.Status}, nil
}

func (h *OrderHandler) SubmitOrder(ctx context.Context, req *prv1.SubmitOrderRequest) (*prv1.SubmitOrderResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	status, instanceID, err := h.svc.SubmitOrder(ctx, grpcx.TenantID(ctx), req.GetId(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.SubmitOrderResponse{Status: status, ApprovalInstanceId: instanceID}, nil
}

func (h *OrderHandler) CancelOrder(ctx context.Context, req *prv1.CancelOrderRequest) (*prv1.CancelOrderResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.CancelOrder(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetReason(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.CancelOrderResponse{}, nil
}

func (h *OrderHandler) ReceiveOrder(ctx context.Context, req *prv1.ReceiveOrderRequest) (*prv1.ReceiveOrderResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.ReceiptLine, 0, len(req.GetLines()))
	for _, l := range req.GetLines() {
		lines = append(lines, app.ReceiptLine{POItemID: l.GetPoItemId(), Qty: l.GetQty()})
	}
	no, err := h.svc.ReceiveOrder(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetWarehouseId(),
		lines, req.GetRemark(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ReceiveOrderResponse{ReceiptNo: no}, nil
}

func (h *OrderHandler) GetOrderDocuments(ctx context.Context, req *prv1.GetOrderDocumentsRequest) (*prv1.GetOrderDocumentsResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	doc, err := h.svc.GetOrderDocuments(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.GetOrderDocumentsResponse{XlsxFileName: doc.XLSXFileName, XlsxData: doc.XLSXData, PdfFileName: doc.PDFFileName, PdfData: doc.PDFData, TemplateVersion: doc.Version}, nil
}

func (h *OrderHandler) BeginOrderSend(ctx context.Context, req *prv1.BeginOrderSendRequest) (*prv1.BeginOrderSendResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	id, err := h.svc.BeginOrderSend(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetRecipientEmail(), req.GetSenderEmployeeId(), req.GetSenderName())
	if err != nil {
		return nil, err
	}
	return &prv1.BeginOrderSendResponse{AttemptId: id}, nil
}

func (h *OrderHandler) CompleteOrderSend(ctx context.Context, req *prv1.CompleteOrderSendRequest) (*prv1.CompleteOrderSendResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	status, err := h.svc.CompleteOrderSend(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetAttemptId(), req.GetSuccess(), req.GetCampaignId(), req.GetCampaignNo(), req.GetErrorMessage(), req.GetAttachmentNames(), req.GetTemplateVersion())
	if err != nil {
		return nil, err
	}
	return &prv1.CompleteOrderSendResponse{SendStatus: status}, nil
}

func (h *OrderHandler) GetOrderExecution(ctx context.Context, req *prv1.GetOrderExecutionRequest) (*prv1.GetOrderExecutionResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	execution, err := h.svc.GetOrderExecution(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return executionToProto(execution), nil
}

func (h *OrderHandler) RecordSupplierConfirmation(ctx context.Context, req *prv1.RecordSupplierConfirmationRequest) (*prv1.RecordSupplierConfirmationResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	lines := make([]app.SupplierConfirmationLine, 0, len(req.GetLines()))
	for _, line := range req.GetLines() {
		lines = append(lines, app.SupplierConfirmationLine{POItemID: line.GetPoItemId(), ConfirmedQty: line.GetConfirmedQty(), ConfirmedUnitPrice: line.GetConfirmedUnitPrice()})
	}
	confirmation, err := h.svc.RecordSupplierConfirmation(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetConfirmedDate(), req.GetConfirmedExpectedDate(), req.GetRemark(), lines, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.RecordSupplierConfirmationResponse{Confirmation: confirmationToProto(confirmation)}, nil
}

func (h *OrderHandler) SaveProductionMilestone(ctx context.Context, req *prv1.SaveProductionMilestoneRequest) (*prv1.SaveProductionMilestoneResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	attachments := make([]app.ProductionAttachment, 0, len(req.GetAttachments()))
	for _, attachment := range req.GetAttachments() {
		attachments = append(attachments, app.ProductionAttachment{FileName: attachment.GetFileName(), FileURL: attachment.GetFileUrl(), ContentType: attachment.GetContentType()})
	}
	milestone, err := h.svc.SaveProductionMilestone(ctx, grpcx.TenantID(ctx), req.GetId(), app.ProductionMilestone{Node: req.GetNode(), PlannedDate: req.GetPlannedDate(), ActualDate: req.GetActualDate(), OwnerID: req.GetOwnerId(), OwnerName: req.GetOwnerName(), Remark: req.GetRemark(), Attachments: attachments}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.SaveProductionMilestoneResponse{Milestone: milestoneToProto(milestone)}, nil
}

func (h *OrderHandler) ReportReceiptException(ctx context.Context, req *prv1.ReportReceiptExceptionRequest) (*prv1.ReportReceiptExceptionResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	exception, err := h.svc.ReportReceiptException(ctx, grpcx.TenantID(ctx), req.GetId(), app.ReceiptException{ReceiptID: req.GetReceiptId(), POItemID: req.GetPoItemId(), Type: req.GetExceptionType(), Qty: req.GetQty(), ActualProduct: req.GetActualProduct(), ActualUOM: req.GetActualUom(), Description: req.GetDescription()}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ReportReceiptExceptionResponse{Exception: exceptionToProto(exception)}, nil
}

func (h *OrderHandler) ResolveReceiptException(ctx context.Context, req *prv1.ResolveReceiptExceptionRequest) (*prv1.ResolveReceiptExceptionResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	exception, err := h.svc.ResolveReceiptException(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetExceptionId(), req.GetResolution(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ResolveReceiptExceptionResponse{Exception: exceptionToProto(exception)}, nil
}

func (h *OrderHandler) RecordInspection(ctx context.Context, req *prv1.RecordInspectionRequest) (*prv1.RecordInspectionResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	attachments := make([]app.ProductionAttachment, 0, len(req.GetAttachments()))
	for _, attachment := range req.GetAttachments() {
		attachments = append(attachments, app.ProductionAttachment{FileName: attachment.GetFileName(), FileURL: attachment.GetFileUrl(), ContentType: attachment.GetContentType()})
	}
	inspection, err := h.svc.RecordInspection(ctx, grpcx.TenantID(ctx), req.GetId(), app.PurchaseInspection{
		ReceiptID: req.GetReceiptId(), POItemID: req.GetPoItemId(), Result: req.GetResult(),
		InspectedQty: req.GetInspectedQty(), DefectQty: req.GetDefectQty(), Note: req.GetNote(),
		Attachments: attachments,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.RecordInspectionResponse{Inspection: inspectionToProto(inspection)}, nil
}

func (h *OrderHandler) ResolveInspection(ctx context.Context, req *prv1.ResolveInspectionRequest) (*prv1.ResolveInspectionResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	inspection, err := h.svc.ResolveInspection(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetInspectionId(), req.GetDisposition(), req.GetDispositionNote(), app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.ResolveInspectionResponse{Inspection: inspectionToProto(inspection)}, nil
}

func (h *OrderHandler) CloseOrder(ctx context.Context, req *prv1.CloseOrderRequest) (*prv1.CloseOrderResponse, error) {
	if err := h.authorizeOrder(ctx, req.GetId()); err != nil {
		return nil, err
	}
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.CloseOrder(ctx, grpcx.TenantID(ctx), req.GetId(), app.CloseOrderInput{
		ShortfallAction: req.GetShortfallAction(), Note: req.GetCloseNote(),
	}, app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	head, err := h.svc.GetOrder(ctx, grpcx.TenantID(ctx), req.GetId())
	if err != nil {
		return nil, err
	}
	return &prv1.CloseOrderResponse{Order: &prv1.PurchaseOrder{
		Id: head.ID, PoNo: head.PoNo, Status: head.Status,
		ClosedAt: head.ClosedAt, ClosedBy: head.ClosedByName,
		CloseNote: head.CloseNote, ShortfallAction: head.ShortfallAction,
	}}, nil
}

func executionToProto(execution app.OrderExecution) *prv1.GetOrderExecutionResponse {
	out := &prv1.GetOrderExecutionResponse{}
	for _, row := range execution.Confirmations {
		out.Confirmations = append(out.Confirmations, confirmationToProto(row))
	}
	for _, row := range execution.Milestones {
		out.Milestones = append(out.Milestones, milestoneToProto(row))
	}
	for _, row := range execution.Exceptions {
		out.Exceptions = append(out.Exceptions, exceptionToProto(row))
	}
	for _, row := range execution.Reminders {
		out.Reminders = append(out.Reminders, &prv1.ProductionReminder{Id: row.ID, Node: row.Node, PlannedDate: row.PlannedDate, BuyerId: row.BuyerID, BuyerName: row.BuyerName, RelatedContracts: row.RelatedContracts, Status: row.Status, CreatedAt: row.CreatedAt})
	}
	for _, row := range execution.Inspections {
		out.Inspections = append(out.Inspections, inspectionToProto(row))
	}
	return out
}

func inspectionToProto(row app.PurchaseInspection) *prv1.PurchaseInspection {
	out := &prv1.PurchaseInspection{Id: row.ID, ReceiptId: row.ReceiptID, ReceiptNo: row.ReceiptNo, PoItemId: row.POItemID, Result: row.Result, InspectedQty: row.InspectedQty, DefectQty: row.DefectQty, Note: row.Note, Status: row.Status, Disposition: row.Disposition, DispositionNote: row.DispositionNote, InspectedBy: row.InspectedBy, InspectedAt: row.InspectedAt, ResolvedBy: row.ResolvedBy, ResolvedAt: row.ResolvedAt}
	for _, attachment := range row.Attachments {
		out.Attachments = append(out.Attachments, &prv1.ProductionAttachment{FileName: attachment.FileName, FileUrl: attachment.FileURL, ContentType: attachment.ContentType})
	}
	return out
}

func confirmationToProto(row app.SupplierConfirmation) *prv1.SupplierConfirmation {
	out := &prv1.SupplierConfirmation{Id: row.ID, Status: row.Status, ConfirmedDate: row.ConfirmedDate, ConfirmedExpectedDate: row.ExpectedDate, Remark: row.Remark, ApprovalInstanceId: row.ApprovalInstanceID, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt}
	for _, line := range row.Lines {
		out.Lines = append(out.Lines, &prv1.SupplierConfirmationLine{PoItemId: line.POItemID, ConfirmedQty: line.ConfirmedQty, ConfirmedUnitPrice: line.ConfirmedUnitPrice})
	}
	return out
}

func milestoneToProto(row app.ProductionMilestone) *prv1.ProductionMilestone {
	out := &prv1.ProductionMilestone{Id: row.ID, Node: row.Node, PlannedDate: row.PlannedDate, ActualDate: row.ActualDate, OwnerId: row.OwnerID, OwnerName: row.OwnerName, Remark: row.Remark, Delayed: row.Delayed, UpdatedAt: row.UpdatedAt}
	for _, attachment := range row.Attachments {
		out.Attachments = append(out.Attachments, &prv1.ProductionAttachment{FileName: attachment.FileName, FileUrl: attachment.FileURL, ContentType: attachment.ContentType})
	}
	return out
}

func exceptionToProto(row app.ReceiptException) *prv1.ReceiptException {
	return &prv1.ReceiptException{Id: row.ID, ReceiptId: row.ReceiptID, PoItemId: row.POItemID, ExceptionType: row.Type, Qty: row.Qty, ActualProduct: row.ActualProduct, ActualUom: row.ActualUOM, Description: row.Description, Status: row.Status, Resolution: row.Resolution, ReportedBy: row.ReportedBy, ReportedAt: row.ReportedAt, ResolvedBy: row.ResolvedBy, ResolvedAt: row.ResolvedAt}
}
