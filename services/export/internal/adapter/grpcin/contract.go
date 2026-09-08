package grpcin

import (
	"context"
	"github.com/sgao19/erp-go/pkg/apierr"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	exv1 "github.com/sgao19/erp-go/gen/go/erp/export/v1"
	"github.com/sgao19/erp-go/pkg/deadletter"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/export/internal/app"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// ContractHandler exposes contracts. It shares the app service with
// quotations, because generating a contract reads a quotation.
type ContractHandler struct {
	exv1.UnimplementedContractServiceServer
	svc  *app.Service
	dead *deadletter.Console
}

func NewContracts(svc *app.Service) *ContractHandler { return &ContractHandler{svc: svc} }

func (h *ContractHandler) ListContracts(ctx context.Context, req *exv1.ListContractsRequest) (*exv1.ListContractsResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read", "export:receipt:read"); err != nil {
		return nil, err
	}
	rows, total, err := h.svc.ListContracts(ctx, grpcx.TenantID(ctx), req.GetKeyword(),
		req.GetCustomerId(), req.GetStatus(), req.GetPage().GetPage(), req.GetPage().GetPageSize(),
		operator(ctx), req.GetSalesEmployeeId())
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.Contract, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.Contract{
			Id: r.ID, ContractNo: r.ContractNo, QuoteNo: r.QuoteNo,
			CustomerId: r.CustomerID, CustomerName: r.CustomerName, Status: r.Status,
			SalesEmployeeId: r.SalesEmployeeID, SalesEmployee: r.SalesEmployee,
			SignedAt: ts(r.SignedAt), EffectiveAt: ts(r.EffectiveAt),
			UpdatedAt: ts(r.UpdatedAt), CreatedAt: ts(r.CreatedAt), ReceivableDueDate: r.ReceivableDueDate,
			ExternalContractNo: r.ExternalContractNo, EntrySource: r.EntrySource,
			Currency:    r.Currency,
			TotalAmount: r.TotalAmount, BaseAmount: r.BaseAmount, VersionNo: r.VersionNo,
		})
	}
	owners, err := h.svc.ContractOwners(ctx, grpcx.TenantID(ctx), operator(ctx))
	if err != nil {
		return nil, err
	}
	options := make([]*exv1.ContractOwnerOption, 0, len(owners))
	for _, owner := range owners {
		options = append(options, &exv1.ContractOwnerOption{Id: owner.ID, Name: owner.Name})
	}
	return &exv1.ListContractsResponse{Contracts: out, Owners: options, Meta: &commonv1.PageMeta{Total: total}}, nil
}

