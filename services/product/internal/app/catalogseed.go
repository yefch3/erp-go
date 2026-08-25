package app

import (
	"context"
	"fmt"

	"github.com/sgao19/erp-go/services/product/internal/store"
)

// 计量单位和产品分类，逐行取自给第一家公司播种的迁移（00001_product.sql）。
// 那条迁移的注释写着播种的理由：「a company can create its first product
// without setup」——理由是对的，只是当时把它写给了 tenant_id = 1。
//
// 两处必须一致：迁移是第一家的事实，这两张表是其余每家的事实。
var defaultUoms = []struct {
	Code string
	Name string
	Type string
}{
	{"PCS", "个", "COUNT"},
	{"SET", "套", "COUNT"},
	{"CTN", "箱", "COUNT"},
	{"PR", "双", "COUNT"},
	{"KG", "千克", "WEIGHT"},
	{"TON", "吨", "WEIGHT"},
	{"CBM", "立方米", "VOLUME"},
	{"M", "米", "LENGTH"},
}

// 一条兜底分类。路径 "/"、层级 1 与 CreateCategory 建根分类时算出来的完全
// 相同，所以播出来的这条和人手建的那条是同一种东西。
var defaultCategories = []struct {
	Code      string
	Name      string
	Path      string
	Level     int32
	SortOrder int32
}{
	{"DEFAULT", "未分类", "/", 1, 999},
}

// seedDefaultUoms 给「一个单位都没有」的公司补上默认单位。
//
// 这是「只给第一家公司播种」里最硬的一处：建产品时基本单位必选（product.go
// 的 PD_UOM_REQUIRED），而第二家公司一个单位都没有，于是**一个产品都建不
// 出来**；更糟的是界面上根本没有新增单位的入口，接口有、页面没有，人在里面
// 转一圈找不到任何出路。
//
// 只在一条都没有时补。单位可以被停用，停用是有人做过的决定。
func (s *Service) seedDefaultUoms(ctx context.Context, tenantID int64) error {
	n, err := s.q.CountUoms(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("product: count uoms: %w", err)
	}
	if n > 0 {
		return nil
	}
	for _, u := range defaultUoms {
		if _, err := s.q.InsertUomIfAbsent(ctx, store.InsertUomIfAbsentParams{
			TenantID: tenantID, Code: u.Code, Name: u.Name, UomType: u.Type,
		}); err != nil {
			return fmt.Errorf("product: seed uom %s: %w", u.Code, err)
		}
	}
	return nil
}

// seedDefaultCategories 给「一个分类都没有」的公司补上兜底分类。
//
// 同样卡在建产品那一步（PD_CATEGORY_REQUIRED）。分类在产品页面上可以自己
// 建，所以这处比单位轻——但让第二家公司在建第一个产品之前先去建一个叫「未
// 分类」的分类，只是把第一家公司不必做的事推给了它。
func (s *Service) seedDefaultCategories(ctx context.Context, tenantID int64) error {
	n, err := s.q.CountCategories(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("product: count categories: %w", err)
	}
	if n > 0 {
		return nil
	}
	for _, c := range defaultCategories {
		if _, err := s.q.InsertCategoryIfAbsent(ctx, store.InsertCategoryIfAbsentParams{
			TenantID: tenantID, Code: c.Code, Name: c.Name,
			Path: c.Path, Level: c.Level, SortOrder: c.SortOrder,
		}); err != nil {
			return fmt.Errorf("product: seed category %s: %w", c.Code, err)
		}
	}
	return nil
}
