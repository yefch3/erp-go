package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type CustomerContactInput struct {
	Name, Department, Title, Email, Phone, Mobile string
	InstantMessaging, Language, Remark            string
	IsPrimary                                     bool
	SortOrder                                     int32
	EmailPermission                               string
	EmailCategories                               []string
	OperatorID                                    int64
	OperatorName                                  string
}

var customerPhonePattern = regexp.MustCompile(`^[0-9+().\-\s]{6,30}$`)

var customerEmailPermissions = map[string]bool{
	"ALLOWED": true, "OPTED_OUT": true, "INVALID": true,
}

var customerEmailCategories = map[string]bool{
	"BUSINESS": true, "QUOTATION": true, "SHIPPING": true, "MARKETING": true,
}

type CustomerOwnerInput struct {
	EmployeeID         int64
	EmployeeName       string
	ResponsibilityCode string
	StartDate, EndDate string
	IsPrimary          bool
	OperatorID         int64
	OperatorName       string
}

// normalizeAndValidateOwner 统一校验负责人职责和生效日期，避免新增与编辑使用不同规则。
func normalizeAndValidateOwner(in *CustomerOwnerInput) (pgtype.Date, pgtype.Date, error) {
	in.ResponsibilityCode = strings.ToUpper(strings.TrimSpace(in.ResponsibilityCode))
	if in.ResponsibilityCode == "" {
		return pgtype.Date{}, pgtype.Date{}, apierr.Invalid("MD_CUSTOMER_OWNER_REQUIRED", "负责人职责必填")
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

// recordCustomerChange 把同一套审计格式用于基本资料、地址、联系人和负责人，
// 这样详情页不需要分别拼接多张业务表才能回答“谁在什么时候改了什么”。
func recordCustomerChange(ctx context.Context, q *store.Queries, tenantID, customerID int64,
	action, section, summary string, before, after any, operatorID int64, operatorName string,
) error {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, err := q.InsertCustomerChangeLog(ctx, store.InsertCustomerChangeLogParams{
		TenantID: tenantID, CustomerID: customerID, Action: action, Section: section,
		Summary: summary, BeforeData: beforeJSON, AfterData: afterJSON,
		OperatorID: operatorID, OperatorName: operatorName,
	})
	return err
}

func (in *CustomerContactInput) normalizeAndValidate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Mobile = strings.TrimSpace(in.Mobile)
	in.Language = strings.TrimSpace(in.Language)
	in.EmailPermission = strings.ToUpper(strings.TrimSpace(in.EmailPermission))
	if in.EmailPermission == "" {
		in.EmailPermission = "ALLOWED"
	}
	if in.Name == "" {
		return apierr.Invalid("MD_CONTACT_NAME_REQUIRED", "联系人姓名必填")
	}
	if in.Email != "" {
		if _, err := mail.ParseAddress(in.Email); err != nil {
			return apierr.Invalid("MD_CONTACT_EMAIL_INVALID", "联系人邮箱格式不正确")
		}
	}
	for field, value := range map[string]string{"电话": in.Phone, "手机": in.Mobile} {
		if value != "" && !customerPhonePattern.MatchString(value) {
			return apierr.Invalid("MD_CONTACT_PHONE_INVALID", field+"格式不正确，只能包含数字、空格和 +()-.")
		}
	}
	if !customerEmailPermissions[in.EmailPermission] {
		return apierr.Invalid("MD_CONTACT_EMAIL_PERMISSION_INVALID", "邮件接收状态不正确")
	}
	seen := make(map[string]bool, len(in.EmailCategories))
	normalizedCategories := make([]string, 0, len(in.EmailCategories))
	for _, category := range in.EmailCategories {
		category = strings.ToUpper(strings.TrimSpace(category))
		if !customerEmailCategories[category] {
			return apierr.Invalid("MD_CONTACT_EMAIL_CATEGORY_INVALID", "邮件类型不正确")
		}
		if !seen[category] {
			seen[category] = true
			normalizedCategories = append(normalizedCategories, category)
		}
	}
	in.EmailCategories = normalizedCategories
	if in.SortOrder < 0 {
		return apierr.Invalid("MD_CONTACT_SORT_INVALID", "联系人排序不能小于 0")
	}
	return nil
}

func (s *Service) ListCustomerContactsDetailed(ctx context.Context, tenantID, customerID int64, status string) ([]store.CustomerContact, error) {
	if _, _, err := s.GetCustomer(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	if status != "ALL" {
		status = ""
	}
	return s.q.ListCustomerContactsDetailed(ctx, store.ListCustomerContactsDetailedParams{TenantID: tenantID, CustomerID: customerID, Status: status})
}

func (s *Service) CreateCustomerContact(ctx context.Context, tenantID, customerID int64, in CustomerContactInput) (store.CustomerContact, error) {
	if err := in.normalizeAndValidate(); err != nil {
		return store.CustomerContact{}, err
	}
	var out store.CustomerContact
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.GetCustomer(ctx, store.GetCustomerParams{TenantID: tenantID, ID: customerID}); err != nil {
			return err
		}
		if in.IsPrimary {
			if err := q.ClearPrimaryCustomerContact(ctx, store.ClearPrimaryCustomerContactParams{TenantID: tenantID, CustomerID: customerID, OperatorID: in.OperatorID}); err != nil {
				return err
			}
		}
		var err error
		out, err = q.CreateCustomerContact(ctx, store.CreateCustomerContactParams{
			TenantID: tenantID, CustomerID: customerID, Name: in.Name, Department: in.Department,
			Title: in.Title, Email: in.Email, Phone: in.Phone, Mobile: in.Mobile,
			InstantMessaging: in.InstantMessaging, Language: in.Language, Remark: in.Remark,
			IsPrimary: in.IsPrimary, SortOrder: in.SortOrder, EmailPermission: in.EmailPermission,
			EmailCategories: in.EmailCategories, OperatorID: in.OperatorID,
		})
		if err != nil {
			return err
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "CREATE", "CONTACT", "新增联系人："+out.Name, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}

func (s *Service) UpdateCustomerContact(ctx context.Context, tenantID, customerID, id int64, in CustomerContactInput) (store.CustomerContact, error) {
	if err := in.normalizeAndValidate(); err != nil {
		return store.CustomerContact{}, err
	}
	var out store.CustomerContact
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetCustomerContact(ctx, store.GetCustomerContactParams{TenantID: tenantID, CustomerID: customerID, ID: id})
		if err != nil {
			return err
		}
		if in.IsPrimary {
			if err := q.ClearPrimaryCustomerContact(ctx, store.ClearPrimaryCustomerContactParams{TenantID: tenantID, CustomerID: customerID, OperatorID: in.OperatorID}); err != nil {
				return err
			}
		}
		out, err = q.UpdateCustomerContact(ctx, store.UpdateCustomerContactParams{
			TenantID: tenantID, CustomerID: customerID, ID: id, Name: in.Name,
			Department: in.Department, Title: in.Title, Email: in.Email, Phone: in.Phone,
			Mobile: in.Mobile, InstantMessaging: in.InstantMessaging, Language: in.Language,
			Remark: in.Remark, IsPrimary: in.IsPrimary, SortOrder: in.SortOrder,
			EmailPermission: in.EmailPermission, EmailCategories: in.EmailCategories, OperatorID: in.OperatorID,
		})
		if err != nil {
			return err
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "UPDATE", "CONTACT", "更新联系人："+out.Name, before, out, in.OperatorID, in.OperatorName)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.CustomerContact{}, apierr.NotFound("MD_CONTACT_NOT_FOUND", "联系人不存在或已停用")
	}
	return out, err
}

func (s *Service) DeactivateCustomerContact(ctx context.Context, tenantID, customerID, id, operatorID int64, operatorName string) error {
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetCustomerContact(ctx, store.GetCustomerContactParams{TenantID: tenantID, CustomerID: customerID, ID: id})
		if err != nil {
			return apierr.NotFound("MD_CONTACT_NOT_FOUND", "联系人不存在")
		}
		n, err := q.DeactivateCustomerContact(ctx, store.DeactivateCustomerContactParams{TenantID: tenantID, CustomerID: customerID, ID: id, OperatorID: operatorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.Conflict("MD_CONTACT_INACTIVE", "联系人已停用")
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "DEACTIVATE", "CONTACT", "停用联系人："+before.Name, before, nil, operatorID, operatorName)
	})
}

func parseOptionalDate(value, field string) (pgtype.Date, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Date{}, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return pgtype.Date{}, apierr.Invalid("MD_DATE_INVALID", field+"必须是 YYYY-MM-DD")
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

func (s *Service) ListCustomerOwners(ctx context.Context, tenantID, customerID int64, status string) ([]store.CustomerOwner, error) {
	if _, _, err := s.GetCustomer(ctx, tenantID, customerID); err != nil {
		return nil, err
	}
	if status != "ALL" {
		status = ""
	}
	return s.q.ListCustomerOwners(ctx, store.ListCustomerOwnersParams{TenantID: tenantID, CustomerID: customerID, Status: status})
}

func (s *Service) CreateCustomerOwner(ctx context.Context, tenantID, customerID int64, in CustomerOwnerInput) (store.CustomerOwner, error) {
	if in.EmployeeID == 0 || strings.TrimSpace(in.EmployeeName) == "" {
		return store.CustomerOwner{}, apierr.Invalid("MD_CUSTOMER_OWNER_REQUIRED", "负责人、姓名和职责必填")
	}
	start, end, err := normalizeAndValidateOwner(&in)
	if err != nil {
		return store.CustomerOwner{}, err
	}
	var out store.CustomerOwner
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.GetCustomer(ctx, store.GetCustomerParams{TenantID: tenantID, ID: customerID}); err != nil {
			return err
		}
		if in.IsPrimary {
			if err := q.ClearPrimaryCustomerOwners(ctx, store.ClearPrimaryCustomerOwnersParams{
				TenantID: tenantID, CustomerID: customerID, ExcludeID: 0, OperatorID: in.OperatorID,
			}); err != nil {
				return err
			}
		}
		out, err = q.CreateCustomerOwner(ctx, store.CreateCustomerOwnerParams{TenantID: tenantID, CustomerID: customerID,
			EmployeeID: in.EmployeeID, EmployeeName: strings.TrimSpace(in.EmployeeName), ResponsibilityCode: in.ResponsibilityCode,
			StartDate: start, EndDate: end, IsPrimary: in.IsPrimary, OperatorID: in.OperatorID})
		if err != nil {
			return translateUnique(err, "MD_CUSTOMER_OWNER_DUPLICATE", "该员工已以相同职责负责此客户")
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "CREATE", "OWNER", "新增负责人："+out.EmployeeName, nil, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}

// UpdateCustomerOwner 修改负责人职责、有效期和主要负责人标记；员工身份本身不在编辑中替换。
func (s *Service) UpdateCustomerOwner(ctx context.Context, tenantID, customerID, id int64, in CustomerOwnerInput) (store.CustomerOwner, error) {
	start, end, err := normalizeAndValidateOwner(&in)
	if err != nil {
		return store.CustomerOwner{}, err
	}
	var out store.CustomerOwner
	err = pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetCustomerOwner(ctx, store.GetCustomerOwnerParams{TenantID: tenantID, CustomerID: customerID, ID: id})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierr.NotFound("MD_CUSTOMER_OWNER_NOT_FOUND", "负责人关系不存在")
			}
			return err
		}
		if before.Status != "ACTIVE" {
			return apierr.Conflict("MD_CUSTOMER_OWNER_INACTIVE", "已移除的负责人不能编辑")
		}
		if in.IsPrimary {
			if err := q.ClearPrimaryCustomerOwners(ctx, store.ClearPrimaryCustomerOwnersParams{
				TenantID: tenantID, CustomerID: customerID, ExcludeID: id, OperatorID: in.OperatorID,
			}); err != nil {
				return err
			}
		}
		out, err = q.UpdateCustomerOwner(ctx, store.UpdateCustomerOwnerParams{
			TenantID: tenantID, CustomerID: customerID, ID: id,
			ResponsibilityCode: in.ResponsibilityCode, StartDate: start, EndDate: end,
			IsPrimary: in.IsPrimary, OperatorID: in.OperatorID,
		})
		if err != nil {
			return translateUnique(err, "MD_CUSTOMER_OWNER_DUPLICATE", "该员工已以相同职责负责此客户")
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "UPDATE", "OWNER", "编辑负责人："+out.EmployeeName, before, out, in.OperatorID, in.OperatorName)
	})
	return out, err
}

