// Package app holds the masterdata use cases: customers, suppliers,
// option dictionaries and document numbering.
package app

import (
	"context"
	"errors"
	"strings"

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
	ShortName, EnglishName, CustomerType, Industry, Source      string
	Website, PrimaryLanguage, Timezone                          string
	RegisteredName, RegistrationNo, TaxID                       string
	InvoiceTitle, InvoiceTaxNo, InvoiceRemark                   string
	CreditCurrency, CreditStatus, BusinessStatus                string
	Tags                                                        []string
	PaymentDays                                                 int32
	CreditLimitMinor                                            int64
	// CountryCode is ISO 3166-1 alpha-2, and it is what the system groups by.
	// Country is the free text that came before it and is on its way out —
	// kept only so a row nobody has re-saved still shows something.
	CountryCode  string
	Contacts     []ContactInput
	OperatorID   int64
	OperatorName string
}

// CustomerProfileInput 只承载客户详细资料。它与基础信息分开，保证旧页面更新
// 名称、国家等字段时，不会把未来新增的详细资料覆盖为空值。
type CustomerProfileInput struct {
	ShortName, EnglishName, CustomerType, Industry, Source string
	Website, PrimaryLanguage, Timezone                     string
	RegisteredName, RegistrationNo, TaxID                  string
	InvoiceTitle, InvoiceTaxNo, InvoiceRemark              string
	CreditCurrency, CreditStatus, BusinessStatus           string
	Tags                                                   []string
	PaymentDays                                            int32
	CreditLimitMinor                                       int64
	OperatorID                                             int64
	OperatorName                                           string
}

// normaliseCountry accepts what a caller sends and returns what the column
// will hold.
//
// Upper-cased and length-checked here rather than trusted, because the check
// constraint in the database would turn a lower-case "br" into a 500 rather
// than into "BR" — and a two-letter code arriving in the wrong case is a
// mistake the system can simply fix.
func normaliseCountry(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "", nil
	}
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return "", apierr.Invalid("MD_COUNTRY_CODE_INVALID",
			"国家代码必须是两位字母的 ISO 代码，例如 BR、US、CN")
	}
	return code, nil
}

func (in CustomerInput) validate() error {
	if in.Name == "" {
		return apierr.Invalid("MD_CUSTOMER_NAME_REQUIRED", "客户名称必填")
	}
	if in.Currency != "" && len(in.Currency) != 3 {
		return apierr.Invalid("MD_CURRENCY_INVALID", "币种必须是 3 位 ISO 代码")
	}
	if in.PaymentDays < 0 {
		return apierr.Invalid("MD_PAYMENT_DAYS_INVALID", "付款账期不能小于 0 天")
	}
	if in.CreditLimitMinor < 0 {
		return apierr.Invalid("MD_CREDIT_LIMIT_INVALID", "信用额度不能小于 0")
	}
	if in.CreditCurrency != "" && len(in.CreditCurrency) != 3 {
		return apierr.Invalid("MD_CREDIT_CURRENCY_INVALID", "信用额度币种必须是 3 位 ISO 代码")
	}
	if in.CreditStatus != "" && !map[string]bool{"NORMAL": true, "WATCH": true, "CREDIT_SUSPENDED": true}[in.CreditStatus] {
		return apierr.Invalid("MD_CREDIT_STATUS_INVALID", "信用状态无效")
	}
	if in.BusinessStatus != "" && !map[string]bool{"PROSPECT": true, "COOPERATING": true, "PAUSED": true, "INACTIVE": true}[in.BusinessStatus] {
		return apierr.Invalid("MD_CUSTOMER_BUSINESS_STATUS_INVALID", "客户业务状态无效")
	}
	for _, c := range in.Contacts {
		if c.Name == "" {
			return apierr.Invalid("MD_CONTACT_NAME_REQUIRED", "联系人姓名必填")
		}
	}
	return nil
}

