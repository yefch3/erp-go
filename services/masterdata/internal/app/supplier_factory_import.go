package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/pgdb"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type SupplierImportRow struct {
	RowNumber                                                                int32
	Code, NameZh, NameEn, ShortName, CountryCode, Currency                   string
	PaymentTerm, TaxID, RegisteredAddress, Address, Remark                   string
	ContactName, ContactDepartment, ContactTitle, ContactPhone, ContactEmail string
	ContactRemark                                                            string
	ContactIsPrimary                                                         bool
	BusinessTypes                                                            []string
}

type MasterDataImportIssue struct {
	RowNumber int32
	Code      string
	Name      string
	Message   string
}

func importRowNumber(value int32, index int) int32 {
	if value > 0 {
		return value
	}
	return int32(index + 2)
}

// ImportSuppliers 先完整预检整批数据；只有确认导入且全部行通过时，才在一个事务中写入。
func (s *Service) ImportSuppliers(ctx context.Context, tenantID int64, rows []SupplierImportRow, confirm bool, operatorID int64, operatorName string) (int32, int32, []MasterDataImportIssue, error) {
	type supplierImportGroup struct {
		row      SupplierImportRow
		input    SupplierInput
		contacts []PartyContactInput
	}
	groups := make([]supplierImportGroup, 0, len(rows))
	groupByCode := make(map[string]int, len(rows))
	issues := make([]MasterDataImportIssue, 0)
	for i, row := range rows {
		code := strings.ToUpper(strings.TrimSpace(row.Code))
		name := defaultText(row.NameZh, row.NameEn)
		issue := MasterDataImportIssue{RowNumber: importRowNumber(row.RowNumber, i), Code: code, Name: name}
		if code == "" {
			issue.Message = "供应商编码必填"
		}
		groupIndex, grouped := groupByCode[code]
		if issue.Message == "" && !grouped {
			exists, err := s.q.SupplierCodeExists(ctx, store.SupplierCodeExistsParams{TenantID: tenantID, Code: code})
			if err != nil {
				return 0, 0, nil, err
			}
			if exists {
				issue.Message = "供应商编码已存在"
			}
		}
		in := SupplierInput{Code: code, NameZh: row.NameZh, NameEn: row.NameEn, ShortName: row.ShortName,
			CountryCode: row.CountryCode, Address: row.Address, Currency: row.Currency, PaymentTerm: row.PaymentTerm,
			BusinessTypes: row.BusinessTypes, ContactName: row.ContactName, ContactPhone: row.ContactPhone,
			ContactEmail: row.ContactEmail, TaxID: row.TaxID, RegisteredAddress: row.RegisteredAddress,
			Remark: row.Remark, OperatorID: operatorID, OperatorName: operatorName}
		if issue.Message == "" && !grouped {
			if err := normalizeSupplierInput(&in); err != nil {
				issue.Message = err.Error()
			}
		}
		contact := PartyContactInput{Name: row.ContactName, Department: row.ContactDepartment, Title: row.ContactTitle,
			Phone: row.ContactPhone, Email: row.ContactEmail, IsPrimary: row.ContactIsPrimary, Remark: row.ContactRemark,
			OperatorID: operatorID, OperatorName: operatorName}
		hasContactData := strings.TrimSpace(contact.Name+contact.Department+contact.Title+contact.Phone+contact.Email+contact.Remark) != "" || contact.IsPrimary
		if issue.Message == "" && hasContactData {
			if err := normalizePartyContact(&contact); err != nil {
				issue.Message = err.Error()
			}
		}
		if issue.Message != "" {
			issues = append(issues, issue)
			continue
		}
		if !grouped {
			groupIndex = len(groups)
			groupByCode[code] = groupIndex
			groups = append(groups, supplierImportGroup{row: row, input: in})
		} else {
			base := groups[groupIndex].input
			if (strings.TrimSpace(row.NameZh) != "" && strings.TrimSpace(row.NameZh) != base.NameZh) ||
				(strings.TrimSpace(row.NameEn) != "" && strings.TrimSpace(row.NameEn) != base.NameEn) {
				issues = append(issues, MasterDataImportIssue{RowNumber: issue.RowNumber, Code: code, Name: name, Message: "同一供应商编码的名称必须一致"})
				continue
			}
		}
		if hasContactData {
			groups[groupIndex].contacts = append(groups[groupIndex].contacts, contact)
		}
	}
	ready := int32(len(groups))
	if !confirm || len(issues) > 0 || len(groups) == 0 {
		return ready, 0, issues, nil
	}
	err := pgdb.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		for _, group := range groups {
			in := group.input
			created, err := q.CreateSupplier(ctx, store.CreateSupplierParams{TenantID: tenantID, Code: in.Code, Name: in.Name,
				NameZh: in.NameZh, NameEn: in.NameEn, ShortName: in.ShortName, Country: in.Country, CountryCode: in.CountryCode,
				Address: in.Address, RegisteredAddress: in.RegisteredAddress, TaxID: in.TaxID, Currency: in.Currency,
				PaymentTerm: in.PaymentTerm, BusinessTypes: in.BusinessTypes, ContactName: in.ContactName,
				ContactPhone: in.ContactPhone, ContactEmail: in.ContactEmail, Remark: in.Remark, OperatorID: operatorID})
			if err != nil {
				return fmt.Errorf("第 %d 行写入失败: %w", group.row.RowNumber, err)
			}
			if operatorID > 0 {
				if _, err := q.CreateSupplierOwner(ctx, store.CreateSupplierOwnerParams{
					TenantID: tenantID, SupplierID: created.ID, EmployeeID: operatorID,
					EmployeeName: supplierOwnerDisplayName(operatorID, operatorName), ResponsibilityCode: "PROCUREMENT",
					IsPrimary: true, OperatorID: operatorID,
				}); err != nil {
					return fmt.Errorf("供应商 %s 负责人写入失败: %w", in.Code, err)
				}
			}
			if err := recordSupplierChange(ctx, q, tenantID, created.ID, "IMPORT", "PROFILE", "批量导入供应商", nil, created, operatorID, operatorName); err != nil {
				return err
			}
			primaryCreated := false
			for _, contact := range group.contacts {
				isPrimary := contact.IsPrimary && !primaryCreated
				createdContact, err := q.CreateSupplierContact(ctx, store.CreateSupplierContactParams{TenantID: tenantID, SupplierID: created.ID,
					Name: contact.Name, Department: contact.Department, Title: contact.Title, Phone: contact.Phone, Email: contact.Email,
					IsPrimary: isPrimary, Remark: contact.Remark, OperatorID: operatorID})
				if err != nil {
					return fmt.Errorf("供应商 %s 联系人写入失败: %w", in.Code, err)
				}
				primaryCreated = primaryCreated || isPrimary
				if err := recordSupplierChange(ctx, q, tenantID, created.ID, "IMPORT", "CONTACT", "批量导入联系人："+createdContact.Name, nil, createdContact, operatorID, operatorName); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return ready, 0, issues, err
	}
	return ready, int32(len(groups)), issues, nil
}
