package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

type CustomerImportFieldMapping struct {
	SourceKey, FieldKey, DisplayName string
	Aliases                          []string
}

func (s *Service) ListCustomerFields(ctx context.Context, tenantID int64) ([]store.CustomerFieldDefinition, error) {
	return s.q.ListCustomerFieldDefinitions(ctx, tenantID)
}

func (s *Service) ListCustomerCustomFieldValues(ctx context.Context, tenantID, customerID int64) ([]store.ListCustomerCustomFieldValuesRow, error) {
	return s.q.ListCustomerCustomFieldValues(ctx, store.ListCustomerCustomFieldValuesParams{TenantID: tenantID, CustomerID: customerID})
}

func (s *Service) SaveCustomerField(ctx context.Context, tenantID int64, fieldKey, displayName string, aliases []string, sortOrder int32, operatorID int64) (store.CustomerFieldDefinition, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return store.CustomerFieldDefinition{}, apierr.Invalid("MD_CUSTOMER_FIELD_NAME_REQUIRED", "字段显示名称必填")
	}
	aliases = normalizeFieldAliases(append(aliases, displayName))
	fieldKey = strings.TrimSpace(fieldKey)
	if fieldKey == "" {
		fieldKey = customerFieldKey(displayName)
		if current, err := s.q.GetCustomerFieldDefinitionByKey(ctx, store.GetCustomerFieldDefinitionByKeyParams{TenantID: tenantID, FieldKey: fieldKey}); err == nil {
			return current, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return store.CustomerFieldDefinition{}, err
		}
		return s.q.CreateCustomerFieldDefinition(ctx, store.CreateCustomerFieldDefinitionParams{
			TenantID: tenantID, FieldKey: fieldKey, DisplayName: displayName, Aliases: aliases,
			SortOrder: sortOrder, OperatorID: operatorID,
		})
	}
	if !strings.HasPrefix(fieldKey, "custom_") {
		return store.CustomerFieldDefinition{}, apierr.Invalid("MD_CUSTOMER_FIELD_KEY_INVALID", "字段标识无效")
	}
	return s.q.UpdateCustomerFieldDefinition(ctx, store.UpdateCustomerFieldDefinitionParams{
		DisplayName: displayName, Aliases: aliases, SortOrder: sortOrder,
		OperatorID: operatorID, TenantID: tenantID, FieldKey: fieldKey,
	})
}

func customerFieldKey(displayName string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(displayName)), " "))
	sum := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("custom_%x", sum[:8])
}

func normalizeFieldAliases(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}