func (in CustomerProfileInput) validate() error {
	if in.PaymentDays < 0 {
		return apierr.Invalid("MD_PAYMENT_DAYS_INVALID", "付款账期不能小于 0 天")
	}
	if in.CreditLimitMinor < 0 {
		return apierr.Invalid("MD_CREDIT_LIMIT_INVALID", "信用额度不能小于 0")
	}
	if in.CreditCurrency != "" && len(in.CreditCurrency) != 3 {
		return apierr.Invalid("MD_CREDIT_CURRENCY_INVALID", "信用额度币种必须是 3 位 ISO 代码")
	}
	if !map[string]bool{"NORMAL": true, "WATCH": true, "CREDIT_SUSPENDED": true}[in.CreditStatus] {
		return apierr.Invalid("MD_CREDIT_STATUS_INVALID", "信用状态无效")
	}
	if !map[string]bool{"PROSPECT": true, "COOPERATING": true, "PAUSED": true, "INACTIVE": true}[in.BusinessStatus] {
		return apierr.Invalid("MD_CUSTOMER_BUSINESS_STATUS_INVALID", "客户业务状态无效")
	}
	return nil
}

// normalizeProfile 为旧客户端未传的新字段补默认值，并清理标签。
// 默认值放在服务层而不是只依赖数据库，确保 Create 和 Update 行为一致。
func (in *CustomerInput) normalizeProfile() {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "USD"
	}
	in.CreditCurrency = strings.ToUpper(strings.TrimSpace(in.CreditCurrency))
	if in.CreditCurrency == "" {
		in.CreditCurrency = in.Currency
	}
	in.CreditStatus = strings.ToUpper(strings.TrimSpace(in.CreditStatus))
	if in.CreditStatus == "" {
		in.CreditStatus = "NORMAL"
	}
	in.BusinessStatus = strings.ToUpper(strings.TrimSpace(in.BusinessStatus))
	if in.BusinessStatus == "" {
		in.BusinessStatus = "PROSPECT"
	}
	in.CustomerType = strings.ToUpper(strings.TrimSpace(in.CustomerType))
	in.Source = strings.ToUpper(strings.TrimSpace(in.Source))
	seen := make(map[string]bool, len(in.Tags))
	tags := make([]string, 0, len(in.Tags))
	for _, raw := range in.Tags {
		tag := strings.TrimSpace(raw)
		if tag != "" && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	in.Tags = tags
}

