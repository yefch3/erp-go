package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type CustomerImportRow struct {
	Code, Name, CountryCode, CustomerType, Currency, PaymentTerm string
	ContactName, ContactEmail, ContactPhone, Remark              string
}

type CustomerImportVerdict struct {
	Line               int32
	Code, Name, Reason string
	OK                 bool
}

// ImportCustomers 的预检和写入共用同一个校验函数。只要有一行错误，正式导入就
// 一行也不写，避免用户无法判断一批表格究竟成功了哪一半。
func (s *Service) ImportCustomers(ctx context.Context, tenantID int64, rows []CustomerImportRow, dryRun bool, operatorID int64, operatorName string) ([]CustomerImportVerdict, int32, error) {
	verdicts := make([]CustomerImportVerdict, len(rows))
	seenCodes := map[string]bool{}
	seenNames := map[string]bool{}
	var ready int32
	for i, row := range rows {
		row.Code = strings.ToUpper(strings.TrimSpace(row.Code))
		row.Name = strings.TrimSpace(row.Name)
		v := CustomerImportVerdict{Line: int32(i + 2), Code: row.Code, Name: row.Name, OK: true}
		if row.Name == "" {
			v.OK, v.Reason = false, "客户名称必填"
		}
		if v.OK {
			if _, err := normaliseCountry(row.CountryCode); err != nil {
				v.OK, v.Reason = false, "国家代码必须是两位 ISO 代码"
			}
		}
		if v.OK && row.Code != "" {
			exists, err := s.q.CustomerCodeExists(ctx, store.CustomerCodeExistsParams{TenantID: tenantID, Code: row.Code})
			if err != nil {
				return nil, 0, err
			}
			if exists || seenCodes[row.Code] {
				v.OK, v.Reason = false, "客户编码已存在或在本批次重复"
			}
			seenCodes[row.Code] = true
		}
		nameKey := strings.ToLower(row.Name)
		if v.OK && nameKey != "" {
			dups, err := s.q.CustomerDuplicateCandidates(ctx, store.CustomerDuplicateCandidatesParams{TenantID: tenantID, Name: row.Name, TaxID: "", ExcludeID: 0})
			if err != nil {
				return nil, 0, err
			}
			if len(dups) > 0 || seenNames[nameKey] {
				v.OK, v.Reason = false, "客户名称已存在或在本批次重复"
			}
			seenNames[nameKey] = true
		}
		if v.OK && row.ContactEmail != "" {
			ci := CustomerContactInput{Name: defaultText(row.ContactName, "主要联系人"), Email: row.ContactEmail}
			if err := ci.normalizeAndValidate(); err != nil {
				v.OK, v.Reason = false, "联系人邮箱格式不正确"
			}
		}
		if v.OK {
			ready++
		}
		verdicts[i] = v
	}
	if dryRun || ready != int32(len(rows)) || len(rows) == 0 {
		return verdicts, 0, nil
	}

	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		for i, row := range rows {
			code := strings.ToUpper(strings.TrimSpace(row.Code))
			if code == "" {
				var err error
				code, err = s.nextNumber(ctx, q, tenantID, "CUSTOMER")
				if err != nil {
					return err
				}
			}
			countryCode, _ := normaliseCountry(row.CountryCode)
			currency := strings.ToUpper(strings.TrimSpace(row.Currency))
			if currency == "" {
				currency = "USD"
			}
			customerType := strings.ToUpper(strings.TrimSpace(row.CustomerType))
			c, err := q.CreateCustomer(ctx, store.CreateCustomerParams{
				TenantID: tenantID, Code: code, Name: strings.TrimSpace(row.Name), Country: "", CountryCode: countryCode,
				Address: "", Currency: currency, PaymentTerm: row.PaymentTerm, Remark: row.Remark,
				ShortName: "", EnglishName: "", CustomerType: customerType, Industry: "", Source: "",
				Tags: []string{}, Website: "", PrimaryLanguage: "", Timezone: "", RegisteredName: "",
				RegistrationNo: "", TaxID: "", InvoiceTitle: "", InvoiceTaxNo: "", InvoiceRemark: "",
				PaymentDays: 0, CreditLimitMinor: 0, CreditCurrency: currency, CreditStatus: "NORMAL",
				BusinessStatus: "PROSPECT", OperatorID: operatorID,
			})
			if err != nil {
				return fmt.Errorf("第 %d 行写入失败: %w", i+2, err)
			}
			if strings.TrimSpace(row.ContactName) != "" || strings.TrimSpace(row.ContactEmail) != "" || strings.TrimSpace(row.ContactPhone) != "" {
				_, err = q.CreateCustomerContact(ctx, store.CreateCustomerContactParams{TenantID: tenantID, CustomerID: c.ID,
					Name: defaultText(strings.TrimSpace(row.ContactName), "主要联系人"), Email: strings.TrimSpace(row.ContactEmail),
					Phone: strings.TrimSpace(row.ContactPhone), IsPrimary: true, Department: "", Title: "", Mobile: "",
					InstantMessaging: "", Language: "", Remark: "", SortOrder: 0, OperatorID: operatorID})
				if err != nil {
					return err
				}
			}
			if err := recordCustomerChange(ctx, q, tenantID, c.ID, "IMPORT", "BASIC", "批量导入客户", nil, c, operatorID, operatorName); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return verdicts, 0, err
	}
	return verdicts, int32(len(rows)), nil
}

func defaultText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
