package grpcout

import (
	"context"

	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/services/procurement/internal/app"
	"google.golang.org/grpc"
)

type Products struct{ client pdv1.CatalogServiceClient }

func NewProducts(conn *grpc.ClientConn) *Products {
	return &Products{client: pdv1.NewCatalogServiceClient(conn)}
}

func (p *Products) Get(ctx context.Context, id int64) (app.ProductIdentity, error) {
	resp, err := p.client.GetProduct(ctx, &pdv1.GetProductRequest{Id: id})
	if err != nil {
		return app.ProductIdentity{}, err
	}
	v := resp.GetProduct()
	return app.ProductIdentity{ID: v.GetId(), Name: v.GetName(), Status: v.GetStatus()}, nil
}