// ListContractExecution 出一览表的出口半边（D2）。
func (h *ContractHandler) ListContractExecution(ctx context.Context, req *exv1.ListContractExecutionRequest) (*exv1.ListContractExecutionResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read", "export:receipt:read"); err != nil {
		return nil, err
	}
	canReadReceipts := h.svc.RequireAnyPermission(ctx, "export:receipt:read") == nil
	rows, total, err := h.svc.ListContractExecution(ctx, grpcx.TenantID(ctx), app.ExecutionFilter{
		Status: req.GetStatus(), CustomerID: req.GetCustomerId(), Keyword: req.GetKeyword(),
	}, req.GetPage().GetPage(), req.GetPage().GetPageSize(), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.ContractExecutionRow, 0, len(rows))
	for _, r := range rows {
		if !canReadReceipts {
			r.ReceivedAmount = ""
		}
		out = append(out, &exv1.ContractExecutionRow{
			ContractId: r.ContractID, ContractNo: r.ContractNo,
			CustomerId: r.CustomerID, CustomerName: r.CustomerName,
			SalesEmployeeId: r.SalesEmployeeID, SalesEmployee: r.SalesEmployee,
			Status: r.Status, EffectiveDate: r.EffectiveDate,
			Currency: r.Currency, TotalAmount: r.TotalAmount,
			ShippedAmount: r.ShippedAmount, ReceivedAmount: r.ReceivedAmount,
			DueDate: r.DueDate, OverdueDays: r.OverdueDays, DueUnset: r.DueUnset,
		})
	}
	return &exv1.ListContractExecutionResponse{Rows: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *ContractHandler) GetContract(ctx context.Context, req *exv1.GetContractRequest) (*exv1.GetContractResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read", "export:receipt:read"); err != nil {
		return nil, err
	}
	view, err := h.svc.GetContractFor(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetVersionId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	if h.svc.RequireAnyPermission(ctx, "export:receipt:read") != nil {
		view.Contract.OpeningReceivedAmount = ""
	}

	// Shipment progress is looked up separately: it lives beside the contract
	// rather than inside it, because an approved version is frozen and what
	// has shipped since keeps moving.
	progress, err := h.svc.ShipmentProgress(ctx, grpcx.TenantID(ctx), view.Contract.ID, view.Version.ID)
	if err != nil {
		return nil, err
	}
	shipments := make([]*exv1.ShipmentProgress, 0, len(progress))
	for _, p := range progress {
		shipments = append(shipments, &exv1.ShipmentProgress{
			LineNo: p.LineNo, ProductId: p.ProductID, SkuId: p.SkuID,
			ProductCode: p.ProductCode, ProductName: p.ProductName, UomCode: p.UomCode,
			Qty: p.Qty, ShippedQty: p.ShippedQty, RemainingQty: p.RemainingQty,
		})
	}
	accepted, err := h.svc.AcceptedContractOffer(ctx, grpcx.TenantID(ctx), view.Contract.ID)
	if err != nil {
		return nil, err
	}

	return &exv1.GetContractResponse{
		AcceptedOfferJson: accepted, Contract: contractToProto(view), Version: versionToProto(view.Version),
		Items: contractItemsToProto(view.Items), Versions: versionListToProto(view.Versions),
		Shipments: shipments,
	}, nil
}

