// Package grpcin exposes the catalog and its attachments over gRPC.
package grpcin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/pkg/grpcx"
	"github.com/sgao19/erp-go/services/product/internal/app"
	"github.com/sgao19/erp-go/services/product/internal/store"
)

type Handler struct {
	pdv1.UnimplementedCatalogServiceServer
	pdv1.UnimplementedAttachmentServiceServer
	svc *app.Service
}

func New(svc *app.Service) *Handler { return &Handler{svc: svc} }

func operator(ctx context.Context) int64 {
	op, _ := grpcx.OperatorFromContext(ctx)
	return op.EmployeeID
}

// ---------------------------------------------------------------- categories

func (h *Handler) ListCategories(ctx context.Context, req *pdv1.ListCategoriesRequest) (*pdv1.ListCategoriesResponse, error) {
	rows, err := h.svc.ListCategories(ctx, grpcx.TenantID(ctx), req.GetStatus())
	if err != nil {
		return nil, err
	}
	out := make([]*pdv1.Category, 0, len(rows))
	for _, r := range rows {
		out = append(out, categoryToProto(r))
	}
	return &pdv1.ListCategoriesResponse{Categories: out}, nil
}

func (h *Handler) CreateCategory(ctx context.Context, req *pdv1.CreateCategoryRequest) (*pdv1.CreateCategoryResponse, error) {
	c, err := h.svc.CreateCategory(ctx, grpcx.TenantID(ctx), req.GetCode(), req.GetName(),
		req.GetParentId(), req.GetSortOrder())
	if err != nil {
		return nil, err
	}
	return &pdv1.CreateCategoryResponse{Category: categoryToProto(c)}, nil
}

func (h *Handler) UpdateCategory(ctx context.Context, req *pdv1.UpdateCategoryRequest) (*pdv1.UpdateCategoryResponse, error) {
	c, err := h.svc.UpdateCategory(ctx, grpcx.TenantID(ctx), req.GetId(), req.GetName(), req.GetSortOrder())
	if err != nil {
		return nil, err
	}
	return &pdv1.UpdateCategoryResponse{Category: categoryToProto(c)}, nil
}

