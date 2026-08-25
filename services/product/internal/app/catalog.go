// Package app holds the product use cases: the catalog (categories, units,
// products, SKUs) and the attachments that hang off a product.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/product/internal/store"
)

// Numbering issues product codes; implemented by the masterdata gRPC client.
// Product does not run its own sequence: one numbering service owns them all.
type Numbering interface {
	Next(ctx context.Context, bizType string) (string, error)
}

// Files is the object storage this service hands presigned URLs for.
type Files interface {
	PresignPut(ctx context.Context, key string) (url string, expiresInSeconds int32, err error)
	PresignGet(ctx context.Context, key string) (string, error)
	Remove(ctx context.Context, key string) error
}

type Service struct {
	pool   *pgxpool.Pool
	q      *store.Queries
	number Numbering
	files  Files
}

func New(pool *pgxpool.Pool, number Numbering, files Files) *Service {
	return &Service{pool: pool, q: store.New(pool), number: number, files: files}
}

// ---------------------------------------------------------------- categories

func (s *Service) ListCategories(ctx context.Context, tenantID int64, status string) ([]store.ProductCategory, error) {
	rows, err := s.q.ListCategories(ctx, store.ListCategoriesParams{TenantID: tenantID, Column2: status})
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	// 空 ≠ 该空：也可能只是这家公司还没被播过种。见 catalogseed.go。
	if err := s.seedDefaultCategories(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.q.ListCategories(ctx, store.ListCategoriesParams{TenantID: tenantID, Column2: status})
}

func (s *Service) CreateCategory(ctx context.Context, tenantID int64, code, name string, parentID int64, sortOrder int32) (store.ProductCategory, error) {
	if code == "" || name == "" {
		return store.ProductCategory{}, apierr.Invalid("PD_CATEGORY_FIELDS_REQUIRED", "分类编码和名称必填")
	}
	// The path is materialized from the parent so a subtree query stays a
	// single LIKE instead of a recursive walk.
	path, level := "/", int32(1)
	if parentID != 0 {
		parent, err := s.q.GetCategory(ctx, store.GetCategoryParams{TenantID: tenantID, ID: parentID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return store.ProductCategory{}, apierr.NotFound("PD_CATEGORY_NOT_FOUND", "上级分类不存在")
			}
			return store.ProductCategory{}, err
		}
		path, level = fmt.Sprintf("%s%d/", parent.Path, parent.ID), parent.Level+1
	}
	var parent *int64
	if parentID != 0 {
		parent = &parentID
	}
	c, err := s.q.CreateCategory(ctx, store.CreateCategoryParams{
		TenantID: tenantID, Code: code, Name: name, ParentID: parent,
		Path: path, Level: level, SortOrder: sortOrder,
	})
	if err != nil {
		return store.ProductCategory{}, translateUnique(err, "PD_CATEGORY_CODE_TAKEN", "分类编码已存在")
	}
	return c, nil
}

func (s *Service) UpdateCategory(ctx context.Context, tenantID, id int64, name string, sortOrder int32) (store.ProductCategory, error) {
	if name == "" {
		return store.ProductCategory{}, apierr.Invalid("PD_CATEGORY_FIELDS_REQUIRED", "分类名称必填")
	}
	c, err := s.q.UpdateCategory(ctx, store.UpdateCategoryParams{
		TenantID: tenantID, ID: id, Name: name, SortOrder: sortOrder,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ProductCategory{}, apierr.NotFound("PD_CATEGORY_NOT_FOUND", "分类不存在")
	}
	return c, err
}

func (s *Service) DeactivateCategory(ctx context.Context, tenantID, id int64) error {
	// Refusing to hide a category that still has products keeps the tree
	// honest: a product must always resolve to a visible category.
	n, err := s.q.CountProductsInCategory(ctx, store.CountProductsInCategoryParams{TenantID: tenantID, CategoryID: id})
	if err != nil {
		return err
	}
	if n > 0 {
		return apierr.Conflict("PD_CATEGORY_IN_USE", "该分类下还有产品，不能停用").
			WithMeta("products", fmt.Sprint(n))
	}
	rows, err := s.q.DeactivateCategory(ctx, store.DeactivateCategoryParams{TenantID: tenantID, ID: id})
	if err != nil {
		return err
	}
	if rows == 0 {
		return apierr.NotFound("PD_CATEGORY_NOT_FOUND", "分类不存在或已停用")
	}
	return nil
}

// ---------------------------------------------------------------- units

func (s *Service) ListUoms(ctx context.Context, tenantID int64) ([]store.Uom, error) {
	rows, err := s.q.ListUoms(ctx, tenantID)
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	// 空 ≠ 该空。这里的空还格外贵：没有单位就一个产品都建不出来，而界面上
	// 没有新增单位的入口。见 catalogseed.go。
	if err := s.seedDefaultUoms(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.q.ListUoms(ctx, tenantID)
}

func (s *Service) CreateUom(ctx context.Context, tenantID int64, code, name, uomType string) (store.Uom, error) {
	if code == "" || name == "" {
		return store.Uom{}, apierr.Invalid("PD_UOM_FIELDS_REQUIRED", "单位编码和名称必填")
	}
	switch uomType {
	case "COUNT", "WEIGHT", "VOLUME", "LENGTH":
	default:
		return store.Uom{}, apierr.Invalid("PD_UOM_TYPE_INVALID", "单位类型必须是 COUNT / WEIGHT / VOLUME / LENGTH")
	}
	u, err := s.q.CreateUom(ctx, store.CreateUomParams{
		TenantID: tenantID, Code: strings.ToUpper(code), Name: name, UomType: uomType,
	})
	if err != nil {
		return store.Uom{}, translateUnique(err, "PD_UOM_CODE_TAKEN", "单位编码已存在")
	}
	return u, nil
}

func translateUnique(err error, code, msg string) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return apierr.Conflict(code, msg)
	}
	return err
}

func normalizePage(page, size int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}
