package app

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

func readCustomerBasic(ctx context.Context, db store.DBTX, tenantID, id int64) (CustomerImportRow, error) {
	var b CustomerImportRow
	err := db.QueryRow(ctx, `SELECT c.code,c.name,c.short_name,c.country,c.country_code,c.customer_type,c.credit_grade,c.source,
 c.archive_creator,c.company_phone,c.fax_number,c.company_email,coalesce(a.address_line,c.address),coalesce(a.postal_code,''),coalesce(a.state,'')
 FROM customers c LEFT JOIN LATERAL (SELECT address_line,postal_code,state FROM customer_addresses
 WHERE tenant_id=c.tenant_id AND customer_id=c.id AND address_type='OFFICE' AND status='ACTIVE'
 ORDER BY is_default DESC,sort_order,id LIMIT 1) a ON true WHERE c.tenant_id=$1 AND c.id=$2`, tenantID, id).
		Scan(&b.Code, &b.Name, &b.ShortName, &b.CountryRegion, &b.CountryCode, &b.CustomerType, &b.CreditGrade, &b.Source,
			&b.ArchiveCreator, &b.CompanyPhone, &b.FaxNumber, &b.CompanyEmail, &b.Address, &b.PostalCode, &b.AddressState)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在或无权访问")
	}
	return b, err
}

func (s *Service) GetCustomerBasic(ctx context.Context, tenantID, id int64) (CustomerImportRow, error) {
	if err := s.AuthorizeCustomer(ctx, tenantID, id); err != nil {
		return CustomerImportRow{}, err
	}
	return readCustomerBasic(ctx, s.pool, tenantID, id)
}

// This is also used by imports, so company/contact fields cannot drift into two
// unrelated stores. Address edits maintain the existing OFFICE address record.
func writeCustomerBasic(ctx context.Context, tx pgx.Tx, tenantID, id, operatorID int64, b CustomerImportRow, preserveBlanks bool) error {
	before, err := readCustomerBasic(ctx, tx, tenantID, id)
	if err != nil {
		return err
	}
	if preserveBlanks {
		dst := []*string{&b.Name, &b.ShortName, &b.CountryRegion, &b.CountryCode, &b.CustomerType, &b.CreditGrade, &b.Source, &b.ArchiveCreator, &b.CompanyPhone, &b.FaxNumber, &b.CompanyEmail, &b.Address, &b.PostalCode, &b.AddressState}
		src := []string{before.Name, before.ShortName, before.CountryRegion, before.CountryCode, before.CustomerType, before.CreditGrade, before.Source, before.ArchiveCreator, before.CompanyPhone, before.FaxNumber, before.CompanyEmail, before.Address, before.PostalCode, before.AddressState}
		for i, p := range dst {
			if strings.TrimSpace(*p) == "" {
				*p = src[i]
			}
		}
	}
	if err := validateCustomerBasic(b); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE customers SET name=$3,short_name=$4,country=$5,country_code=$6,customer_type=$7,credit_grade=$8::text,source=$9,
 archive_creator=$10,company_phone=$11,fax_number=$12,company_email=$13,address=$14,updated_by=$15,updated_at=now(),
 credit_graded_at=CASE WHEN credit_grade<>$8::text THEN now() ELSE credit_graded_at END WHERE tenant_id=$1 AND id=$2`, tenantID, id, b.Name, b.ShortName, b.CountryRegion, b.CountryCode, b.CustomerType, b.CreditGrade, b.Source, b.ArchiveCreator, b.CompanyPhone, b.FaxNumber, b.CompanyEmail, b.Address, operatorID)
	if err != nil {
		return err
	}
	var addressID int64
	err = tx.QueryRow(ctx, `SELECT id FROM customer_addresses WHERE tenant_id=$1 AND customer_id=$2 AND address_type='OFFICE' AND status='ACTIVE' ORDER BY is_default DESC,sort_order,id LIMIT 1 FOR UPDATE`, tenantID, id).Scan(&addressID)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE customer_addresses SET address_line=$4,postal_code=$5,country_code=$6,state=$7,updated_by=$8,updated_at=now() WHERE tenant_id=$1 AND customer_id=$2 AND id=$3`, tenantID, id, addressID, b.Address, b.PostalCode, b.CountryCode, b.AddressState, operatorID)
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = nil
		if b.Address != "" || b.PostalCode != "" {
			_, err = tx.Exec(ctx, `INSERT INTO customer_addresses(tenant_id,customer_id,address_type,address_line,postal_code,country_code,state,is_default,created_by,updated_by) VALUES($1,$2,'OFFICE',$3,$4,$5,$6,true,$7,$7)`, tenantID, id, b.Address, b.PostalCode, b.CountryCode, b.AddressState, operatorID)
		}
	}
	return err
}

