package app

import (
	"context"
	"strings"

	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

// DuplicateCandidate 是各类基础资料共用的重复预检结果。
// 预检只负责提示可能重复，不代替编码、UN/LOCODE 等数据库唯一约束。
type DuplicateCandidate struct {
	ID           int64
	Code         string
	Name         string
	TaxID        string
	Email        string
	SupplierID   int64
	SupplierName string
	Address      string
	MatchFields  []string
}

func sameText(left, right string) bool {
	return strings.TrimSpace(right) != "" && strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func nameMatches(candidate, input string) []string {
	if sameText(candidate, input) {
		return []string{"NAME"}
	}
	if strings.TrimSpace(input) != "" {
		return []string{"SIMILAR_NAME"}
	}
	return nil
}

// CheckCustomerDuplicates 按名称、税号和联系人邮箱给出候选资料。
func (s *Service) CheckCustomerDuplicates(ctx context.Context, tenantID int64, name, taxID, email string, excludeID int64) ([]DuplicateCandidate, error) {
	name, taxID, email = strings.TrimSpace(name), strings.TrimSpace(taxID), strings.TrimSpace(email)
	if name == "" && taxID == "" && email == "" {
		return nil, nil
	}
	rows, err := s.q.CustomerDuplicateCandidates(ctx, store.CustomerDuplicateCandidatesParams{
		TenantID: tenantID, Name: name, TaxID: taxID, Email: email, ExcludeID: excludeID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]DuplicateCandidate, len(rows))
	for i, row := range rows {
		fields := nameMatches(row.Name, name)
		if sameText(row.TaxID, taxID) {
			fields = append(fields, "TAX_ID")
		}
		if sameText(row.Email, email) {
			fields = append(fields, "EMAIL")
		}
		out[i] = DuplicateCandidate{ID: row.ID, Code: row.Code, Name: row.Name, TaxID: row.TaxID, Email: row.Email, MatchFields: fields}
	}
	return out, nil
}

// CheckSupplierDuplicates 对供应商名称、税号和联系邮箱做非阻断式预检。
func (s *Service) CheckSupplierDuplicates(ctx context.Context, tenantID int64, name, taxID, email string, excludeID int64) ([]DuplicateCandidate, error) {
	name, taxID, email = strings.TrimSpace(name), strings.TrimSpace(taxID), strings.TrimSpace(email)
	if name == "" && taxID == "" && email == "" {
		return nil, nil
	}
	rows, err := s.q.SupplierDuplicateCandidates(ctx, store.SupplierDuplicateCandidatesParams{
		TenantID: tenantID, Name: name, TaxID: taxID, Email: email, ExcludeID: excludeID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]DuplicateCandidate, len(rows))
	for i, row := range rows {
		fields := nameMatches(row.Name, name)
		if sameText(row.TaxID, taxID) {
			fields = append(fields, "TAX_ID")
		}
		if sameText(row.ContactEmail, email) {
			fields = append(fields, "EMAIL")
		}
		out[i] = DuplicateCandidate{ID: row.ID, Code: row.Code, Name: row.Name, TaxID: row.TaxID, Email: row.ContactEmail, MatchFields: fields}
	}
	return out, nil
}

// CheckFactoryDuplicates 只在同一供应商下比较工厂名称和地址。
func (s *Service) CheckFactoryDuplicates(ctx context.Context, tenantID, supplierID int64, name, address string, excludeID int64) ([]DuplicateCandidate, error) {
	name, address = strings.TrimSpace(name), strings.TrimSpace(address)
	if supplierID == 0 || name == "" {
		return nil, nil
	}
	rows, err := s.q.FactoryDuplicateCandidates(ctx, store.FactoryDuplicateCandidatesParams{
		TenantID: tenantID, SupplierID: supplierID, Name: name, Address: address, ExcludeID: excludeID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]DuplicateCandidate, len(rows))
	for i, row := range rows {
		fields := nameMatches(row.Name, name)
		if sameText(row.Address, address) {
			fields = append(fields, "ADDRESS")
		}
		out[i] = DuplicateCandidate{ID: row.ID, Code: row.Code, Name: row.Name, SupplierID: row.SupplierID, SupplierName: row.SupplierName, Address: row.Address, MatchFields: fields}
	}
	return out, nil
}
