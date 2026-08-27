package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/product/internal/store"
)

// ProductInput is the write side of a product. Numeric fields are decimal
// strings ("" means "not set") so no price ever passes through float64.
type ProductInput struct {
	Code, Name, NameEn string
	CategoryID         int64
	ProductType, Brand string
	BaseUomID          int64
	ReferencePrice     string
	ReferenceCurrency  string
	HsCode             string
	TaxRate            string
	ExportRebateRate   string
	Description        string
	OperatorID         int64
}

func (in ProductInput) validate() error {
	switch {
	case in.Name == "":
		return apierr.Invalid("PD_NAME_REQUIRED", "产品名称必填")
	case in.CategoryID == 0:
		return apierr.Invalid("PD_CATEGORY_REQUIRED", "产品分类必选")
	case in.BaseUomID == 0:
		return apierr.Invalid("PD_UOM_REQUIRED", "基本单位必选")
	}
	switch in.ProductType {
	case "", "FINISHED", "SEMI", "MATERIAL", "SERVICE":
	default:
		return apierr.Invalid("PD_TYPE_INVALID", "产品类型不合法")
	}
	for label, v := range map[string]string{
		"参考价": in.ReferencePrice, "税率": in.TaxRate, "退税率": in.ExportRebateRate,
	} {
		if err := requireDecimal(label, v); err != nil {
			return err
		}
	}
	if in.ReferenceCurrency != "" && len(in.ReferenceCurrency) != 3 {
		return apierr.Invalid("PD_CURRENCY_INVALID", "币种必须是 3 位 ISO 代码")
	}
	return nil
}

// requireDecimal rejects anything the database would refuse anyway, but with
// a business error instead of a SQL error surfacing to the user.
func requireDecimal(label, v string) error {
	if v == "" {
		return nil
	}
	seenDot := false
	for i, r := range v {
		switch {
		case r >= '0' && r <= '9':
		case r == '-' && i == 0:
		case r == '.' && !seenDot:
			seenDot = true
		default:
			return apierr.Invalid("PD_NUMBER_INVALID", label+"必须是数字")
		}
	}
	return nil
}

func (in *ProductInput) applyDefaults() {
	if in.ProductType == "" {
		in.ProductType = "FINISHED"
	}
	if in.ReferenceCurrency == "" {
		in.ReferenceCurrency = "USD"
	}
	in.ReferenceCurrency = strings.ToUpper(in.ReferenceCurrency)
}

func (s *Service) CreateProduct(ctx context.Context, tenantID int64, in ProductInput) (store.GetProductRow, error) {
	if err := in.validate(); err != nil {
		return store.GetProductRow{}, err
	}
	in.applyDefaults()

	// An empty code means "issue one". Unlike customers, the number comes
	// from the masterdata numbering service over gRPC, so it is fetched
	// before the insert rather than inside its transaction.
	code := in.Code
	if code == "" {
		var err error
		if code, err = s.number.Next(ctx, "PRODUCT"); err != nil {
			return store.GetProductRow{}, err
		}
	}
	id, err := s.q.CreateProduct(ctx, store.CreateProductParams{
		TenantID: tenantID, Code: code, Name: in.Name, NameEn: in.NameEn,
		CategoryID: in.CategoryID, ProductType: in.ProductType, Brand: in.Brand,
		BaseUomID: in.BaseUomID, ReferencePrice: in.ReferencePrice,
		ReferenceCurrency: in.ReferenceCurrency, HsCode: in.HsCode,
		TaxRate: in.TaxRate, ExportRebateRate: in.ExportRebateRate,
		Description: in.Description, CreatedBy: in.OperatorID,
	})
	if err != nil {
		return store.GetProductRow{}, translateForeignKey(translateUnique(err, "PD_CODE_TAKEN", "产品编码已存在"))
	}
	return s.GetProduct(ctx, tenantID, id)
}

func (s *Service) UpdateProduct(ctx context.Context, tenantID, id int64, in ProductInput) (store.GetProductRow, error) {
	if err := in.validate(); err != nil {
		return store.GetProductRow{}, err
	}
	in.applyDefaults()
	rows, err := s.q.UpdateProduct(ctx, store.UpdateProductParams{
		TenantID: tenantID, ID: id, Name: in.Name, NameEn: in.NameEn,
		CategoryID: in.CategoryID, ProductType: in.ProductType, Brand: in.Brand,
		BaseUomID: in.BaseUomID, ReferencePrice: in.ReferencePrice,
		ReferenceCurrency: in.ReferenceCurrency, HsCode: in.HsCode,
		TaxRate: in.TaxRate, ExportRebateRate: in.ExportRebateRate,
		Description: in.Description, UpdatedBy: in.OperatorID,
	})
	if err != nil {
		return store.GetProductRow{}, translateForeignKey(err)
	}
	if rows == 0 {
		return store.GetProductRow{}, apierr.NotFound("PD_NOT_FOUND", "产品不存在")
	}
	return s.GetProduct(ctx, tenantID, id)
}

