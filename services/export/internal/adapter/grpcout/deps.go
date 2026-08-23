// Package grpcout holds export's outbound dependencies. Every one of them is
// a question asked at write time - never a read of somebody else's tables.
package grpcout

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"

	fxv1 "github.com/sgao19/erp-go/gen/go/erp/fx/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	pdv1 "github.com/sgao19/erp-go/gen/go/erp/product/v1"
	"github.com/sgao19/erp-go/services/export/internal/app"
)

type Customers struct{ client mdv1.CustomerServiceClient }

func NewCustomers(conn *grpc.ClientConn) *Customers {
	return &Customers{client: mdv1.NewCustomerServiceClient(conn)}
}

func (c *Customers) Get(ctx context.Context, id int64) (app.Customer, error) {
	resp, err := c.client.GetCustomer(ctx, &mdv1.GetCustomerRequest{Id: id})
	if err != nil {
		return app.Customer{}, err
	}
	cu := resp.GetCustomer()
	contacts := make([]app.Contact, 0, len(cu.GetContacts()))
	for i, c := range cu.GetContacts() {
		// masterdata returns contacts without ids on the customer message, so
		// position is the stable handle within one customer's list.
		contacts = append(contacts, app.Contact{
			ID: int64(i + 1), Name: c.GetName(), Email: c.GetEmail(), IsPrimary: c.GetIsPrimary(),
		})
	}
	return app.Customer{
		ID: cu.GetId(), Name: cu.GetName(), Address: cu.GetAddress(),
		Currency: cu.GetCurrency(), Status: cu.GetStatus(), Contacts: contacts,
		PaymentDays: cu.GetPaymentDays(),
	}, nil
}

type Products struct{ client pdv1.CatalogServiceClient }

func NewProducts(conn *grpc.ClientConn) *Products {
	return &Products{client: pdv1.NewCatalogServiceClient(conn)}
}

func (p *Products) Get(ctx context.Context, id int64) (app.Product, error) {
	resp, err := p.client.GetProduct(ctx, &pdv1.GetProductRequest{Id: id})
	if err != nil {
		return app.Product{}, err
	}
	pr := resp.GetProduct()
	return app.Product{
		ID: pr.GetId(), Code: pr.GetCode(), Name: pr.GetName(),
		UomID: pr.GetBaseUomId(), UomCode: pr.GetBaseUomCode(),
		HsCode: pr.GetHsCode(), Status: pr.GetStatus(),
	}, nil
}

type Rates struct{ client fxv1.FxServiceClient }

func NewRates(conn *grpc.ClientConn) *Rates {
	return &Rates{client: fxv1.NewFxServiceClient(conn)}
}

// Latest reads the rate once, at document creation. The document then keeps
// its own copy: fx is asked, never joined.
func (r *Rates) Latest(ctx context.Context, currency string) (app.Rate, error) {
	// A document already in the base currency needs no lookup, and asking for
	// USD/USD would be a rate that does not exist.
	if currency == "USD" {
		return app.Rate{Rate: decimal.NewFromInt(1), At: time.Now().UTC(), Source: "BASE", Base: "USD"}, nil
	}
	resp, err := r.client.GetLatestRate(ctx, &fxv1.GetLatestRateRequest{QuoteCurrency: currency})
	if err != nil {
		return app.Rate{}, err
	}
	rate := resp.GetRate()
	value, err := decimal.NewFromString(rate.GetUnitsPerUsd())
	if err != nil {
		return app.Rate{}, err
	}
	at, err := time.Parse(time.RFC3339, rate.GetFetchedAt())
	if err != nil {
		at = time.Now().UTC()
	}
	return app.Rate{Rate: value, At: at, Source: rate.GetSource(), Base: rate.GetBaseCurrency()}, nil
}

type Numbering struct{ client mdv1.NumberingServiceClient }

func NewNumbering(conn *grpc.ClientConn) *Numbering {
	return &Numbering{client: mdv1.NewNumberingServiceClient(conn)}
}

func (n *Numbering) Next(ctx context.Context, bizType string) (string, error) {
	resp, err := n.client.NextNumber(ctx, &mdv1.NextNumberRequest{BizType: bizType})
	if err != nil {
		return "", err
	}
	return resp.GetNumber(), nil
}
