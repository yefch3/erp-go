// Package app holds the masterdata use cases: customers, suppliers,
// option dictionaries and document numbering.
package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

// ---------------------------------------------------------------- customers

type ContactInput struct {
	Name, Title, Email, Phone string
	IsPrimary                 bool
}

type CustomerInput struct {
	Code, Name, Country, Address, Currency, PaymentTerm, Remark string
	Contacts                                                    []ContactInput
	OperatorID                                                  int64
}

func (in CustomerInput) validate(requireCode bool) error {
	if requireCode && in.Code == "" {
		return apierr.Invalid("MD_CUSTOMER_CODE_REQUIRED", "客户编码必填")
	}
	if in.Name == "" {
		return apierr.Invalid("MD_CUSTOMER_NAME_REQUIRED", "客户名称必填")
	}
	if in.Currency != "" && len(in.Currency) != 3 {
		return apierr.Invalid("MD_CURRENCY_INVALID", "币种必须是 3 位 ISO 代码")
	}
	for _, c := range in.Contacts {
		if c.Name == "" {
			return apierr.Invalid("MD_CONTACT_NAME_REQUIRED", "联系人姓名必填")
		}
	}
	return nil
}

func (s *Service) CreateCustomer(ctx context.Context, tenantID int64, in CustomerInput) (store.Customer, []store.CustomerContact, error) {
	if err := in.validate(true); err != nil {
		return store.Customer{}, nil, err
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	var out store.Customer
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, err := q.CreateCustomer(ctx, store.CreateCustomerParams{
			TenantID: tenantID, Code: in.Code, Name: in.Name, Country: in.Country,
			Address: in.Address, Currency: in.Currency, PaymentTerm: in.PaymentTerm,
			Remark: in.Remark, CreatedBy: in.OperatorID,
		})
		if err != nil {
			return translateUnique(err, "MD_CUSTOMER_CODE_TAKEN", "客户编码已存在")
		}
		out = c
		return replaceContacts(ctx, q, tenantID, c.ID, in.Contacts)
	})
	if err != nil {
		return store.Customer{}, nil, err
	}
	contacts, err := s.q.ListCustomerContacts(ctx, store.ListCustomerContactsParams{TenantID: tenantID, CustomerID: out.ID})
	return out, contacts, err
}

func (s *Service) GetCustomer(ctx context.Context, tenantID, id int64) (store.Customer, []store.CustomerContact, error) {
	c, err := s.q.GetCustomer(ctx, store.GetCustomerParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Customer{}, nil, apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
		}
		return store.Customer{}, nil, err
	}
	contacts, err := s.q.ListCustomerContacts(ctx, store.ListCustomerContactsParams{TenantID: tenantID, CustomerID: id})
	return c, contacts, err
}