func (s *Service) GetProduct(ctx context.Context, tenantID, id int64) (store.GetProductRow, error) {
	p, err := s.q.GetProduct(ctx, store.GetProductParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetProductRow{}, apierr.NotFound("PD_NOT_FOUND", "产品不存在")
	}
	return p, err
}

// GetProducts 一次取一批，形状和 GetProduct 一致。
//
// **查不到的 id 不报错，就是不在结果里。** 调用方按 id 对一遍就知道少了谁，
// 而在这里报一句「其中某个不存在」，它还得再问一遍是哪一个。
//
// 和 GetProduct 一样不按状态过滤：调用方要拿到停用的产品，才说得出
// 「产品已停用」而不是「产品不存在」。
func (s *Service) GetProducts(ctx context.Context, tenantID int64, ids []int64) ([]store.GetProductsRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.q.GetProducts(ctx, store.GetProductsParams{TenantID: tenantID, Ids: ids})
}

func (s *Service) ListProducts(ctx context.Context, tenantID int64, keyword string, categoryID int64, status string, page, size int32) ([]store.ListProductsRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListProducts(ctx, store.ListProductsParams{
		TenantID: tenantID, Keyword: keyword, CategoryID: categoryID, Status: status,
		RowLimit: size, RowOffset: (page - 1) * size,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

// DeactivateProduct is the only removal there is: a product quoted on a
// contract or held in stock must stay resolvable forever.
func (s *Service) DeactivateProduct(ctx context.Context, tenantID, id, operatorID int64) error {
	rows, err := s.q.DeactivateProduct(ctx, store.DeactivateProductParams{
		TenantID: tenantID, ID: id, UpdatedBy: operatorID,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return apierr.NotFound("PD_NOT_FOUND", "产品不存在或已停用")
	}
	return nil
}

func (s *Service) ActivateProduct(ctx context.Context, tenantID, id, operatorID int64) error {
	rows, err := s.q.ActivateProduct(ctx, store.ActivateProductParams{
		TenantID: tenantID, ID: id, UpdatedBy: operatorID,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return apierr.NotFound("PD_NOT_FOUND", "产品不存在或已是启用状态")
	}
	return nil
}

// ---------------------------------------------------------------- skus

func (s *Service) ListSkus(ctx context.Context, tenantID, productID int64) ([]store.Sku, error) {
	return s.q.ListSkus(ctx, store.ListSkusParams{TenantID: tenantID, ProductID: productID})
}

func (s *Service) CreateSku(ctx context.Context, tenantID, productID int64, code, spec, attributes string) (store.Sku, error) {
	if productID == 0 || code == "" {
		return store.Sku{}, apierr.Invalid("PD_SKU_FIELDS_REQUIRED", "SKU 编码必填")
	}
	if attributes == "" {
		attributes = "{}"
	}
	sku, err := s.q.CreateSku(ctx, store.CreateSkuParams{
		TenantID: tenantID, ProductID: productID, Code: code, Spec: spec, Attributes: []byte(attributes),
	})
	if err != nil {
		return store.Sku{}, translateForeignKey(translateUnique(err, "PD_SKU_CODE_TAKEN", "SKU 编码已存在"))
	}
	return sku, nil
}

func (s *Service) DeactivateSku(ctx context.Context, tenantID, id int64) error {
	rows, err := s.q.DeactivateSku(ctx, store.DeactivateSkuParams{TenantID: tenantID, ID: id})
	if err != nil {
		return err
	}
	if rows == 0 {
		return apierr.NotFound("PD_SKU_NOT_FOUND", "SKU 不存在或已停用")
	}
	return nil
}

// translateForeignKey turns "category 999 does not exist" into a business
// error instead of a 500 with a constraint name in it.
func translateForeignKey(err error) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23503" {
		return apierr.Invalid("PD_REFERENCE_INVALID", "所选分类、单位或产品不存在")
	}
	return err
}
