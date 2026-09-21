package app

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/sgao19/erp-go/services/masterdata/internal/store"
)

func resolveImportFields(ctx context.Context, q *store.Queries, tenantID, operatorID int64, mappings []CustomerImportFieldMapping) (map[string]store.CustomerFieldDefinition, error) {
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
			return nil, err
		}
		resolvedFields[mapping.SourceKey] = definition
	}
	return resolvedFields, nil
}