func (in *CustomerProfileInput) normalize(defaultCurrency string) {
	in.CreditCurrency = strings.ToUpper(strings.TrimSpace(in.CreditCurrency))
	if in.CreditCurrency == "" {
		in.CreditCurrency = strings.ToUpper(strings.TrimSpace(defaultCurrency))
	}
	if in.CreditCurrency == "" {
		in.CreditCurrency = "USD"
	}
	in.CreditStatus = strings.ToUpper(strings.TrimSpace(in.CreditStatus))
	if in.CreditStatus == "" {
		in.CreditStatus = "NORMAL"
	}
	in.BusinessStatus = strings.ToUpper(strings.TrimSpace(in.BusinessStatus))
	if in.BusinessStatus == "" {
		in.BusinessStatus = "PROSPECT"
	}
	in.CustomerType = strings.ToUpper(strings.TrimSpace(in.CustomerType))
	in.Source = strings.ToUpper(strings.TrimSpace(in.Source))
	seen := make(map[string]bool, len(in.Tags))
	tags := make([]string, 0, len(in.Tags))
	for _, raw := range in.Tags {
		tag := strings.TrimSpace(raw)
		if tag != "" && !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	in.Tags = tags
}

func (s *Service) CreateCustomer(ctx context.Context, tenantID int64, in CustomerInput) (store.Customer, []store.CustomerContact, error) {
	in.normalizeProfile()
	if err := in.validate(); err != nil {
		return store.Customer{}, nil, err
	}
	var out store.Customer
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		// An empty code means "let the system issue one". Drawing it here,
		// inside the insert's transaction, is what keeps the sequence tight:
		// abandoning the form costs nothing because no call was made, and a
		// rejected insert rolls the sequence back with it.
		code := in.Code
		if code == "" {
			var err error
			if code, err = s.nextNumber(ctx, q, tenantID, "CUSTOMER"); err != nil {
				return err
			}
		}
		cc, err := normaliseCountry(in.CountryCode)
		if err != nil {
			return err
		}
		c, err := q.CreateCustomer(ctx, store.CreateCustomerParams{
			TenantID: tenantID, Code: code, Name: in.Name, Country: in.Country,
			CountryCode: cc,
			Address:     in.Address, Currency: in.Currency, PaymentTerm: in.PaymentTerm,
			Remark: in.Remark, ShortName: in.ShortName, EnglishName: in.EnglishName,
			CustomerType: in.CustomerType, Industry: in.Industry, Source: in.Source,
			Tags: in.Tags, Website: in.Website, PrimaryLanguage: in.PrimaryLanguage,
			Timezone: in.Timezone, RegisteredName: in.RegisteredName,
			RegistrationNo: in.RegistrationNo, TaxID: in.TaxID,
			InvoiceTitle: in.InvoiceTitle, InvoiceTaxNo: in.InvoiceTaxNo,
			InvoiceRemark: in.InvoiceRemark, PaymentDays: in.PaymentDays,
			CreditLimitMinor: in.CreditLimitMinor, CreditCurrency: in.CreditCurrency,
			CreditStatus: in.CreditStatus, BusinessStatus: in.BusinessStatus,
			OperatorID: in.OperatorID,
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

func (s *Service) ListCustomers(ctx context.Context, tenantID int64, keyword, status, countryCode, customerType, businessStatus, tag string, ownerEmployeeID int64, page, size int32) ([]store.ListCustomersRow, int64, error) {
	page, size = normalizePage(page, size)
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode != "" && countryCode != "__UNCLASSIFIED__" {
		var err error
		countryCode, err = normaliseCountry(countryCode)
		if err != nil {
			return nil, 0, err
		}
	}
	rows, err := s.q.ListCustomers(ctx, store.ListCustomersParams{
		TenantID: tenantID, Status: status, CountryCode: countryCode, Keyword: keyword,
		CustomerType:   strings.ToUpper(strings.TrimSpace(customerType)),
		BusinessStatus: strings.ToUpper(strings.TrimSpace(businessStatus)), Tag: strings.TrimSpace(tag),
		OwnerEmployeeID: ownerEmployeeID,
		LimitCount:      size, OffsetCount: (page - 1) * size,
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
	if err := in.validate(); err != nil {
		return store.Customer{}, nil, err
	}
	var out store.Customer
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cc, err := normaliseCountry(in.CountryCode)
		if err != nil {
			return err
		}
		c, err := q.UpdateCustomer(ctx, store.UpdateCustomerParams{
			TenantID: tenantID, ID: id, Name: in.Name, Country: in.Country,
			CountryCode: cc,
			Address:     in.Address, Currency: in.Currency, PaymentTerm: in.PaymentTerm,
			Remark: in.Remark, OperatorID: in.OperatorID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
			}
			return err
		}
		out = c
		return recordCustomerChange(ctx, q, tenantID, id, "UPDATE", "BASIC", "更新客户基本资料", nil, c, in.OperatorID, in.OperatorName)
	})
	if err != nil {
		return store.Customer{}, nil, err
	}
	contacts, err := s.q.ListCustomerContacts(ctx, store.ListCustomerContactsParams{TenantID: tenantID, CustomerID: id})
	return out, contacts, err
}

// UpdateCustomerProfile 只更新详细资料，基础信息和联系人保持不变。
func (s *Service) UpdateCustomerProfile(ctx context.Context, tenantID, id int64, in CustomerProfileInput) (store.Customer, error) {
	current, err := s.q.GetCustomer(ctx, store.GetCustomerParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Customer{}, apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
		}
		return store.Customer{}, err
	}
	in.normalize(current.Currency)
	if err := in.validate(); err != nil {
		return store.Customer{}, err
	}
	// 合作中客户必须已经完成国家归类，并且至少分配一位在职负责人。
	// 这能避免客户进入正式业务流程后仍无人跟进或无法按国家统计。
	if in.BusinessStatus == "COOPERATING" {
		if strings.TrimSpace(current.CountryCode) == "" {
			return store.Customer{}, apierr.Invalid("MD_CUSTOMER_COUNTRY_REQUIRED", "合作中客户必须设置国家")
		}
		ownerCount, err := s.q.CountActiveCustomerOwners(ctx, store.CountActiveCustomerOwnersParams{
			TenantID: tenantID, CustomerID: id,
		})
		if err != nil {
			return store.Customer{}, err
		}
		if ownerCount == 0 {
			return store.Customer{}, apierr.Invalid("MD_CUSTOMER_OWNER_REQUIRED", "合作中客户至少需要一位负责人")
		}
	}
	out, err := s.q.UpdateCustomerProfile(ctx, store.UpdateCustomerProfileParams{
		TenantID: tenantID, ID: id, ShortName: in.ShortName, EnglishName: in.EnglishName,
		CustomerType: in.CustomerType, Industry: in.Industry, Source: in.Source,
		Tags: in.Tags, Website: in.Website, PrimaryLanguage: in.PrimaryLanguage,
		Timezone: in.Timezone, RegisteredName: in.RegisteredName,
		RegistrationNo: in.RegistrationNo, TaxID: in.TaxID,
		InvoiceTitle: in.InvoiceTitle, InvoiceTaxNo: in.InvoiceTaxNo,
		InvoiceRemark: in.InvoiceRemark, PaymentDays: in.PaymentDays,
		CreditLimitMinor: in.CreditLimitMinor, CreditCurrency: in.CreditCurrency,
		CreditStatus: in.CreditStatus, BusinessStatus: in.BusinessStatus,
		OperatorID: in.OperatorID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Customer{}, apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在")
		}
		return store.Customer{}, err
	}
	if err := recordCustomerChange(ctx, s.q, tenantID, id, "UPDATE", "PROFILE", "更新客户详细资料", current, out, in.OperatorID, in.OperatorName); err != nil {
		return store.Customer{}, err
	}
	return out, nil
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
	if in.Name == "" {
		return store.Supplier{}, apierr.Invalid("MD_SUPPLIER_FIELDS_REQUIRED", "供应商名称必填")
	}
	if in.Currency == "" {
		in.Currency = "CNY"
	}
	var out store.Supplier
	// Same deal as customers: an empty code is issued here so an abandoned
	// form never burns a number. See CreateCustomer.
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		code := in.Code
		if code == "" {
			var err error
			if code, err = s.nextNumber(ctx, q, tenantID, "SUPPLIER"); err != nil {
				return err
			}
		}
		sp, err := q.CreateSupplier(ctx, store.CreateSupplierParams{
			TenantID: tenantID, Code: code, Name: in.Name, Country: in.Country,
			Address: in.Address, Currency: in.Currency, ContactName: in.ContactName,
			ContactPhone: in.ContactPhone, ContactEmail: in.ContactEmail,
			Remark: in.Remark, CreatedBy: in.OperatorID,
		})
		if err != nil {
			return translateUnique(err, "MD_SUPPLIER_CODE_TAKEN", "供应商编码已存在")
		}
		out = sp
		return nil
	})
	return out, err
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

// ListMailingContacts is the address book the mail composer picks from.
// Unpaginated on purpose: the composer needs to search and multi-select
// across the whole book, and the query caps itself at 500 rows.
func (s *Service) ListMailingContacts(ctx context.Context, tenantID int64, keyword string, customerIDs []int64) ([]store.ListMailingContactsRow, error) {
	if customerIDs == nil {
		customerIDs = []int64{}
	}
	return s.q.ListMailingContacts(ctx, store.ListMailingContactsParams{
		TenantID: tenantID, Keyword: keyword, CustomerIds: customerIDs,
	})
}

// CountryGroup is one country and how big a send to it would be.
type CountryGroup struct {
	Code          string
	CustomerCount int64
	ContactCount  int64
	OneEachCount  int64
}

// ListCustomerCountries answers "which countries do we sell to, and how many
// people are in each" for the recipient picker.
//
// The name of the country is not here and should not be. It is presentation,
// it differs per reader, and every browser already ships the translations —
// so the code travels and the screen decides what to call it.
func (s *Service) ListCustomerCountries(ctx context.Context, tenantID int64, status string) ([]CountryGroup, error) {
	rows, err := s.q.ListCustomerCountries(ctx, store.ListCustomerCountriesParams{TenantID: tenantID, Status: status})
	if err != nil {
		return nil, err
	}
	out := make([]CountryGroup, len(rows))
	for i, r := range rows {
		out[i] = CountryGroup{
			Code: strings.TrimSpace(r.CountryCode), CustomerCount: r.CustomerCount,
			ContactCount: r.ContactCount, OneEachCount: r.OneEachCount,
		}
	}
	return out, nil
}

// CountryRecipient is one addressable person, the same shape the address book
// already returns so the picker can treat both sources identically.
type CountryRecipient struct {
	ContactID    int64
	Name         string
	Title        string
	Email        string
	IsPrimary    bool
	CustomerID   int64
	CustomerName string
	Country      string
	CountryCode  string
}

// ContactsInCountry returns everybody writable in one country.
//
// primaryOnly picks one person per customer instead of everybody at it, and
// the caller has to say which because both are real intentions: a price
// update goes to the buyer, an invitation to a trade fair goes to whoever
// might come. Guessing here would silently multiply or divide the size of
// somebody's send.
func (s *Service) ContactsInCountry(ctx context.Context, tenantID int64, code string, primaryOnly bool) ([]CountryRecipient, error) {
	// Normalised the same way it is on the way in, so "br" from a URL finds
	// the rows stored as "BR" rather than quietly returning nothing.
	code, err := normaliseCountry(code)
	if err != nil {
		return nil, err
	}
	if primaryOnly {
		rows, err := s.q.ContactsInCountry(ctx, store.ContactsInCountryParams{
			TenantID: tenantID, CountryCode: code,
		})
		if err != nil {
			return nil, err
		}
		out := make([]CountryRecipient, len(rows))
		for i, r := range rows {
			out[i] = CountryRecipient{
				ContactID: r.ContactID, Name: r.Name, Title: r.Title, Email: r.Email,
				IsPrimary: r.IsPrimary, CustomerID: r.CustomerID,
				CustomerName: r.CustomerName, Country: r.Country,
				CountryCode: strings.TrimSpace(r.CountryCode),
			}
		}
		return out, nil
	}
	rows, err := s.q.AllContactsInCountry(ctx, store.AllContactsInCountryParams{
		TenantID: tenantID, CountryCode: code,
	})
	if err != nil {
		return nil, err
	}
	out := make([]CountryRecipient, len(rows))
	for i, r := range rows {
		out[i] = CountryRecipient{
			ContactID: r.ContactID, Name: r.Name, Title: r.Title, Email: r.Email,
			IsPrimary: r.IsPrimary, CustomerID: r.CustomerID,
			CustomerName: r.CustomerName, Country: r.Country,
			CountryCode: strings.TrimSpace(r.CountryCode),
		}
	}
	return out, nil
}
