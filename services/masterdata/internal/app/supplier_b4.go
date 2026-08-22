package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type PartyContactInput struct {
	Name, Department, Title, Phone, Email, Remark string
	IsPrimary                                     bool
	OperatorID                                    int64
	OperatorName                                  string
}

type PartyOwnerInput struct {
	EmployeeID                                           int64
	EmployeeName, ResponsibilityCode, StartDate, EndDate string
	IsPrimary                                            bool
	OperatorID                                           int64
	OperatorName                                         string
}

type FactoryInput struct {
	SupplierID                                             int64
	Code, NameZh, NameEn, ShortName, CountryCode, Timezone string
	StateProvince, City, District, PostalCode, Address     string
	Status, Remark, TransferReason                         string
	OperatorID                                             int64
	OperatorName                                           string
}

type FactoryCapabilityInput struct {
	ProductCategory, Process, MonthlyCapacity, CapacityUnit, MOQ string
	LeadTimeDays                                                 int32
	PeriodLabel, ConfirmedOn, Remark                             string
	OperatorID                                                   int64
	OperatorName                                                 string
}

type FactoryCertificateInput struct {
	Name, CertificateNo, IssuedOn, ExpiresOn, Status, FileKey, Remark string
	OperatorID                                                        int64
	OperatorName                                                      string
}

func recordSupplierChange(ctx context.Context, q *store.Queries, tenantID, supplierID int64, action, section, summary string, before, after any, operatorID int64, operatorName string) error {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	return q.CreateSupplierChange(ctx, store.CreateSupplierChangeParams{TenantID: tenantID, SupplierID: supplierID,
		Action: action, Section: section, Summary: summary, BeforeData: beforeJSON, AfterData: afterJSON,
		OperatorID: operatorID, OperatorName: operatorName})
}

func recordFactoryChange(ctx context.Context, q *store.Queries, tenantID, factoryID int64, action, section, summary string, before, after any, operatorID int64, operatorName string) error {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	return q.CreateFactoryChange(ctx, store.CreateFactoryChangeParams{TenantID: tenantID, FactoryID: factoryID,
		Action: action, Section: section, Summary: summary, BeforeData: beforeJSON, AfterData: afterJSON,
		OperatorID: operatorID, OperatorName: operatorName})
}

func normalizePartyContact(in *PartyContactInput) error {
	in.Name, in.Email, in.Phone = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), strings.TrimSpace(in.Phone)
	if in.Name == "" {
		return apierr.Invalid("MD_CONTACT_NAME_REQUIRED", "联系人姓名必填")
	}
	if in.Email != "" {
		if _, err := mail.ParseAddress(in.Email); err != nil {
			return apierr.Invalid("MD_CONTACT_EMAIL_INVALID", "联系人邮箱格式不正确")
		}
	}
	if in.Phone != "" && !customerPhonePattern.MatchString(in.Phone) {
		return apierr.Invalid("MD_CONTACT_PHONE_INVALID", "联系人电话格式不正确")
	}
	return nil
}

func normalizePartyOwner(in *PartyOwnerInput) (pgtype.Date, pgtype.Date, error) {
	in.EmployeeName = strings.TrimSpace(in.EmployeeName)
	in.ResponsibilityCode = strings.ToUpper(strings.TrimSpace(in.ResponsibilityCode))
	if in.EmployeeID == 0 || in.EmployeeName == "" || in.ResponsibilityCode == "" {
		return pgtype.Date{}, pgtype.Date{}, apierr.Invalid("MD_OWNER_REQUIRED", "负责人、姓名和职责必填")
	}
	start, err := parseOptionalDate(in.StartDate, "开始日期")
	if err != nil {
		return pgtype.Date{}, pgtype.Date{}, err
	}
	end, err := parseOptionalDate(in.EndDate, "结束日期")
	if err != nil {
		return pgtype.Date{}, pgtype.Date{}, err
	}
	if start.Valid && end.Valid && end.Time.Before(start.Time) {
		return pgtype.Date{}, pgtype.Date{}, apierr.Invalid("MD_OWNER_DATES_INVALID", "结束日期不能早于开始日期")
	}
	return start, end, nil
}

