package grpcin

import (
	"context"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	prv1 "github.com/sgao19/erp-go/gen/go/erp/procurement/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
)

// OrderHandler exposes purchase orders. Separate from the requirement handler
// because they are separate services in the proto: reading what has to be
// bought and committing company money are different permissions.
type OrderHandler struct {
	prv1.UnimplementedPurchaseOrderServiceServer
	svc *app.Service
}

func NewOrders(svc *app.Service) *OrderHandler { return &OrderHandler{svc: svc} }

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
	rows, total, err := h.svc.ListOrders(ctx, grpcx.TenantID(ctx), app.OrderFilter{
		Status: req.GetStatus(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*prv1.PurchaseOrder, 0, len(rows))
	for _, r := range rows {
		out = append(out, &prv1.PurchaseOrder{
			Id: r.ID, PoNo: r.PoNo, SupplierId: r.SupplierID, SupplierName: r.SupplierName,
			Currency: r.Currency, TotalAmount: r.TotalAmount, ExpectedDate: r.ExpectedDate,
			Status: r.Status, BuyerName: r.BuyerName, Remark: r.Remark,
			RejectReason: r.RejectReason, CancelReason: r.CancelReason,
			ItemCount: r.ItemCount, TotalQty: r.TotalQty, ReceivedQty: r.ReceivedQty,
			CreatedAt: ts(r.CreatedAt),
		})
	}
	return &prv1.ListOrdersResponse{Orders: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *OrderHandler) GetOrder(ctx context.Context, req *prv1.GetOrderRequest) (*prv1.GetOrderResponse, error) {
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
	return &prv1.GetOrderResponse{
		Order: &prv1.PurchaseOrder{
			Id: head.ID, PoNo: head.PoNo, SupplierId: head.SupplierID,
			SupplierCode: head.SupplierCode, SupplierName: head.SupplierName,
			Currency: head.Currency, TotalAmount: head.TotalAmount,
			ExpectedDate: head.ExpectedDate, Status: head.Status,
			BuyerName: head.BuyerName, Remark: head.Remark,
			RejectReason: head.RejectReason, CancelReason: head.CancelReason,
			ApprovalInstanceId: head.ApprovalInstanceID, CreatedAt: ts(head.CreatedAt),
		},
		Items: outItems, Receipts: outReceipts,
	}, nil
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
		ExpectedDate: req.GetExpectedDate(), Remark: req.GetRemark(), Lines: lines,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.CreateOrderResponse{Id: row.ID, PoNo: row.PoNo, Status: row.Status}, nil
}

func (h *OrderHandler) UpdateOrder(ctx context.Context, req *prv1.UpdateOrderRequest) (*prv1.UpdateOrderResponse, error) {
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
		ExpectedDate: req.GetExpectedDate(), Remark: req.GetRemark(), Lines: lines,
	}, app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.UpdateOrderResponse{Id: row.ID, PoNo: row.PoNo, Status: row.Status}, nil
}

func (h *OrderHandler) SubmitOrder(ctx context.Context, req *prv1.SubmitOrderRequest) (*prv1.SubmitOrderResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	status, instanceID, err := h.svc.SubmitOrder(ctx, grpcx.TenantID(ctx), req.GetId(),
		app.Operator{ID: op.EmployeeID, Name: op.Name})
	if err != nil {
		return nil, err
	}
	return &prv1.SubmitOrderResponse{Status: status, ApprovalInstanceId: instanceID}, nil
}

func (h *OrderHandler) CancelOrder(ctx context.Context, req *prv1.CancelOrderRequest) (*prv1.CancelOrderResponse, error) {
	op, _ := grpcx.OperatorFromContext(ctx)
	if err := h.svc.CancelOrder(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetReason(),
		app.Operator{ID: op.EmployeeID, Name: op.Name}); err != nil {
		return nil, err
	}
	return &prv1.CancelOrderResponse{}, nil
}

func (h *OrderHandler) ReceiveOrder(ctx context.Context, req *prv1.ReceiveOrderRequest) (*prv1.ReceiveOrderResponse, error) {
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
