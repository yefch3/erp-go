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

func recordSupplierChange(ctx context.Context, q *store.Queries, tenantID, supplierID int64, action, section, summary string, before, after any, operatorID int64, operatorName string) error {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	return q.CreateSupplierChange(ctx, store.CreateSupplierChangeParams{TenantID: tenantID, SupplierID: supplierID,
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
	return s.q.ListSupplierCountries(ctx, store.ListSupplierCountriesParams{TenantID: tenantID, IncludeInactive: includeInactive, AccessEmployeeID: supplierAccessEmployee(ctx)})
}

func (s *Service) ListSupplierContacts(ctx context.Context, tenantID, supplierID int64, includeInactive bool) ([]store.SupplierContact, error) {
	if _, err := s.GetSupplier(ctx, tenantID, supplierID); err != nil {
		return nil, err
	}
	return s.q.ListSupplierContacts(ctx, store.ListSupplierContactsParams{TenantID: tenantID, SupplierID: supplierID, IncludeInactive: includeInactive})
}

func (s *Service) CreateSupplierContact(ctx context.Context, tenantID, supplierID int64, in PartyContactInput) (store.SupplierContact, error) {
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return store.SupplierContact{}, err
	}
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
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return store.SupplierContact{}, err
	}
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
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return err
	}
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
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return nil, err
	}
	return s.q.ListSupplierOwners(ctx, store.ListSupplierOwnersParams{TenantID: tenantID, SupplierID: supplierID, IncludeInactive: includeInactive})
}
func (s *Service) CreateSupplierOwner(ctx context.Context, tenantID, supplierID int64, in PartyOwnerInput) (store.SupplierOwner, error) {
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return store.SupplierOwner{}, err
	}
	if supplierAccessEmployee(ctx) != 0 {
		return store.SupplierOwner{}, apierr.Permission("MD_SUPPLIER_OWNER_MANAGE_DENIED", "只有最高权限用户可以管理供应商负责人")
	}
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
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return store.SupplierOwner{}, err
	}
	if supplierAccessEmployee(ctx) != 0 {
		return store.SupplierOwner{}, apierr.Permission("MD_SUPPLIER_OWNER_MANAGE_DENIED", "只有最高权限用户可以管理供应商负责人")
	}
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
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return err
	}
	if supplierAccessEmployee(ctx) != 0 {
		return apierr.Permission("MD_SUPPLIER_OWNER_MANAGE_DENIED", "只有最高权限用户可以管理供应商负责人")
	}
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
	if err := s.AuthorizeSupplier(ctx, tenantID, supplierID); err != nil {
		return nil, err
	}
	return s.q.ListSupplierChanges(ctx, store.ListSupplierChangesParams{TenantID: tenantID, SupplierID: supplierID})
}
