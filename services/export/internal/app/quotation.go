// Package app holds the export use cases. The first of them is the
// quotation: what was offered, at which price, under which exchange rate.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// Customer is the slice of a customer this service needs. Export never reads
// masterdata's tables; it asks, and copies what it must display later.
type Customer struct {
	ID       int64
	Name     string
	Currency string
	Status   string
}

// Product is likewise the slice of a product a quotation line copies.
type Product struct {
	ID      int64
	Code    string
	Name    string
	UomID   int64
	UomCode string
	Status  string
}

// Rate is one exchange-rate reading, taken once per document.
type Rate struct {
	Rate   decimal.Decimal
	At     time.Time
	Source string
	Base   string
}

type Customers interface {
	Get(ctx context.Context, id int64) (Customer, error)
}

type Products interface {
	Get(ctx context.Context, id int64) (Product, error)
}

type Rates interface {
	Latest(ctx context.Context, currency string) (Rate, error)
}

type Numbering interface {
	Next(ctx context.Context, bizType string) (string, error)
}

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	customers Customers
	products  Products
	rates     Rates
	number    Numbering
}

func New(pool *pgxpool.Pool, c Customers, p Products, r Rates, n Numbering) *Service {
	return &Service{pool: pool, q: store.New(pool), customers: c, products: p, rates: r, number: n}
}

type ItemInput struct {
	ProductID, SkuID int64
	Spec             string
	Qty              string
	UnitPrice        string
	Remark           string
}

type QuotationInput struct {
	CustomerID                     int64
	Currency, Incoterm             string
	PortOfLoading, PortOfDischarge string
	PaymentMethod, ValidUntil      string
	Remark                         string
	Items                          []ItemInput
	OperatorID                     int64
	OperatorName                   string
}

// priced is one resolved line: client input plus the product facts and the
// computed amount.
type priced struct {
	in      ItemInput
	product Product
	qty     decimal.Decimal
	price   decimal.Decimal
	amount  decimal.Decimal
}

func (s *Service) resolve(ctx context.Context, in QuotationInput) (Customer, []priced, decimal.Decimal, error) {
	if in.CustomerID == 0 {
		return Customer{}, nil, decimal.Zero, apierr.Invalid("EX_CUSTOMER_REQUIRED", "客户必选")
	}
	if len(in.Items) == 0 {
		return Customer{}, nil, decimal.Zero, apierr.Invalid("EX_ITEMS_REQUIRED", "至少需要一条报价明细")
	}
	// Write-time validation against the owning services: a quotation may not
	// reference a customer or product that does not exist or is retired.
	customer, err := s.customers.Get(ctx, in.CustomerID)
	if err != nil {
		return Customer{}, nil, decimal.Zero, err
	}
	if customer.Status != "ACTIVE" {
		return Customer{}, nil, decimal.Zero, apierr.Invalid("EX_CUSTOMER_INACTIVE", "该客户已停用，不能报价")
	}

	lines := make([]priced, 0, len(in.Items))
	total := decimal.Zero
	for i, item := range in.Items {
		qty, err := decimal.NewFromString(item.Qty)
		if err != nil || qty.LessThanOrEqual(decimal.Zero) {
			return Customer{}, nil, decimal.Zero, apierr.Invalid("EX_QTY_INVALID", "数量必须是大于 0 的数字").
				WithMeta("line", itoa(i+1))
		}
		price, err := decimal.NewFromString(item.UnitPrice)
		if err != nil || price.IsNegative() {
			return Customer{}, nil, decimal.Zero, apierr.Invalid("EX_PRICE_INVALID", "单价必须是不小于 0 的数字").
				WithMeta("line", itoa(i+1))
		}
		product, err := s.products.Get(ctx, item.ProductID)
		if err != nil {
			return Customer{}, nil, decimal.Zero, err
		}
		if product.Status != "ACTIVE" {
			return Customer{}, nil, decimal.Zero, apierr.Invalid("EX_PRODUCT_INACTIVE", "产品已停用，不能报价").
				WithMeta("product", product.Code)
		}
		// Rounded per line, then summed: the customer sees line amounts, and
		// the total must be exactly their sum, not a re-rounded product.
		amount := qty.Mul(price).Round(2)
		total = total.Add(amount)
		lines = append(lines, priced{in: item, product: product, qty: qty, price: price, amount: amount})
	}
	return customer, lines, total, nil
}