func (s *Service) ListSupplierCountries(ctx context.Context, tenantID int64, includeInactive bool) ([]store.ListSupplierCountriesRow, error) {
	return s.q.ListSupplierCountries(ctx, store.ListSupplierCountriesParams{TenantID: tenantID, IncludeInactive: includeInactive})
}

func (s *Service) ListSupplierContacts(ctx context.Context, tenantID, supplierID int64, includeInactive bool) ([]store.SupplierContact, error) {
	if _, err := s.GetSupplier(ctx, tenantID, supplierID); err != nil {
		return nil, err
	}
	return s.q.ListSupplierContacts(ctx, store.ListSupplierContactsParams{TenantID: tenantID, SupplierID: supplierID, IncludeInactive: includeInactive})
}

func (s *Service) CreateSupplierContact(ctx context.Context, tenantID, supplierID int64, in PartyContactInput) (store.SupplierContact, error) {
	if err := normalizePartyContact(&in); err != nil {
		return store.SupplierContact{}, err
	}
	var out store.SupplierContact
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.GetSupplier(ctx, store.GetSupplierParams{TenantID: tenantID, ID: supplierID}); err != nil {
			return err
		}
		if in.IsPrimary {
			if err := q.ClearSupplierPrimaryContact(ctx, store.ClearSupplierPrimaryContactParams{TenantID: tenantID, SupplierID: supplierID, OperatorID: in.OperatorID, ExceptID: 0}); err != nil {
				return err
			}
		}
		var err error
		out, err = q.CreateSupplierContact(ctx, store.CreateSupplierContactParams{TenantID: tenantID, SupplierID: supplierID, Name: in.Name, Department: in.Department, Title: in.Title, Phone: in.Phone, Email: in.Email, IsPrimary: in.IsPrimary, Remark: in.Remark, OperatorID: in.OperatorID})
		if err != nil {
			return err
		}
		return recordSupplierChange(ctx, q, tenantID, supplierID, "CREATE", "CONTACT", "新增联系人："+out.Name, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}

func (s *Service) UpdateSupplierContact(ctx context.Context, tenantID, supplierID, id int64, in PartyContactInput) (store.SupplierContact, error) {
	if err := normalizePartyContact(&in); err != nil {
		return store.SupplierContact{}, err
	}
	var out store.SupplierContact
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsPrimary {
			if err := q.ClearSupplierPrimaryContact(ctx, store.ClearSupplierPrimaryContactParams{TenantID: tenantID, SupplierID: supplierID, OperatorID: in.OperatorID, ExceptID: id}); err != nil {
				return err
			}
		}
		var err error
		out, err = q.UpdateSupplierContact(ctx, store.UpdateSupplierContactParams{TenantID: tenantID, SupplierID: supplierID, ID: id, Name: in.Name, Department: in.Department, Title: in.Title, Phone: in.Phone, Email: in.Email, IsPrimary: in.IsPrimary, Remark: in.Remark, OperatorID: in.OperatorID})
		if err != nil {
			return err
		}
		return recordSupplierChange(ctx, q, tenantID, supplierID, "UPDATE", "CONTACT", "更新联系人："+out.Name, nil, out, in.OperatorID, in.OperatorName)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.SupplierContact{}, apierr.NotFound("MD_CONTACT_NOT_FOUND", "联系人不存在")
	}
	return out, err
}

func (s *Service) DeactivateSupplierContact(ctx context.Context, tenantID, supplierID, id, operatorID int64, operatorName string) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.DeactivateSupplierContact(ctx, store.DeactivateSupplierContactParams{TenantID: tenantID, SupplierID: supplierID, ID: id, OperatorID: operatorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("MD_CONTACT_NOT_FOUND", "联系人不存在或已停用")
		}
		return recordSupplierChange(ctx, q, tenantID, supplierID, "DEACTIVATE", "CONTACT", "停用供应商联系人", nil, nil, operatorID, operatorName)
	})
}

