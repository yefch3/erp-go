// Package grpcout holds inventory's outbound dependencies. Inventory owns
// stock and nothing else; anything it needs from another service it asks for
// at write time rather than reading somebody else's tables.
package grpcout

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"

	commonv1 "github.com/sgao19/erp-go/gen/go/erp/common/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/services/inventory/internal/app"
)

type Numbering struct{ client mdv1.NumberingServiceClient }

func NewNumbering(conn *grpc.ClientConn) *Numbering {
	return &Numbering{client: mdv1.NewNumberingServiceClient(conn)}
}

type Catalog struct{ client pdv1.CatalogServiceClient }

func NewCatalog(conn *grpc.ClientConn) *Catalog {
	return &Catalog{client: pdv1.NewCatalogServiceClient(conn)}
}

// Resolve 使用产品服务的租户隔离查询解析产品编码和可选 SKU 编码。
func (c *Catalog) Resolve(ctx context.Context, productCode, skuCode string) (app.CatalogItem, error) {
	productCode, skuCode = strings.TrimSpace(productCode), strings.TrimSpace(skuCode)
	resp, err := c.client.ListProducts(ctx, &pdv1.ListProductsRequest{
		Keyword: productCode, Page: &commonv1.PageRequest{Page: 1, PageSize: 50},
	})
	if err != nil {
		return app.CatalogItem{}, err
	}
	var product *pdv1.Product
	for _, candidate := range resp.GetProducts() {
		if strings.EqualFold(strings.TrimSpace(candidate.GetCode()), productCode) && candidate.GetStatus() == "ACTIVE" {
			product = candidate
			break
		}
	}
	if product == nil {
		return app.CatalogItem{}, errors.New("active product not found")
	}
	item := app.CatalogItem{ProductID: product.GetId(), ProductCode: product.GetCode(), ProductName: product.GetName(), UomID: product.GetBaseUomId(), UomCode: product.GetBaseUomCode()}
	if skuCode == "" {
		return item, nil
	}
	detail, err := c.client.GetProduct(ctx, &pdv1.GetProductRequest{Id: product.GetId()})
	if err != nil {
		return app.CatalogItem{}, err
	}
	for _, sku := range detail.GetSkus() {
		if strings.EqualFold(strings.TrimSpace(sku.GetCode()), skuCode) && sku.GetStatus() == "ACTIVE" {
			item.SKUID, item.SKUCode = sku.GetId(), sku.GetCode()
			return item, nil
		}
	}
	return app.CatalogItem{}, errors.New("active sku not found")
}

func (n *Numbering) Next(ctx context.Context, bizType string) (string, error) {
	resp, err := n.client.NextNumber(ctx, &mdv1.NextNumberRequest{BizType: bizType})
	if err != nil {
		return "", err
	}
	return resp.GetNumber(), nil
}
