package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/export/internal/store"
)

// pickContact returns the addressee: the one asked for, or the primary when
// none was chosen. Choosing a contact who works for a different customer is
// refused rather than silently ignored.
func pickContact(customer Customer, contactID int64) (Contact, error) {
	if contactID != 0 {
		for _, c := range customer.Contacts {
			if c.ID == contactID {
				return c, nil
			}
		}
		return Contact{}, apierr.Invalid("EX_CONTACT_NOT_FOUND", "所选联系人不属于该客户")
	}
	for _, c := range customer.Contacts {
		if c.IsPrimary {
			return c, nil
		}
	}
	if len(customer.Contacts) > 0 {
		return customer.Contacts[0], nil
	}
	return Contact{}, nil
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
	ContactID                      int64
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
	contact, err := pickContact(customer, in.ContactID)
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
			CustomerName: customer.Name,
			ContactID:    contact.ID, ContactName: contact.Name, ContactEmail: contact.Email,
			Currency: in.Currency, Incoterm: orDefault(in.Incoterm, "FOB"),
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
	if err := s.mayWrite(ctx, Operator{ID: in.OperatorID, Name: in.OperatorName},
		current.SalesEmployeeID, "EX_QUOTE_NOT_OWNER", "只能修改自己负责的报价单"); err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	if current.Status != "DRAFT" {
		return store.GetQuotationRow{}, nil, apierr.Conflict("EX_NOT_DRAFT", "只有草稿可以修改，已发送的报价请新建一版")
	}
	customer, lines, total, err := s.resolve(ctx, in)
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	contact, err := pickContact(customer, in.ContactID)
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
			ContactID: contact.ID, ContactName: contact.Name, ContactEmail: contact.Email,
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

func translateUnique(err error, code, msg string) error {
	if isUniqueViolation(err) {
		return apierr.Conflict(code, msg)
	}
	return err
}

// GetQuotationFor is GetQuotation with the caller's data scope applied; the
// unscoped one stays for internal reads such as generating a contract.
func (s *Service) GetQuotationFor(ctx context.Context, tenantID, id int64, op Operator) (store.GetQuotationRow, []store.ListQuotationItemsRow, error) {
	q, items, err := s.GetQuotation(ctx, tenantID, id)
	if err != nil {
		return q, items, err
	}
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return store.GetQuotationRow{}, nil, err
	}
	if !allowedOwner(visible, q.SalesEmployeeID) {
		// "Not found" rather than "forbidden": confirming a document exists
		// is itself information.
		return store.GetQuotationRow{}, nil, apierr.NotFound("EX_QUOTE_NOT_FOUND", "报价单不存在")
	}
	return q, items, nil
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

// QuotationFilter is the set of narrowing options a quotation list accepts.
type QuotationFilter struct {
	Keyword    string
	CustomerID int64
	Status     string
	// WithoutContract hides offers that have already been written up.
	WithoutContract bool
}

func (s *Service) ListQuotations(ctx context.Context, tenantID int64, f QuotationFilter, page, size int32, op Operator) ([]store.ListQuotationsRow, int64, error) {
	page, size = normalizePage(page, size)
	visible, err := s.visibleTo(ctx, op)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListQuotations(ctx, store.ListQuotationsParams{
		TenantID: tenantID, Keyword: f.Keyword, CustomerID: f.CustomerID, Status: f.Status,
		WithoutContract: f.WithoutContract,
		VisibleAll:      visible.All, VisibleIds: visible.EmployeeIDs,
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