func validateCustomerBasic(b CustomerImportRow) error {
	if strings.TrimSpace(b.Name) == "" {
		return apierr.Invalid("MD_CUSTOMER_NAME_REQUIRED", "客户名称必填")
	}
	for _, f := range []struct {
		label, value string
		max          int
	}{{"客户代码", b.Code, 50}, {"客户名称", b.Name, 200}, {"客户简称", b.ShortName, 100}, {"所属地区", b.CountryRegion, 100}, {"客户类型", b.CustomerType, 50}, {"客户等级", b.CreditGrade, 32}, {"客户来源", b.Source, 50}, {"建档人", b.ArchiveCreator, 100}, {"公司电话", b.CompanyPhone, 50}, {"传真号码", b.FaxNumber, 50}, {"电子信箱", b.CompanyEmail, 200}, {"通信地址", b.Address, 500}, {"邮政编码", b.PostalCode, 30}} {
		if utf8.RuneCountInString(f.value) > f.max {
			return apierr.Invalid("MD_CUSTOMER_FIELD_TOO_LONG", fmt.Sprintf("%s不能超过%d个字", f.label, f.max))
		}
	}
	if b.CreditGrade != "" && b.CreditGrade != "A" && b.CreditGrade != "B" && b.CreditGrade != "C" && b.CreditGrade != "D" {
		return apierr.Invalid("MD_CUSTOMER_GRADE_INVALID", "客户等级请选择 A、B、C 或 D")
	}
	if _, err := normaliseCountry(b.CountryCode); err != nil {
		return err
	}
	if b.CompanyEmail != "" {
		if _, err := mail.ParseAddress(b.CompanyEmail); err != nil {
			return apierr.Invalid("MD_COMPANY_EMAIL_INVALID", "电子信箱格式不正确")
		}
	}
	return nil
}

func (s *Service) SaveCustomerBasic(ctx context.Context, tenantID, id int64, b CustomerImportRow, operatorID int64, operatorName string) (CustomerImportRow, error) {
	if err := s.AuthorizeCustomer(ctx, tenantID, id); err != nil {
		return b, err
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockCustomerForImport(ctx, tx, tenantID, id, customerAccessEmployee(ctx)); err != nil {
			return err
		}
		before, err := readCustomerBasic(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if err = writeCustomerBasic(ctx, tx, tenantID, id, operatorID, b, false); err != nil {
			return err
		}
		return recordCustomerChange(ctx, s.q.WithTx(tx), tenantID, id, "UPDATE", "BASIC", "修改客户基本资料", before, b, operatorID, operatorName)
	})
	if err != nil {
		return b, err
	}
	return s.GetCustomerBasic(ctx, tenantID, id)
}

func lockCustomerForImport(ctx context.Context, tx pgx.Tx, tenantID, id, employeeID int64) error {
	var found int64
	err := tx.QueryRow(ctx, `SELECT c.id FROM customers c WHERE c.tenant_id=$1 AND c.id=$2
 AND ($3::bigint=0 OR EXISTS(SELECT 1 FROM customer_owners o WHERE o.tenant_id=c.tenant_id AND o.customer_id=c.id AND o.employee_id=$3
 AND o.status='ACTIVE' AND (o.start_date IS NULL OR o.start_date<=CURRENT_DATE) AND (o.end_date IS NULL OR o.end_date>=CURRENT_DATE))) FOR UPDATE`, tenantID, id, employeeID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在或无权访问")
	}
	return err
}
