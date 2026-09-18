package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type CustomerImportRow struct {
	Code, Name, CountryCode, CustomerType, Currency, PaymentTerm   string
	ContactName, ContactEmail, ContactPhone, ContactMobile, Remark string
	Address, ShortName, EnglishName, Industry, Source, Tags        string
	Website, PrimaryLanguage, Timezone                             string
	RegisteredName, RegistrationNo, TaxID                          string
	InvoiceTitle, InvoiceTaxNo, InvoiceRemark, BusinessStatus      string
	PostalCode, AddressState, CreditGrade, OwnerName               string
	OwnerEmployeeID                                                int64
	CustomFields                                                   map[string]string
}

type CustomerImportVerdict struct {
	Line               int32
	Code, Name, Reason string
	OK                 bool
}

// ImportCustomers 的预检和写入共用同一个校验函数。只要有一行错误，正式导入就
// 一行也不写，避免用户无法判断一批表格究竟成功了哪一半。
func (s *Service) ImportCustomers(ctx context.Context, tenantID int64, rows []CustomerImportRow, mappings []CustomerImportFieldMapping, dryRun bool, operatorID int64, operatorName string) ([]CustomerImportVerdict, int32, error) {
	seenSources := map[string]bool{}
	seenTargets := map[string]bool{}
	for _, mapping := range mappings {
		mapping.SourceKey = strings.TrimSpace(mapping.SourceKey)
		mapping.FieldKey = strings.TrimSpace(mapping.FieldKey)
		mapping.DisplayName = strings.TrimSpace(mapping.DisplayName)
		if mapping.SourceKey == "" || seenSources[mapping.SourceKey] {
			return nil, 0, apierr.Invalid("MD_CUSTOMER_IMPORT_FIELD_MAPPING_INVALID", "动态字段映射重复或缺少来源标识")
		}
		seenSources[mapping.SourceKey] = true
		if mapping.FieldKey == "" && mapping.DisplayName == "" {
			return nil, 0, apierr.Invalid("MD_CUSTOMER_IMPORT_FIELD_NAME_REQUIRED", "新客户字段显示名称必填")
		}
		targetKey := mapping.FieldKey
		if targetKey == "" {
			targetKey = customerFieldKey(mapping.DisplayName)
		}
		if seenTargets[targetKey] {
			return nil, 0, apierr.Invalid("MD_CUSTOMER_IMPORT_FIELD_MAPPING_DUPLICATE", "多个 Excel 列不能对应同一个客户字段")
		}
		seenTargets[targetKey] = true
		if mapping.FieldKey != "" {
			if _, err := s.q.GetCustomerFieldDefinitionByKey(ctx, store.GetCustomerFieldDefinitionByKeyParams{TenantID: tenantID, FieldKey: mapping.FieldKey}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, 0, apierr.Invalid("MD_CUSTOMER_IMPORT_FIELD_NOT_FOUND", "选择的客户字段不存在或已停用")
				}
				return nil, 0, err
			}
		}
	}
	verdicts := make([]CustomerImportVerdict, len(rows))
	seenCodes := map[string]bool{}
	seenNames := map[string]bool{}
	var ready int32
	for i, row := range rows {
		row.Code = strings.ToUpper(strings.TrimSpace(row.Code))
		row.Name = strings.TrimSpace(row.Name)
		row.OwnerName = strings.TrimSpace(row.OwnerName)
		if row.OwnerName != "" && row.OwnerEmployeeID == 0 && strings.EqualFold(row.OwnerName, strings.TrimSpace(operatorName)) {
			row.OwnerEmployeeID = operatorID
		}
		rows[i] = row
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
		if v.OK && (row.ContactName != "" || row.ContactEmail != "" || row.ContactPhone != "" || row.ContactMobile != "") {
			ci := CustomerContactInput{Name: defaultText(row.ContactName, "主要联系人"), Email: row.ContactEmail, Phone: row.ContactPhone, Mobile: row.ContactMobile}
			if err := ci.normalizeAndValidate(); err != nil {
				v.OK, v.Reason = false, "联系人姓名、邮箱或电话格式不正确"
			}
		}
		if v.OK && row.Address != "" {
			address := CustomerAddressInput{AddressType: "OFFICE", CountryCode: row.CountryCode, State: row.AddressState, PostalCode: row.PostalCode, AddressLine: row.Address, IsDefault: true}
			if err := address.normalizeAndValidateAddress(); err != nil {
				v.OK, v.Reason = false, err.Error()
			}
		}
		if v.OK && (row.OwnerName != "" || row.OwnerEmployeeID != 0) && (row.OwnerName == "" || row.OwnerEmployeeID <= 0) {
			v.OK, v.Reason = false, "分管人必须与系统中的唯一在职员工匹配"
		}
		if v.OK {
			in := CustomerInput{Name: row.Name, Currency: row.Currency, Website: row.Website, Timezone: row.Timezone, BusinessStatus: row.BusinessStatus, CreditStatus: "NORMAL"}
			if strings.TrimSpace(row.ContactName) != "" || strings.TrimSpace(row.ContactEmail) != "" || strings.TrimSpace(row.ContactPhone) != "" {
				in.Contacts = []ContactInput{{Name: defaultText(row.ContactName, "主要联系人"), Email: row.ContactEmail, Phone: row.ContactPhone}}
			}
			in.normalizeProfile()
			if err := in.validate(); err != nil {
				v.OK = false
				var businessErr *apierr.Error
				if errors.As(err, &businessErr) {
					v.Reason = businessErr.Msg
				} else {
					v.Reason = err.Error()
				}
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
		resolvedFields := make(map[string]store.CustomerFieldDefinition, len(mappings))
		for index, mapping := range mappings {
			var definition store.CustomerFieldDefinition
			var err error
			if mapping.FieldKey != "" {
				definition, err = q.GetCustomerFieldDefinitionByKey(ctx, store.GetCustomerFieldDefinitionByKeyParams{TenantID: tenantID, FieldKey: mapping.FieldKey})
				if err == nil {
					aliases := normalizeFieldAliases(append(append(definition.Aliases, mapping.Aliases...), mapping.DisplayName))
					definition, err = q.UpdateCustomerFieldDefinition(ctx, store.UpdateCustomerFieldDefinitionParams{DisplayName: definition.DisplayName, Aliases: aliases, SortOrder: definition.SortOrder, OperatorID: operatorID, TenantID: tenantID, FieldKey: definition.FieldKey})
				}
			} else {
				key := customerFieldKey(mapping.DisplayName)
				definition, err = q.GetCustomerFieldDefinitionByKey(ctx, store.GetCustomerFieldDefinitionByKeyParams{TenantID: tenantID, FieldKey: key})
				if errors.Is(err, pgx.ErrNoRows) {
					definition, err = q.CreateCustomerFieldDefinition(ctx, store.CreateCustomerFieldDefinitionParams{TenantID: tenantID, FieldKey: key, DisplayName: mapping.DisplayName, Aliases: normalizeFieldAliases(append(mapping.Aliases, mapping.DisplayName)), SortOrder: int32(index + 100), OperatorID: operatorID})
				} else if err == nil {
					aliases := normalizeFieldAliases(append(append(definition.Aliases, mapping.Aliases...), mapping.DisplayName))
					definition, err = q.UpdateCustomerFieldDefinition(ctx, store.UpdateCustomerFieldDefinitionParams{DisplayName: definition.DisplayName, Aliases: aliases, SortOrder: definition.SortOrder, OperatorID: operatorID, TenantID: tenantID, FieldKey: definition.FieldKey})
				}
			}
			if err != nil {
				return err
			}
			resolvedFields[mapping.SourceKey] = definition
		}
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
			businessStatus := strings.ToUpper(strings.TrimSpace(row.BusinessStatus))
			if businessStatus == "" {
				businessStatus = "PROSPECT"
			}
			c, err := q.CreateCustomer(ctx, store.CreateCustomerParams{
				TenantID: tenantID, Code: code, Name: strings.TrimSpace(row.Name), Country: "", CountryCode: countryCode,
				Address: strings.TrimSpace(row.Address), Currency: currency, PaymentTerm: row.PaymentTerm, Remark: row.Remark,
				ShortName: strings.TrimSpace(row.ShortName), EnglishName: strings.TrimSpace(row.EnglishName), CustomerType: customerType, Industry: strings.TrimSpace(row.Industry), Source: strings.ToUpper(strings.TrimSpace(row.Source)),
				Tags: splitImportTags(row.Tags), Website: strings.TrimSpace(row.Website), PrimaryLanguage: strings.TrimSpace(row.PrimaryLanguage), Timezone: strings.TrimSpace(row.Timezone), RegisteredName: strings.TrimSpace(row.RegisteredName),
				RegistrationNo: strings.TrimSpace(row.RegistrationNo), TaxID: strings.TrimSpace(row.TaxID), InvoiceTitle: strings.TrimSpace(row.InvoiceTitle), InvoiceTaxNo: strings.TrimSpace(row.InvoiceTaxNo), InvoiceRemark: strings.TrimSpace(row.InvoiceRemark),
				CreditLimitMinor: 0, CreditCurrency: currency, CreditStatus: "NORMAL",
				BusinessStatus: businessStatus, OperatorID: operatorID,
			})
			if err != nil {
				return fmt.Errorf("第 %d 行写入失败: %w", i+2, err)
			}
			if strings.TrimSpace(row.Address) != "" {
				_, err = q.CreateCustomerAddress(ctx, store.CreateCustomerAddressParams{
					TenantID: tenantID, CustomerID: c.ID, AddressType: "OFFICE", CountryCode: countryCode,
					State: strings.TrimSpace(row.AddressState), City: "", PostalCode: strings.TrimSpace(row.PostalCode),
					AddressLine: strings.TrimSpace(row.Address), IsDefault: true, SortOrder: 0, OperatorID: operatorID,
				})
				if err != nil {
					return fmt.Errorf("第 %d 行地址写入失败: %w", i+2, err)
				}
			}
			if grade := strings.ToUpper(strings.TrimSpace(row.CreditGrade)); grade != "" {
				if _, err = q.SetCustomerCreditGrade(ctx, store.SetCustomerCreditGradeParams{Grade: grade, TenantID: tenantID, ID: c.ID}); err != nil {
					return fmt.Errorf("第 %d 行客户等级写入失败: %w", i+2, err)
				}
			}
			if row.OwnerEmployeeID > 0 && strings.TrimSpace(row.OwnerName) != "" {
				_, err = q.CreateCustomerOwner(ctx, store.CreateCustomerOwnerParams{
					TenantID: tenantID, CustomerID: c.ID, EmployeeID: row.OwnerEmployeeID,
					EmployeeName: strings.TrimSpace(row.OwnerName), ResponsibilityCode: "SALES",
					IsPrimary: true, OperatorID: operatorID,
				})
				if err != nil {
					return fmt.Errorf("第 %d 行负责人写入失败: %w", i+2, err)
				}
			}
			for sourceKey, value := range row.CustomFields {
				definition, ok := resolvedFields[sourceKey]
				if !ok || strings.TrimSpace(value) == "" {
					continue
				}
				if err := q.UpsertCustomerCustomFieldValue(ctx, store.UpsertCustomerCustomFieldValueParams{TenantID: tenantID, CustomerID: c.ID, FieldID: definition.ID, Value: strings.TrimSpace(value), OperatorID: operatorID}); err != nil {
					return err
				}
			}
			if strings.TrimSpace(row.ContactName) != "" || strings.TrimSpace(row.ContactEmail) != "" || strings.TrimSpace(row.ContactPhone) != "" || strings.TrimSpace(row.ContactMobile) != "" {
				_, err = q.CreateCustomerContact(ctx, store.CreateCustomerContactParams{TenantID: tenantID, CustomerID: c.ID,
					Name: defaultText(strings.TrimSpace(row.ContactName), "主要联系人"), Email: strings.TrimSpace(row.ContactEmail),
					Phone: strings.TrimSpace(row.ContactPhone), IsPrimary: true, Department: "", Title: "", Mobile: strings.TrimSpace(row.ContactMobile),
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

func splitImportTags(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '；' })
	return normalizeFieldAliases(parts)
}