func (s *Service) ListSupplierOwners(ctx context.Context, tenantID, supplierID int64, includeInactive bool) ([]store.SupplierOwner, error) {
	return s.q.ListSupplierOwners(ctx, store.ListSupplierOwnersParams{TenantID: tenantID, SupplierID: supplierID, IncludeInactive: includeInactive})
}
func (s *Service) CreateSupplierOwner(ctx context.Context, tenantID, supplierID int64, in PartyOwnerInput) (store.SupplierOwner, error) {
	start, end, err := normalizePartyOwner(&in)
	if err != nil {
		return store.SupplierOwner{}, err
	}
	var out store.SupplierOwner
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsPrimary {
			if err := q.ClearSupplierPrimaryOwner(ctx, store.ClearSupplierPrimaryOwnerParams{TenantID: tenantID, SupplierID: supplierID, OperatorID: in.OperatorID, ExceptID: 0}); err != nil {
				return err
			}
		}
		out, err = q.CreateSupplierOwner(ctx, store.CreateSupplierOwnerParams{TenantID: tenantID, SupplierID: supplierID, EmployeeID: in.EmployeeID, EmployeeName: in.EmployeeName, ResponsibilityCode: in.ResponsibilityCode, IsPrimary: in.IsPrimary, StartDate: start, EndDate: end, OperatorID: in.OperatorID})
		if err != nil {
			return translateUnique(err, "MD_SUPPLIER_OWNER_DUPLICATE", "该员工已以相同职责负责此供应商")
		}
		return recordSupplierChange(ctx, q, tenantID, supplierID, "CREATE", "OWNER", "新增负责人："+out.EmployeeName, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}
func (s *Service) UpdateSupplierOwner(ctx context.Context, tenantID, supplierID, id int64, in PartyOwnerInput) (store.SupplierOwner, error) {
	start, end, err := normalizePartyOwner(&in)
	if err != nil {
		return store.SupplierOwner{}, err
	}
	var out store.SupplierOwner
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsPrimary {
			if err := q.ClearSupplierPrimaryOwner(ctx, store.ClearSupplierPrimaryOwnerParams{TenantID: tenantID, SupplierID: supplierID, OperatorID: in.OperatorID, ExceptID: id}); err != nil {
				return err
			}
		}
		out, err = q.UpdateSupplierOwner(ctx, store.UpdateSupplierOwnerParams{TenantID: tenantID, SupplierID: supplierID, ID: id, EmployeeID: in.EmployeeID, EmployeeName: in.EmployeeName, ResponsibilityCode: in.ResponsibilityCode, IsPrimary: in.IsPrimary, StartDate: start, EndDate: end, OperatorID: in.OperatorID})
		if err != nil {
			return err
		}
		return recordSupplierChange(ctx, q, tenantID, supplierID, "UPDATE", "OWNER", "更新负责人："+out.EmployeeName, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}
func (s *Service) DeactivateSupplierOwner(ctx context.Context, tenantID, supplierID, id, operatorID int64, operatorName string) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.DeactivateSupplierOwner(ctx, store.DeactivateSupplierOwnerParams{TenantID: tenantID, SupplierID: supplierID, ID: id, OperatorID: operatorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("MD_OWNER_NOT_FOUND", "负责人关系不存在或已移除")
		}
		return recordSupplierChange(ctx, q, tenantID, supplierID, "DEACTIVATE", "OWNER", "移除供应商负责人", nil, nil, operatorID, operatorName)
	})
}
func (s *Service) ListSupplierChanges(ctx context.Context, tenantID, supplierID int64) ([]store.SupplierChangeLog, error) {
	return s.q.ListSupplierChanges(ctx, store.ListSupplierChangesParams{TenantID: tenantID, SupplierID: supplierID})
}