func (h *Handler) DeactivateCategory(ctx context.Context, req *pdv1.DeactivateCategoryRequest) (*pdv1.DeactivateCategoryResponse, error) {
	if err := h.svc.DeactivateCategory(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &pdv1.DeactivateCategoryResponse{Deactivated: true}, nil
}

func (h *Handler) ListUoms(ctx context.Context, _ *pdv1.ListUomsRequest) (*pdv1.ListUomsResponse, error) {
	rows, err := h.svc.ListUoms(ctx, grpcx.TenantID(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]*pdv1.Uom, 0, len(rows))
	for _, r := range rows {
		out = append(out, &pdv1.Uom{Id: r.ID, Code: r.Code, Name: r.Name, UomType: r.UomType})
	}
	return &pdv1.ListUomsResponse{Uoms: out}, nil
}

func (h *Handler) CreateUom(ctx context.Context, req *pdv1.CreateUomRequest) (*pdv1.CreateUomResponse, error) {
	u, err := h.svc.CreateUom(ctx, grpcx.TenantID(ctx), req.GetCode(), req.GetName(), req.GetUomType())
	if err != nil {
		return nil, err
	}
	return &pdv1.CreateUomResponse{Uom: &pdv1.Uom{Id: u.ID, Code: u.Code, Name: u.Name, UomType: u.UomType}}, nil
}

// ---------------------------------------------------------------- products

func (h *Handler) ListProducts(ctx context.Context, req *pdv1.ListProductsRequest) (*pdv1.ListProductsResponse, error) {
	rows, total, err := h.svc.ListProducts(ctx, grpcx.TenantID(ctx), req.GetKeyword(),
		req.GetCategoryId(), req.GetStatus(), req.GetPage().GetPage(), req.GetPage().GetPageSize())
	if err != nil {
		return nil, err
	}
	out := make([]*pdv1.Product, 0, len(rows))
	for _, r := range rows {
		out = append(out, &pdv1.Product{
			Id: r.ID, Code: r.Code, Name: r.Name, NameEn: r.NameEn,
			CategoryId: r.CategoryID, CategoryName: r.CategoryName,
			ProductType: r.ProductType, Brand: r.Brand,
			BaseUomId: r.BaseUomID, BaseUomCode: r.BaseUomCode,
			ReferencePrice: r.ReferencePrice, ReferenceCurrency: r.ReferenceCurrency,
			HsCode: r.HsCode, TaxRate: r.TaxRate, ExportRebateRate: r.ExportRebateRate,
			Description: r.Description, Status: r.Status,
		})
	}
	return &pdv1.ListProductsResponse{Products: out, Meta: &commonv1.PageMeta{Total: total}}, nil
}

func (h *Handler) GetProduct(ctx context.Context, req *pdv1.GetProductRequest) (*pdv1.GetProductResponse, error) {
	tenantID := grpcx.TenantID(ctx)
	p, err := h.svc.GetProduct(ctx, tenantID, req.GetId())
	if err != nil {
		return nil, err
	}
	skus, err := h.svc.ListSkus(ctx, tenantID, req.GetId())
	if err != nil {
		return nil, err
	}
	files, err := h.svc.ListAttachments(ctx, tenantID, req.GetId())
	if err != nil {
		return nil, err
	}
	return &pdv1.GetProductResponse{
		Product: productToProto(p), Skus: skusToProto(skus), Attachments: attachmentsToProto(files),
	}, nil
}

func (h *Handler) CreateProduct(ctx context.Context, req *pdv1.CreateProductRequest) (*pdv1.CreateProductResponse, error) {
	p, err := h.svc.CreateProduct(ctx, grpcx.TenantID(ctx), app.ProductInput{
		Code: req.GetCode(), Name: req.GetName(), NameEn: req.GetNameEn(),
		CategoryID: req.GetCategoryId(), ProductType: req.GetProductType(), Brand: req.GetBrand(),
		BaseUomID: req.GetBaseUomId(), ReferencePrice: req.GetReferencePrice(),
		ReferenceCurrency: req.GetReferenceCurrency(), HsCode: req.GetHsCode(),
		TaxRate: req.GetTaxRate(), ExportRebateRate: req.GetExportRebateRate(),
		Description: req.GetDescription(), OperatorID: operator(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &pdv1.CreateProductResponse{Product: productToProto(p)}, nil
}

func (h *Handler) UpdateProduct(ctx context.Context, req *pdv1.UpdateProductRequest) (*pdv1.UpdateProductResponse, error) {
	p, err := h.svc.UpdateProduct(ctx, grpcx.TenantID(ctx), req.GetId(), app.ProductInput{
		Name: req.GetName(), NameEn: req.GetNameEn(), CategoryID: req.GetCategoryId(),
		ProductType: req.GetProductType(), Brand: req.GetBrand(), BaseUomID: req.GetBaseUomId(),
		ReferencePrice: req.GetReferencePrice(), ReferenceCurrency: req.GetReferenceCurrency(),
		HsCode: req.GetHsCode(), TaxRate: req.GetTaxRate(),
		ExportRebateRate: req.GetExportRebateRate(), Description: req.GetDescription(),
		OperatorID: operator(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &pdv1.UpdateProductResponse{Product: productToProto(p)}, nil
}

func (h *Handler) DeactivateProduct(ctx context.Context, req *pdv1.DeactivateProductRequest) (*pdv1.DeactivateProductResponse, error) {
	if err := h.svc.DeactivateProduct(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx)); err != nil {
		return nil, err
	}
	return &pdv1.DeactivateProductResponse{Deactivated: true}, nil
}

func (h *Handler) ActivateProduct(ctx context.Context, req *pdv1.ActivateProductRequest) (*pdv1.ActivateProductResponse, error) {
	if err := h.svc.ActivateProduct(ctx, grpcx.TenantID(ctx), req.GetId(), operator(ctx)); err != nil {
		return nil, err
	}
	return &pdv1.ActivateProductResponse{Activated: true}, nil
}

// ---------------------------------------------------------------- skus

func (h *Handler) ListSkus(ctx context.Context, req *pdv1.ListSkusRequest) (*pdv1.ListSkusResponse, error) {
	rows, err := h.svc.ListSkus(ctx, grpcx.TenantID(ctx), req.GetProductId())
	if err != nil {
		return nil, err
	}
	return &pdv1.ListSkusResponse{Skus: skusToProto(rows)}, nil
}

func (h *Handler) CreateSku(ctx context.Context, req *pdv1.CreateSkuRequest) (*pdv1.CreateSkuResponse, error) {
	sku, err := h.svc.CreateSku(ctx, grpcx.TenantID(ctx), req.GetProductId(),
		req.GetCode(), req.GetSpec(), req.GetAttributes())
	if err != nil {
		return nil, err
	}
	return &pdv1.CreateSkuResponse{Sku: skuToProto(sku)}, nil
}

func (h *Handler) DeactivateSku(ctx context.Context, req *pdv1.DeactivateSkuRequest) (*pdv1.DeactivateSkuResponse, error) {
	if err := h.svc.DeactivateSku(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &pdv1.DeactivateSkuResponse{Deactivated: true}, nil
}

// ---------------------------------------------------------------- attachments

func (h *Handler) PresignUpload(ctx context.Context, req *pdv1.PresignUploadRequest) (*pdv1.PresignUploadResponse, error) {
	key, url, expires, err := h.svc.PresignUpload(ctx, grpcx.TenantID(ctx), req.GetProductId(),
		req.GetFileName(), req.GetContentType())
	if err != nil {
		return nil, err
	}
	return &pdv1.PresignUploadResponse{FileKey: key, UploadUrl: url, ExpiresInSeconds: expires}, nil
}

func (h *Handler) Register(ctx context.Context, req *pdv1.RegisterRequest) (*pdv1.RegisterResponse, error) {
	view, err := h.svc.Register(ctx, grpcx.TenantID(ctx), req.GetProductId(), req.GetFileKey(),
		req.GetFileName(), req.GetFileSize(), req.GetContentType(), operator(ctx))
	if err != nil {
		return nil, err
	}
	return &pdv1.RegisterResponse{Attachment: attachmentToProto(view)}, nil
}

func (h *Handler) ListAttachments(ctx context.Context, req *pdv1.ListAttachmentsRequest) (*pdv1.ListAttachmentsResponse, error) {
	views, err := h.svc.ListAttachments(ctx, grpcx.TenantID(ctx), req.GetProductId())
	if err != nil {
		return nil, err
	}
	return &pdv1.ListAttachmentsResponse{Attachments: attachmentsToProto(views)}, nil
}

func (h *Handler) Remove(ctx context.Context, req *pdv1.RemoveRequest) (*pdv1.RemoveResponse, error) {
	if err := h.svc.RemoveAttachment(ctx, grpcx.TenantID(ctx), req.GetId()); err != nil {
		return nil, err
	}
	return &pdv1.RemoveResponse{Removed: true}, nil
}

// ---------------------------------------------------------------- mapping

func ts(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func categoryToProto(c store.ProductCategory) *pdv1.Category {
	var parent int64
	if c.ParentID != nil {
		parent = *c.ParentID
	}
	return &pdv1.Category{
		Id: c.ID, Code: c.Code, Name: c.Name, ParentId: parent, Path: c.Path,
		Level: c.Level, SortOrder: c.SortOrder, Status: c.Status,
	}
}

func productToProto(p store.GetProductRow) *pdv1.Product {
	return &pdv1.Product{
		Id: p.ID, Code: p.Code, Name: p.Name, NameEn: p.NameEn,
		CategoryId: p.CategoryID, CategoryName: p.CategoryName,
		ProductType: p.ProductType, Brand: p.Brand,
		BaseUomId: p.BaseUomID, BaseUomCode: p.BaseUomCode,
		ReferencePrice: p.ReferencePrice, ReferenceCurrency: p.ReferenceCurrency,
		HsCode: p.HsCode, TaxRate: p.TaxRate, ExportRebateRate: p.ExportRebateRate,
		Description: p.Description, Status: p.Status,
	}
}

func skuToProto(s store.Sku) *pdv1.Sku {
	return &pdv1.Sku{
		Id: s.ID, ProductId: s.ProductID, Code: s.Code, Spec: s.Spec,
		Attributes: string(s.Attributes), Status: s.Status,
	}
}

func skusToProto(rows []store.Sku) []*pdv1.Sku {
	out := make([]*pdv1.Sku, 0, len(rows))
	for _, r := range rows {
		out = append(out, skuToProto(r))
	}
	return out
}

func attachmentToProto(v app.AttachmentView) *pdv1.Attachment {
	return &pdv1.Attachment{
		Id: v.Row.ID, ProductId: v.Row.ProductID, FileName: v.Row.FileName,
		FileKey: v.Row.FileKey, FileSize: v.Row.FileSize, ContentType: v.Row.ContentType,
		UploadedAt: ts(v.Row.UploadedAt), DownloadUrl: v.DownloadURL,
	}
}

func attachmentsToProto(views []app.AttachmentView) []*pdv1.Attachment {
	out := make([]*pdv1.Attachment, 0, len(views))
	for _, v := range views {
		out = append(out, attachmentToProto(v))
	}
	return out
}