func (s *Service) CreateQuotation(ctx context.Context, tenantID int64, in QuotationInput) (store.GetQuotationRow, []store.ListQuotationItemsRow, error) {
	if in.Currency == "" {
		return store.GetQuotationRow{}, nil, apierr.Invalid("EX_CURRENCY_REQUIRED", "币种必选")
	}
	customer, lines, total, err := s.resolve(ctx, in)
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	// The rate is read once, here, and stored with the document. Everything
	// downstream reads the stored copy, never the live rate.
	rate, err := s.rates.Latest(ctx, in.Currency)
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	quoteNo, err := s.number.Next(ctx, "QUOTATION")
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}

	var id int64
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		var err error
		id, err = q.CreateQuotation(ctx, store.CreateQuotationParams{
			TenantID: tenantID, QuoteNo: quoteNo, CustomerID: customer.ID,
			CustomerName: customer.Name, Currency: in.Currency, Incoterm: orDefault(in.Incoterm, "FOB"),
			PortOfLoading: in.PortOfLoading, PortOfDischarge: in.PortOfDischarge,
			PaymentMethod: in.PaymentMethod, ValidUntil: in.ValidUntil,
			FxRate: rate.Rate.String(), FxRateAt: tsFrom(rate.At),
			FxSource: rate.Source, FxBaseCurrency: rate.Base,
			TotalAmount: total.StringFixed(2), BaseAmount: baseAmount(total, rate).StringFixed(2),
			Remark: in.Remark, SalesEmployeeID: in.OperatorID, SalesEmployee: in.OperatorName,
			CreatedBy: in.OperatorID,
		})
		if err != nil {
			return translateUnique(err, "EX_QUOTE_NO_TAKEN", "报价单号已存在")
		}
		return writeItems(ctx, q, tenantID, id, lines)
	})
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	return s.GetQuotation(ctx, tenantID, id)
}

// UpdateQuotation replaces a draft's header and lines. The fx snapshot is
// deliberately not refreshed: it belongs to the document, and a draft that
// silently re-prices between edits would be worse than one that is stale.
func (s *Service) UpdateQuotation(ctx context.Context, tenantID, id int64, in QuotationInput) (store.GetQuotationRow, []store.ListQuotationItemsRow, error) {
	current, _, err := s.GetQuotation(ctx, tenantID, id)
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	if current.Status != "DRAFT" {
		return store.GetQuotationRow{}, nil, apierr.Conflict("EX_NOT_DRAFT", "只有草稿可以修改，已发送的报价请新建一版")
	}
	customer, lines, total, err := s.resolve(ctx, in)
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	rate := Rate{Source: current.FxSource, Base: current.FxBaseCurrency}
	if rate.Rate, err = decimal.NewFromString(current.FxRate); err != nil {
		return store.GetQuotationRow{}, nil, err
	}

	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		rows, err := q.UpdateQuotationHeader(ctx, store.UpdateQuotationHeaderParams{
			TenantID: tenantID, ID: id, CustomerID: customer.ID, CustomerName: customer.Name,
			Currency: orDefault(in.Currency, current.Currency), Incoterm: orDefault(in.Incoterm, "FOB"),
			PortOfLoading: in.PortOfLoading, PortOfDischarge: in.PortOfDischarge,
			PaymentMethod: in.PaymentMethod, ValidUntil: in.ValidUntil,
			TotalAmount: total.StringFixed(2), BaseAmount: baseAmount(total, rate).StringFixed(2),
			Remark: in.Remark, UpdatedBy: in.OperatorID,
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			return apierr.Conflict("EX_NOT_DRAFT", "只有草稿可以修改")
		}
		if err := q.DeleteQuotationItems(ctx, store.DeleteQuotationItemsParams{
			TenantID: tenantID, QuotationID: id,
		}); err != nil {
			return err
		}
		return writeItems(ctx, q, tenantID, id, lines)
	})
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	return s.GetQuotation(ctx, tenantID, id)
}

func writeItems(ctx context.Context, q *store.Queries, tenantID, quotationID int64, lines []priced) error {
	for i, l := range lines {
		var sku *int64
		if l.in.SkuID != 0 {
			sku = &l.in.SkuID
		}
		if err := q.AddQuotationItem(ctx, store.AddQuotationItemParams{
			TenantID: tenantID, QuotationID: quotationID, LineNo: int32(i + 1),
			ProductID: l.product.ID, SkuID: sku,
			ProductCode: l.product.Code, ProductName: l.product.Name, Spec: l.in.Spec,
			Qty: l.qty.String(), UomID: l.product.UomID, UomCode: l.product.UomCode,
			UnitPrice: l.price.String(), Amount: l.amount.StringFixed(2), Remark: l.in.Remark,
		}); err != nil {
			return err
		}
	}
	return nil
}

// baseAmount converts to the base currency at the snapshot rate. The rate is
// quoted as units of the document currency per base unit, so it divides.
func baseAmount(total decimal.Decimal, rate Rate) decimal.Decimal {
	if rate.Rate.IsZero() {
		return decimal.Zero
	}
	return total.Div(rate.Rate).Round(2)
}

func tsFrom(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func itoa(n int) string {
	return decimal.NewFromInt(int64(n)).String()
}

func translateUnique(err error, code, msg string) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return apierr.Conflict(code, msg)
	}
	return err
}

func (s *Service) GetQuotation(ctx context.Context, tenantID, id int64) (store.GetQuotationRow, []store.ListQuotationItemsRow, error) {
	q, err := s.q.GetQuotation(ctx, store.GetQuotationParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.GetQuotationRow{}, nil, apierr.NotFound("EX_QUOTE_NOT_FOUND", "报价单不存在")
		}
		return store.GetQuotationRow{}, nil, err
	}
	items, err := s.q.ListQuotationItems(ctx, store.ListQuotationItemsParams{TenantID: tenantID, QuotationID: id})
	return q, items, err
}

func (s *Service) ListQuotations(ctx context.Context, tenantID int64, keyword string, customerID int64, status string, page, size int32) ([]store.ListQuotationsRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListQuotations(ctx, store.ListQuotationsParams{
		TenantID: tenantID, Keyword: keyword, CustomerID: customerID, Status: status,
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