func normalizeFactoryInput(in *FactoryInput) error {
	in.NameZh, in.NameEn = strings.TrimSpace(in.NameZh), strings.TrimSpace(in.NameEn)
	if in.NameZh == "" && in.NameEn == "" {
		return apierr.Invalid("MD_FACTORY_NAME_REQUIRED", "工厂中文名或英文名至少填写一项")
	}
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	in.Status = strings.ToUpper(strings.TrimSpace(in.Status))
	if in.Status == "" {
		in.Status = "PREPARING"
	}
	allowed := map[string]bool{"PREPARING": true, "COOPERATING": true, "SUSPENDED": true, "INACTIVE": true}
	if !allowed[in.Status] {
		return apierr.Invalid("MD_FACTORY_STATUS_INVALID", "工厂状态不正确")
	}
	if in.Status == "COOPERATING" && (len(in.CountryCode) != 2 || strings.TrimSpace(in.Timezone) == "") {
		return apierr.Invalid("MD_FACTORY_ACTIVE_FIELDS_REQUIRED", "合作中的工厂必须填写标准国家和 IANA 时区")
	}
	if in.CountryCode != "" && len(in.CountryCode) != 2 {
		return apierr.Invalid("MD_FACTORY_COUNTRY_INVALID", "国家/地区必须使用两位 ISO 代码")
	}
	if in.SupplierID == 0 {
		return apierr.Invalid("MD_FACTORY_SUPPLIER_REQUIRED", "必须选择所属供应商")
	}
	return nil
}
func (s *Service) ListFactories(ctx context.Context, tenantID int64, keyword, status, countryCode, city, productCategory string, supplierID, ownerID int64, page, size int32) ([]store.ListFactoriesRow, int64, error) {
	page, size = normalizePage(page, size)
	rows, err := s.q.ListFactories(ctx, store.ListFactoriesParams{TenantID: tenantID, Keyword: keyword, Status: status, CountryCode: strings.ToUpper(countryCode), City: city, ProductCategory: strings.TrimSpace(productCategory), SupplierID: supplierID, OwnerID: ownerID, PageSize: size, PageOffset: (page - 1) * size})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}