func (h *ContractHandler) CreateContractFromQuotation(context.Context, *exv1.CreateContractFromQuotationRequest) (*exv1.CreateContractFromQuotationResponse, error) {
	return nil, apierr.Conflict("CONTRACT_ENTRY_REPLACED", "请在客户报价点击客户已确认，系统自动创建或打开外销合同")
}
func (h *ContractHandler) CreateContract(context.Context, *exv1.CreateContractRequest) (*exv1.CreateContractResponse, error) {
	return nil, apierr.Conflict("CONTRACT_ENTRY_REPLACED", "新合同从客户报价确认后自动创建；历史合同请使用录入执行中合同")
}
func (h *ContractHandler) ImportExistingContract(ctx context.Context, req *exv1.ImportExistingContractRequest) (*exv1.ImportExistingContractResponse, error) {
	view, err := h.svc.ImportExistingContract(ctx, grpcx.TenantID(ctx), app.ExistingContractInput{
		SignedFileKey: req.GetSignedFileKey(), SignedFileName: req.GetSignedFileName(),
		CustomerID: req.GetCustomerId(), Currency: req.GetCurrency(), Terms: termsFromProto(req.GetTerms()),
		Items: itemsFromProto(req.GetItems()), ExternalContractNo: req.GetExternalContractNo(),
		SalesEmployeeID: req.GetSalesEmployeeId(), SignedDate: req.GetSignedDate(), EffectiveDate: req.GetEffectiveDate(),
		OpeningReceivedAmount: req.GetOpeningReceivedAmount(), FilePending: req.GetFilePending(),
		ProcurementEmployeeID: req.GetProcurementEmployeeId(), SupplierID: req.GetSupplierId(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.ImportExistingContractResponse{
		Contract: contractToProto(view), Version: versionToProto(view.Version), Items: contractItemsToProto(view.Items),
	}, nil
}

func (h *ContractHandler) UpdateContract(ctx context.Context, req *exv1.UpdateContractRequest) (*exv1.UpdateContractResponse, error) {
	view, err := h.svc.UpdateContract(ctx, grpcx.TenantID(ctx), req.GetId(),
		termsFromProto(req.GetTerms()), itemsFromProto(req.GetItems()), app.ContractEditMeta{
			ExternalContractNo: req.GetExternalContractNo(), SignedDate: req.GetSignedDate(), EffectiveDate: req.GetEffectiveDate(),
		}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.UpdateContractResponse{
		Contract: contractToProto(view), Version: versionToProto(view.Version),
	}, nil
}

func (h *ContractHandler) SubmitContract(ctx context.Context, req *exv1.SubmitContractRequest) (*exv1.SubmitContractResponse, error) {
	status, instanceID, err := h.svc.SubmitContract(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.SubmitContractResponse{Status: status, ApprovalInstanceId: instanceID}, nil
}

func (h *ContractHandler) ChangeContract(ctx context.Context, req *exv1.ChangeContractRequest) (*exv1.ChangeContractResponse, error) {
	return nil, apierr.Conflict("EX_CONTRACT_FLOW_RETIRED", "当前合同流程不提供此操作")
}

func (h *ContractHandler) SignContract(ctx context.Context, req *exv1.SignContractRequest) (*exv1.SignContractResponse, error) {
	status, err := h.svc.SignContract(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetConditionStatus(), req.GetConditionConfirmedAt(), req.GetConditionConfirmationNote(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.SignContractResponse{Status: status}, nil
}

func (h *ContractHandler) CancelContract(ctx context.Context, req *exv1.CancelContractRequest) (*exv1.CancelContractResponse, error) {
	return nil, apierr.Conflict("EX_CONTRACT_FLOW_RETIRED", "当前合同流程不提供此操作")
}

// ---------------------------------------------------------------- mapping

func operator(ctx context.Context) app.Operator {
	op, _ := grpcx.OperatorFromContext(ctx)
	return app.Operator{ID: op.EmployeeID, Name: op.Name}
}

func termsFromProto(t *exv1.ContractTerms) app.Terms {
	return app.Terms{
		BuyerName: t.GetBuyerName(), BuyerAddress: t.GetBuyerAddress(),
		SellerName: t.GetSellerName(), SellerAddress: t.GetSellerAddress(),
		Incoterm: t.GetIncoterm(), PortOfLoading: t.GetPortOfLoading(),
		PortOfDischarge: t.GetPortOfDischarge(), PaymentMethod: t.GetPaymentMethod(),
		DeliveryDate: t.GetDeliveryDate(), Text: t.GetTerms(),
		ReceivableDueDate: t.GetReceivableDueDate(),
	}
}

func contractToProto(view app.ContractView) *exv1.Contract {
	c := view.Contract
	return &exv1.Contract{
		Id: c.ID, ContractNo: c.ContractNo, QuotationId: c.QuotationID, QuoteNo: c.QuoteNo,
		CustomerId: c.CustomerID, CustomerName: c.CustomerName,
		CurrentVersionId: c.CurrentVersionID, Status: c.Status,
		SalesEmployeeId: c.SalesEmployeeID, SalesEmployee: c.SalesEmployee,
		SignatureSource:      c.SignatureSource,
		ConditionConfirmedAt: c.ConditionConfirmedAt, ConditionConfirmationNote: c.ConditionConfirmationNote,
		ConditionConfirmedBy: c.ConditionConfirmedBy, ConditionConfirmedByName: c.ConditionConfirmedByName,
		ExternalContractNo: c.ExternalContractNo, EntrySource: c.EntrySource,
		OpeningReceivedAmount: c.OpeningReceivedAmount, FilePending: c.FilePending,
		SignedAt: ts(c.SignedAt), EffectiveAt: ts(c.EffectiveAt), CompletedAt: ts(c.CompletedAt),
		CreatedAt: ts(c.CreatedAt), ReceivableDueDate: c.ReceivableDueDate,
		Currency: view.Version.Currency, TotalAmount: view.Version.TotalAmount,
		BaseAmount: view.Version.BaseAmount, VersionNo: view.Version.VersionNo,
	}
}

func versionToProto(v store.GetContractVersionRow) *exv1.ContractVersion {
	return &exv1.ContractVersion{
		Id: v.ID, ContractId: v.ContractID, VersionNo: v.VersionNo,
		BuyerName: v.BuyerName, BuyerAddress: v.BuyerAddress,
		SellerName: v.SellerName, SellerAddress: v.SellerAddress,
		Currency: v.Currency, Incoterm: v.Incoterm,
		PortOfLoading: v.PortOfLoading, PortOfDischarge: v.PortOfDischarge,
		PaymentMethod: v.PaymentMethod, DeliveryDate: v.DeliveryDate, Terms: v.Terms,
		TotalAmount: v.TotalAmount, BaseAmount: v.BaseAmount,
		Fx: &exv1.FxSnapshot{
			Rate: v.FxRate, RateAt: ts(v.FxRateAt), Source: v.FxSource, BaseCurrency: v.FxBaseCurrency,
		},
		ChangeReason: v.ChangeReason, Status: v.Status, CreatedAt: ts(v.CreatedAt),
	}
}

// versionListToProto fills only what a history list shows; the full terms of
// an older version are fetched on demand by id.
func versionListToProto(rows []store.ListContractVersionsRow) []*exv1.ContractVersion {
	out := make([]*exv1.ContractVersion, 0, len(rows))
	for _, v := range rows {
		out = append(out, &exv1.ContractVersion{
			Id: v.ID, ContractId: v.ContractID, VersionNo: v.VersionNo, Currency: v.Currency,
			TotalAmount: v.TotalAmount, BaseAmount: v.BaseAmount, DeliveryDate: v.DeliveryDate,
			ChangeReason: v.ChangeReason, Status: v.Status, CreatedAt: ts(v.CreatedAt),
		})
	}
	return out
}

func contractItemsToProto(items []store.ListContractItemsRow) []*exv1.ContractItem {
	out := make([]*exv1.ContractItem, 0, len(items))
	for _, i := range items {
		var sku int64
		if i.SkuID != nil {
			sku = *i.SkuID
		}
		out = append(out, &exv1.ContractItem{
			Id: i.ID, LineNo: i.LineNo, ProductId: i.ProductID, SkuId: sku,
			ProductCode: i.ProductCode, ProductName: i.ProductName, Spec: i.Spec,
			Qty: i.Qty, UomId: i.UomID, UomCode: i.UomCode,
			UnitPrice: i.UnitPrice, Amount: i.Amount, HsCode: i.HsCode, Remark: i.Remark,
			OpeningProcuredQty: i.OpeningProcuredQty, OpeningArrivedQty: i.OpeningArrivedQty,
			OpeningShippedQty: i.OpeningShippedQty,
		})
	}
	return out
}

// ---------------------------------------------------------------- files

func (h *ContractHandler) PresignContractFile(ctx context.Context, req *exv1.PresignContractFileRequest) (*exv1.PresignContractFileResponse, error) {
	key, url, expires, err := h.svc.PresignContractFile(ctx, grpcx.TenantID(ctx),
		req.GetContractId(), req.GetFileName(), req.GetContentType(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.PresignContractFileResponse{FileKey: key, UploadUrl: url, ExpiresInSeconds: expires}, nil
}

func (h *ContractHandler) RegisterContractFile(ctx context.Context, req *exv1.RegisterContractFileRequest) (*exv1.RegisterContractFileResponse, error) {
	view, err := h.svc.RegisterContractFile(ctx, grpcx.TenantID(ctx), req.GetContractId(), app.FileInput{
		Key: req.GetFileKey(), FileName: req.GetFileName(), ContentType: req.GetContentType(),
		Size: req.GetSizeBytes(), Kind: req.GetKind(), VersionID: req.GetContractVersionId(),
	}, operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.RegisterContractFileResponse{File: fileToProto(view)}, nil
}

func (h *ContractHandler) ListContractFiles(ctx context.Context, req *exv1.ListContractFilesRequest) (*exv1.ListContractFilesResponse, error) {
	if err := h.svc.RequireAnyPermission(ctx, "export:quotation:read", "export:receipt:read"); err != nil {
		return nil, err
	}
	views, err := h.svc.ListContractFiles(ctx, grpcx.TenantID(ctx), req.GetContractId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*exv1.ContractFile, 0, len(views))
	for _, v := range views {
		out = append(out, fileToProto(v))
	}
	return &exv1.ListContractFilesResponse{Files: out}, nil
}

func (h *ContractHandler) RemoveContractFile(ctx context.Context, req *exv1.RemoveContractFileRequest) (*exv1.RemoveContractFileResponse, error) {
	removed, err := h.svc.RemoveContractFile(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.RemoveContractFileResponse{Removed: removed}, nil
}

func fileToProto(v app.FileView) *exv1.ContractFile {
	r := v.Row
	return &exv1.ContractFile{
		Id: r.ID, ContractId: r.ContractID, ContractVersionId: r.ContractVersionID,
		VersionNo: r.VersionNo, Kind: r.Kind, FileName: r.FileName,
		ContentType: r.ContentType, SizeBytes: r.SizeBytes,
		UploadedAt: ts(r.UploadedAt), UploaderName: r.UploaderName,
		DownloadUrl: v.DownloadURL, Source: r.Source,
	}
}

// ---------------------------------------------------------------- ownership

func (h *ContractHandler) TransferOwnership(ctx context.Context, req *exv1.TransferOwnershipRequest) (*exv1.TransferOwnershipResponse, error) {
	rows, err := h.svc.TransferOwnership(ctx, grpcx.TenantID(ctx), req.GetBizType(),
		req.GetBizId(), req.GetToEmployeeId(), req.GetReason(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.TransferOwnershipResponse{Transfers: transfersToProto(rows)}, nil
}

func (h *ContractHandler) ListOwnershipTransfers(ctx context.Context, req *exv1.ListOwnershipTransfersRequest) (*exv1.ListOwnershipTransfersResponse, error) {
	rows, err := h.svc.ListOwnershipTransfers(ctx, grpcx.TenantID(ctx),
		req.GetBizType(), req.GetBizId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.ListOwnershipTransfersResponse{Transfers: transfersToProto(rows)}, nil
}

func transfersToProto(rows []store.ListOwnershipTransfersRow) []*exv1.OwnershipTransfer {
	out := make([]*exv1.OwnershipTransfer, 0, len(rows))
	for _, r := range rows {
		out = append(out, &exv1.OwnershipTransfer{
			Id: r.ID, BizType: r.BizType, BizId: r.BizID, BizNo: r.BizNo,
			FromEmployeeId: r.FromEmployeeID, FromEmployee: r.FromEmployee,
			ToEmployeeId: r.ToEmployeeID, ToEmployee: r.ToEmployee,
			Reason: r.Reason, TransferredBy: r.TransferredBy,
			TransferredByName: r.TransferredByName, TransferredAt: ts(r.TransferredAt),
		})
	}
	return out
}

func (h *ContractHandler) CompleteContract(ctx context.Context, req *exv1.CompleteContractRequest) (*exv1.CompleteContractResponse, error) {
	state, err := h.svc.CompleteContract(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &exv1.CompleteContractResponse{Status: state}, nil
}

func (h *ContractHandler) PresignExistingContractFile(ctx context.Context, req *exv1.PresignExistingContractFileRequest) (*exv1.PresignExistingContractFileResponse, error) {
	key, url, expires, err := h.svc.PresignExistingContractFile(ctx, grpcx.TenantID(ctx), req.GetFileName())
	if err != nil {
		return nil, err
	}
	return &exv1.PresignExistingContractFileResponse{FileKey: key, UploadUrl: url, ExpiresInSeconds: expires}, nil
}