func (s *Service) ListCustomers(ctx context.Context, tenantID int64, keyword, status string, page, size int32) ([]store.ListCustomersRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListCustomers(ctx, store.ListCustomersParams{
		TenantID: tenantID, Status: status, Keyword: keyword,
		Limit: size, Offset: (page - 1) * size,
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

func (s *Service) UpdateCustomer(ctx context.Context, tenantID, id int64, in CustomerInput) (store.Customer, []store.CustomerContact, error) {
	if err := in.validate(false); err != nil {
		return store.Customer{}, nil, err
	}
	var out store.Customer
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		c, err := q.UpdateCustomer(ctx, store.UpdateCustomerParams{
			TenantID: tenantID, ID: id, Name: in.Name, Country: in.Country,
			Address: in.Address, Currency: in.Currency, PaymentTerm: in.PaymentTerm,
			Remark: in.Remark, UpdatedBy: in.OperatorID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
			}
			return err
		}
		out = c
		return replaceContacts(ctx, q, tenantID, id, in.Contacts)
	})
	if err != nil {
		return store.Customer{}, nil, err
	}
	contacts, err := s.q.ListCustomerContacts(ctx, store.ListCustomerContactsParams{TenantID: tenantID, CustomerID: id})
	return out, contacts, err
}

func (s *Service) DeactivateCustomer(ctx context.Context, tenantID, id, operatorID int64) error {
	n, err := s.q.DeactivateCustomer(ctx, store.DeactivateCustomerParams{TenantID: tenantID, ID: id, UpdatedBy: operatorID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在或已停用")
	}
	return nil
}

func replaceContacts(ctx context.Context, q *store.Queries, tenantID, customerID int64, contacts []ContactInput) error {
	if err := q.DeleteCustomerContacts(ctx, store.DeleteCustomerContactsParams{TenantID: tenantID, CustomerID: customerID}); err != nil {
		return err
	}
	for i, c := range contacts {
		if err := q.AddCustomerContact(ctx, store.AddCustomerContactParams{
			TenantID: tenantID, CustomerID: customerID, Name: c.Name, Title: c.Title,
			Email: c.Email, Phone: c.Phone, IsPrimary: c.IsPrimary, SortOrder: int32(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- suppliers

type SupplierInput struct {
	Code, Name, Country, Address, Currency          string
	ContactName, ContactPhone, ContactEmail, Remark string
	OperatorID                                      int64
}

func (s *Service) CreateSupplier(ctx context.Context, tenantID int64, in SupplierInput) (store.Supplier, error) {
	if in.Code == "" || in.Name == "" {
		return store.Supplier{}, apierr.Invalid("MD_SUPPLIER_FIELDS_REQUIRED", "供应商编码和名称必填")
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	sp, err := s.q.CreateSupplier(ctx, store.CreateSupplierParams{
		TenantID: tenantID, Code: in.Code, Name: in.Name, Country: in.Country,
		Address: in.Address, Currency: in.Currency, ContactName: in.ContactName,
		ContactPhone: in.ContactPhone, ContactEmail: in.ContactEmail,
		Remark: in.Remark, CreatedBy: in.OperatorID,
	})
	if err != nil {
		return store.Supplier{}, translateUnique(err, "MD_SUPPLIER_CODE_TAKEN", "供应商编码已存在")
	}
	return sp, nil
}

func (s *Service) GetSupplier(ctx context.Context, tenantID, id int64) (store.Supplier, error) {
	sp, err := s.q.GetSupplier(ctx, store.GetSupplierParams{TenantID: tenantID, ID: id})
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return store.Supplier{}, apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在")
	}
	return sp, err
}

func (s *Service) ListSuppliers(ctx context.Context, tenantID int64, keyword, status string, page, size int32) ([]store.ListSuppliersRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListSuppliers(ctx, store.ListSuppliersParams{
		TenantID: tenantID, Status: status, Keyword: keyword,
		Limit: size, Offset: (page - 1) * size,
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

func (s *Service) UpdateSupplier(ctx context.Context, tenantID, id int64, in SupplierInput) (store.Supplier, error) {
	if in.Name == "" {
		return store.Supplier{}, apierr.Invalid("MD_SUPPLIER_FIELDS_REQUIRED", "供应商名称必填")
	}
	sp, err := s.q.UpdateSupplier(ctx, store.UpdateSupplierParams{
		TenantID: tenantID, ID: id, Name: in.Name, Country: in.Country,
		Address: in.Address, Currency: in.Currency, ContactName: in.ContactName,
		ContactPhone: in.ContactPhone, ContactEmail: in.ContactEmail,
		Remark: in.Remark, UpdatedBy: in.OperatorID,
	})
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return store.Supplier{}, apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在")
	}
	return sp, err
}

func (s *Service) DeactivateSupplier(ctx context.Context, tenantID, id, operatorID int64) error {
	n, err := s.q.DeactivateSupplier(ctx, store.DeactivateSupplierParams{TenantID: tenantID, ID: id, UpdatedBy: operatorID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在或已停用")
	}
	return nil
}

// ---------------------------------------------------------------- options

func (s *Service) ListOptions(ctx context.Context, tenantID int64, category string) ([]store.OptionItem, error) {
	return s.q.ListOptions(ctx, store.ListOptionsParams{TenantID: tenantID, Category: category})
}

func (s *Service) CreateOption(ctx context.Context, tenantID int64, category, code, label string, sortOrder int32) (store.OptionItem, error) {
	if category == "" || code == "" || label == "" {
		return store.OptionItem{}, apierr.Invalid("MD_OPTION_FIELDS_REQUIRED", "选项类别、编码、名称必填")
	}
	o, err := s.q.CreateOption(ctx, store.CreateOptionParams{
		TenantID: tenantID, Category: category, Code: code, Label: label, SortOrder: sortOrder,
	})
	if err != nil {
		return store.OptionItem{}, translateUnique(err, "MD_OPTION_CODE_TAKEN", "该类别下选项编码已存在")
	}
	return o, nil
}

// ---------------------------------------------------------------- shared

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

func translateUnique(err error, code, msg string) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return apierr.Conflict(code, msg)
	}
	return err
}

func (s *Service) ActivateCustomer(ctx context.Context, tenantID, id, operatorID int64) error {
	n, err := s.q.ActivateCustomer(ctx, store.ActivateCustomerParams{TenantID: tenantID, ID: id, UpdatedBy: operatorID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在或已是启用状态")
	}
	return nil
}

func (s *Service) ActivateSupplier(ctx context.Context, tenantID, id, operatorID int64) error {
	n, err := s.q.ActivateSupplier(ctx, store.ActivateSupplierParams{TenantID: tenantID, ID: id, UpdatedBy: operatorID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("MD_SUPPLIER_NOT_FOUND", "供应商不存在或已是启用状态")
	}
	return nil
}