func (s *Service) GetFactory(ctx context.Context, tenantID, id int64) (store.Factory, error) {
	f, err := s.q.GetFactory(ctx, store.GetFactoryParams{TenantID: tenantID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Factory{}, apierr.NotFound("MD_FACTORY_NOT_FOUND", "工厂不存在")
	}
	return f, err
}
func (s *Service) CreateFactory(ctx context.Context, tenantID int64, in FactoryInput) (store.Factory, error) {
	if err := normalizeFactoryInput(&in); err != nil {
		return store.Factory{}, err
	}
	var out store.Factory
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		supplier, err := q.GetSupplier(ctx, store.GetSupplierParams{TenantID: tenantID, ID: in.SupplierID})
		if err != nil || supplier.Status != "ACTIVE" {
			return apierr.Conflict("MD_FACTORY_SUPPLIER_INACTIVE", "所属供应商不存在或已停用")
		}
		code := strings.TrimSpace(in.Code)
		if code == "" {
			code, err = s.nextNumber(ctx, q, tenantID, "FACTORY")
			if err != nil {
				return err
			}
		}
		out, err = q.CreateFactory(ctx, store.CreateFactoryParams{TenantID: tenantID, SupplierID: in.SupplierID, Code: code, NameZh: in.NameZh, NameEn: in.NameEn, ShortName: in.ShortName, CountryCode: in.CountryCode, Timezone: in.Timezone, StateProvince: in.StateProvince, City: in.City, District: in.District, PostalCode: in.PostalCode, Address: in.Address, Status: in.Status, Remark: in.Remark, OperatorID: in.OperatorID})
		if err != nil {
			return translateUnique(err, "MD_FACTORY_CODE_TAKEN", "工厂编码已存在")
		}
		return recordFactoryChange(ctx, q, tenantID, out.ID, "CREATE", "PROFILE", "新增工厂", nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}
func (s *Service) UpdateFactory(ctx context.Context, tenantID, id int64, in FactoryInput) (store.Factory, error) {
	if err := normalizeFactoryInput(&in); err != nil {
		return store.Factory{}, err
	}
	var out store.Factory
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetFactory(ctx, store.GetFactoryParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		supplier, err := q.GetSupplier(ctx, store.GetSupplierParams{TenantID: tenantID, ID: in.SupplierID})
		if errors.Is(err, pgx.ErrNoRows) {
			return apierr.Conflict("MD_FACTORY_SUPPLIER_INACTIVE", "所属供应商不存在或已停用")
		}
		if err != nil {
			return err
		}
		if supplier.Status != "ACTIVE" && (in.Status == "PREPARING" || in.Status == "COOPERATING") {
			return apierr.Conflict("MD_FACTORY_SUPPLIER_INACTIVE", "供应商已停用，不能启用或恢复其工厂")
		}
		if before.SupplierID != in.SupplierID && strings.TrimSpace(in.TransferReason) == "" {
			return apierr.Invalid("MD_FACTORY_TRANSFER_REASON_REQUIRED", "变更所属供应商时必须填写转移原因")
		}
		if before.Status != in.Status && strings.TrimSpace(in.TransferReason) == "" {
			return apierr.Invalid("MD_FACTORY_STATUS_REASON_REQUIRED", "变更工厂状态时必须填写原因")
		}
		out, err = q.UpdateFactory(ctx, store.UpdateFactoryParams{TenantID: tenantID, ID: id, SupplierID: in.SupplierID, NameZh: in.NameZh, NameEn: in.NameEn, ShortName: in.ShortName, CountryCode: in.CountryCode, Timezone: in.Timezone, StateProvince: in.StateProvince, City: in.City, District: in.District, PostalCode: in.PostalCode, Address: in.Address, Status: in.Status, Remark: in.Remark, OperatorID: in.OperatorID})
		if err != nil {
			return err
		}
		summary := "更新工厂基本资料"
		if before.SupplierID != out.SupplierID {
			summary = "转移工厂所属供应商：" + strings.TrimSpace(in.TransferReason)
		} else if before.Status != out.Status {
			summary = "工厂状态由 " + before.Status + " 变更为 " + out.Status + "：" + strings.TrimSpace(in.TransferReason)
		}
		return recordFactoryChange(ctx, q, tenantID, id, "UPDATE", "PROFILE", summary, before, out, in.OperatorID, in.OperatorName)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Factory{}, apierr.NotFound("MD_FACTORY_NOT_FOUND", "工厂不存在")
	}
	return out, err
}
func (s *Service) ListFactoryCountries(ctx context.Context, tenantID int64, includeInactive bool) ([]store.ListFactoryCountriesRow, error) {
	return s.q.ListFactoryCountries(ctx, store.ListFactoryCountriesParams{TenantID: tenantID, IncludeInactive: includeInactive})
}

func (s *Service) ListFactoryContacts(ctx context.Context, tenantID, factoryID int64, includeInactive bool) ([]store.FactoryContact, error) {
	return s.q.ListFactoryContacts(ctx, store.ListFactoryContactsParams{TenantID: tenantID, FactoryID: factoryID, IncludeInactive: includeInactive})
}
func (s *Service) CreateFactoryContact(ctx context.Context, tenantID, factoryID int64, in PartyContactInput) (store.FactoryContact, error) {
	if err := normalizePartyContact(&in); err != nil {
		return store.FactoryContact{}, err
	}
	var out store.FactoryContact
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsPrimary {
			if err := q.ClearFactoryPrimaryContact(ctx, store.ClearFactoryPrimaryContactParams{TenantID: tenantID, FactoryID: factoryID, OperatorID: in.OperatorID, ExceptID: 0}); err != nil {
				return err
			}
		}
		var err error
		out, err = q.CreateFactoryContact(ctx, store.CreateFactoryContactParams{TenantID: tenantID, FactoryID: factoryID, Name: in.Name, Department: in.Department, Title: in.Title, Phone: in.Phone, Email: in.Email, IsPrimary: in.IsPrimary, Remark: in.Remark, OperatorID: in.OperatorID})
		if err != nil {
			return err
		}
		return recordFactoryChange(ctx, q, tenantID, factoryID, "CREATE", "CONTACT", "新增联系人："+out.Name, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}
func (s *Service) UpdateFactoryContact(ctx context.Context, tenantID, factoryID, id int64, in PartyContactInput) (store.FactoryContact, error) {
	if err := normalizePartyContact(&in); err != nil {
		return store.FactoryContact{}, err
	}
	var out store.FactoryContact
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsPrimary {
			if err := q.ClearFactoryPrimaryContact(ctx, store.ClearFactoryPrimaryContactParams{TenantID: tenantID, FactoryID: factoryID, OperatorID: in.OperatorID, ExceptID: id}); err != nil {
				return err
			}
		}
		var err error
		out, err = q.UpdateFactoryContact(ctx, store.UpdateFactoryContactParams{TenantID: tenantID, FactoryID: factoryID, ID: id, Name: in.Name, Department: in.Department, Title: in.Title, Phone: in.Phone, Email: in.Email, IsPrimary: in.IsPrimary, Remark: in.Remark, OperatorID: in.OperatorID})
		if err != nil {
			return err
		}
		return recordFactoryChange(ctx, q, tenantID, factoryID, "UPDATE", "CONTACT", "更新联系人："+out.Name, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}
func (s *Service) DeactivateFactoryContact(ctx context.Context, tenantID, factoryID, id, operatorID int64, operatorName string) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.DeactivateFactoryContact(ctx, store.DeactivateFactoryContactParams{TenantID: tenantID, FactoryID: factoryID, ID: id, OperatorID: operatorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("MD_CONTACT_NOT_FOUND", "联系人不存在或已停用")
		}
		return recordFactoryChange(ctx, q, tenantID, factoryID, "DEACTIVATE", "CONTACT", "停用工厂联系人", nil, nil, operatorID, operatorName)
	})
}
func (s *Service) ListFactoryOwners(ctx context.Context, tenantID, factoryID int64, includeInactive bool) ([]store.FactoryOwner, error) {
	return s.q.ListFactoryOwners(ctx, store.ListFactoryOwnersParams{TenantID: tenantID, FactoryID: factoryID, IncludeInactive: includeInactive})
}
func (s *Service) CreateFactoryOwner(ctx context.Context, tenantID, factoryID int64, in PartyOwnerInput) (store.FactoryOwner, error) {
	start, end, err := normalizePartyOwner(&in)
	if err != nil {
		return store.FactoryOwner{}, err
	}
	var out store.FactoryOwner
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsPrimary {
			if err := q.ClearFactoryPrimaryOwner(ctx, store.ClearFactoryPrimaryOwnerParams{TenantID: tenantID, FactoryID: factoryID, OperatorID: in.OperatorID, ExceptID: 0}); err != nil {
				return err
			}
		}
		out, err = q.CreateFactoryOwner(ctx, store.CreateFactoryOwnerParams{TenantID: tenantID, FactoryID: factoryID, EmployeeID: in.EmployeeID, EmployeeName: in.EmployeeName, ResponsibilityCode: in.ResponsibilityCode, IsPrimary: in.IsPrimary, StartDate: start, EndDate: end, OperatorID: in.OperatorID})
		if err != nil {
			return translateUnique(err, "MD_FACTORY_OWNER_DUPLICATE", "该员工已以相同职责负责此工厂")
		}
		return recordFactoryChange(ctx, q, tenantID, factoryID, "CREATE", "OWNER", "新增负责人："+out.EmployeeName, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}
func (s *Service) UpdateFactoryOwner(ctx context.Context, tenantID, factoryID, id int64, in PartyOwnerInput) (store.FactoryOwner, error) {
	start, end, err := normalizePartyOwner(&in)
	if err != nil {
		return store.FactoryOwner{}, err
	}
	var out store.FactoryOwner
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if in.IsPrimary {
			if err := q.ClearFactoryPrimaryOwner(ctx, store.ClearFactoryPrimaryOwnerParams{TenantID: tenantID, FactoryID: factoryID, OperatorID: in.OperatorID, ExceptID: id}); err != nil {
				return err
			}
		}
		out, err = q.UpdateFactoryOwner(ctx, store.UpdateFactoryOwnerParams{TenantID: tenantID, FactoryID: factoryID, ID: id, EmployeeID: in.EmployeeID, EmployeeName: in.EmployeeName, ResponsibilityCode: in.ResponsibilityCode, IsPrimary: in.IsPrimary, StartDate: start, EndDate: end, OperatorID: in.OperatorID})
		if err != nil {
			return err
		}
		return recordFactoryChange(ctx, q, tenantID, factoryID, "UPDATE", "OWNER", "更新负责人："+out.EmployeeName, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}
func (s *Service) DeactivateFactoryOwner(ctx context.Context, tenantID, factoryID, id, operatorID int64, operatorName string) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		n, err := q.DeactivateFactoryOwner(ctx, store.DeactivateFactoryOwnerParams{TenantID: tenantID, FactoryID: factoryID, ID: id, OperatorID: operatorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound("MD_OWNER_NOT_FOUND", "负责人关系不存在或已移除")
		}
		return recordFactoryChange(ctx, q, tenantID, factoryID, "DEACTIVATE", "OWNER", "移除工厂负责人", nil, nil, operatorID, operatorName)
	})
}

func parseNumeric(value, field string) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if strings.TrimSpace(value) == "" {
		value = "0"
	}
	if err := n.Scan(value); err != nil {
		return pgtype.Numeric{}, apierr.Invalid("MD_NUMBER_INVALID", field+"必须是有效数字")
	}
	return n, nil
}
func (s *Service) ListFactoryCapabilities(ctx context.Context, tenantID, factoryID int64) ([]store.FactoryCapability, error) {
	return s.q.ListFactoryCapabilities(ctx, store.ListFactoryCapabilitiesParams{TenantID: tenantID, FactoryID: factoryID})
}
func (s *Service) CreateFactoryCapability(ctx context.Context, tenantID, factoryID int64, in FactoryCapabilityInput) (store.FactoryCapability, error) {
	if strings.TrimSpace(in.ProductCategory) == "" {
		return store.FactoryCapability{}, apierr.Invalid("MD_FACTORY_CAPABILITY_REQUIRED", "主要产品类别必填")
	}
	capacity, err := parseNumeric(in.MonthlyCapacity, "月产能")
	if err != nil {
		return store.FactoryCapability{}, err
	}
	moq, err := parseNumeric(in.MOQ, "最小起订量")
	if err != nil {
		return store.FactoryCapability{}, err
	}
	confirmed, err := parseOptionalDate(in.ConfirmedOn, "最近确认日期")
	if err != nil {
		return store.FactoryCapability{}, err
	}
	out, err := s.q.CreateFactoryCapability(ctx, store.CreateFactoryCapabilityParams{TenantID: tenantID, FactoryID: factoryID, ProductCategory: in.ProductCategory, Process: in.Process, MonthlyCapacity: capacity, CapacityUnit: in.CapacityUnit, Moq: moq, LeadTimeDays: in.LeadTimeDays, PeriodLabel: in.PeriodLabel, ConfirmedOn: confirmed, Remark: in.Remark, OperatorID: in.OperatorID})
	if err == nil {
		_ = recordFactoryChange(ctx, s.q, tenantID, factoryID, "CREATE", "CAPABILITY", "新增生产能力："+out.ProductCategory, nil, out, in.OperatorID, in.OperatorName)
	}
	return out, err
}
func (s *Service) DeleteFactoryCapability(ctx context.Context, tenantID, factoryID, id, operatorID int64, operatorName string) error {
	n, err := s.q.DeleteFactoryCapability(ctx, store.DeleteFactoryCapabilityParams{TenantID: tenantID, FactoryID: factoryID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("MD_FACTORY_CAPABILITY_NOT_FOUND", "生产能力记录不存在")
	}
	return recordFactoryChange(ctx, s.q, tenantID, factoryID, "DELETE", "CAPABILITY", "移除生产能力", nil, nil, operatorID, operatorName)
}
func (s *Service) ListFactoryCertificates(ctx context.Context, tenantID, factoryID int64) ([]FactoryCertificateView, error) {
	rows, err := s.q.ListFactoryCertificates(ctx, store.ListFactoryCertificatesParams{TenantID: tenantID, FactoryID: factoryID})
	if err != nil {
		return nil, err
	}
	out := make([]FactoryCertificateView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.withFileURL(ctx, r))
	}
	return out, nil
}
func (s *Service) CreateFactoryCertificate(ctx context.Context, tenantID, factoryID int64, in FactoryCertificateInput) (store.FactoryCertificate, error) {
	if strings.TrimSpace(in.Name) == "" {
		return store.FactoryCertificate{}, apierr.Invalid("MD_FACTORY_CERTIFICATE_REQUIRED", "认证名称必填")
	}
	issued, err := parseOptionalDate(in.IssuedOn, "签发日期")
	if err != nil {
		return store.FactoryCertificate{}, err
	}
	expires, err := parseOptionalDate(in.ExpiresOn, "到期日期")
	if err != nil {
		return store.FactoryCertificate{}, err
	}
	if issued.Valid && expires.Valid && expires.Time.Before(issued.Time) {
		return store.FactoryCertificate{}, apierr.Invalid("MD_FACTORY_CERTIFICATE_DATES_INVALID", "到期日期不能早于签发日期")
	}
	if in.Status == "" {
		in.Status = "VALID"
	}
	if err := validateCertificateFileKey(tenantID, factoryID, in.FileKey); err != nil {
		return store.FactoryCertificate{}, err
	}
	out, err := s.q.CreateFactoryCertificate(ctx, store.CreateFactoryCertificateParams{TenantID: tenantID, FactoryID: factoryID, Name: in.Name, CertificateNo: in.CertificateNo, IssuedOn: issued, ExpiresOn: expires, Status: in.Status, FileKey: in.FileKey, Remark: in.Remark, OperatorID: in.OperatorID})
	if err == nil {
		_ = recordFactoryChange(ctx, s.q, tenantID, factoryID, "CREATE", "CERTIFICATE", "新增资质："+out.Name, nil, out, in.OperatorID, in.OperatorName)
	}
	return out, err
}
func (s *Service) DeleteFactoryCertificate(ctx context.Context, tenantID, factoryID, id, operatorID int64, operatorName string) error {
	// Read the file key before the row goes: afterwards nothing points at
	// the object and it would leak silently.
	var fileKey string
	_ = s.pool.QueryRow(ctx,
		`SELECT file_key FROM factory_certificates WHERE tenant_id=$1 AND factory_id=$2 AND id=$3`,
		tenantID, factoryID, id).Scan(&fileKey)
	n, err := s.q.DeleteFactoryCertificate(ctx, store.DeleteFactoryCertificateParams{TenantID: tenantID, FactoryID: factoryID, ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return apierr.NotFound("MD_FACTORY_CERTIFICATE_NOT_FOUND", "资质记录不存在")
	}
	if fileKey != "" && s.files != nil {
		// Row first, object second; a removal hiccup leaves only litter a
		// sweep can collect, and must not fail the delete the user saw.
		_ = s.files.Remove(ctx, fileKey)
	}
	return recordFactoryChange(ctx, s.q, tenantID, factoryID, "DELETE", "CERTIFICATE", "移除工厂资质", nil, nil, operatorID, operatorName)
}
func (s *Service) ListFactoryChanges(ctx context.Context, tenantID, factoryID int64) ([]store.FactoryChangeLog, error) {
	return s.q.ListFactoryChanges(ctx, store.ListFactoryChangesParams{TenantID: tenantID, FactoryID: factoryID})
}