func (s *Service) DeactivateCustomerOwner(ctx context.Context, tenantID, customerID, id int64, endDate string, operatorID int64, operatorName string) error {
	end, err := parseOptionalDate(endDate, "结束日期")
	if err != nil {
		return err
	}
	return pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		owners, err := q.ListCustomerOwners(ctx, store.ListCustomerOwnersParams{TenantID: tenantID, CustomerID: customerID, Status: "ALL"})
		if err != nil {
			return err
		}
		var before *store.CustomerOwner
		for i := range owners {
			if owners[i].ID == id {
				before = &owners[i]
				break
			}
		}
		if before == nil {
			return apierr.NotFound("MD_CUSTOMER_OWNER_NOT_FOUND", "负责人关系不存在")
		}
		n, err := q.DeactivateCustomerOwner(ctx, store.DeactivateCustomerOwnerParams{TenantID: tenantID, CustomerID: customerID, ID: id, EndDate: end, OperatorID: operatorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.Conflict("MD_CUSTOMER_OWNER_INACTIVE", "负责人关系已停用")
		}
		return recordCustomerChange(ctx, q, tenantID, customerID, "DEACTIVATE", "OWNER", "移除负责人："+before.EmployeeName, before, nil, operatorID, operatorName)
	})
}

func (s *Service) ListCustomerChanges(ctx context.Context, tenantID, customerID int64, page, size int32) ([]store.CustomerChangeLog, int64, error) {
	if _, _, err := s.GetCustomer(ctx, tenantID, customerID); err != nil {
		return nil, 0, err
	}
	page, size = normalizePage(page, size)
	rows, err := s.q.ListCustomerChangeLogs(ctx, store.ListCustomerChangeLogsParams{TenantID: tenantID, CustomerID: customerID, OffsetCount: (page - 1) * size, LimitCount: size})
	if err != nil {
		return nil, 0, err
	}
	total, err := s.q.CountCustomerChangeLogs(ctx, store.CountCustomerChangeLogsParams{TenantID: tenantID, CustomerID: customerID})
	return rows, total, err
}

func (s *Service) CheckCustomerDuplicates(ctx context.Context, tenantID int64, name, taxID string, excludeID int64) ([]store.CustomerDuplicateCandidatesRow, error) {
	name, taxID = strings.TrimSpace(name), strings.TrimSpace(taxID)
	if name == "" && taxID == "" {
		return nil, nil
	}
	return s.q.CustomerDuplicateCandidates(ctx, store.CustomerDuplicateCandidatesParams{TenantID: tenantID, Name: name, TaxID: taxID, ExcludeID: excludeID})
}
