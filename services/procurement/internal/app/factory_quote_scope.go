package app

import (
	"context"
	"slices"
)

const factoryQuoteScopeModule = "procurement_factory_quote"

func (s *Service) CanViewAllFactoryQuotes(ctx context.Context, op Operator) (bool, error) {
	if s.scopes == nil {
		return false, nil
	}
	visibility, err := s.scopes.VisibleEmployees(ctx, op.ID, factoryQuoteScopeModule)
	return visibility.All, err
}

func (s *Service) canManageAllFactoryQuotes(ctx context.Context, op Operator) (bool, error) {
	manager, err := s.CanManageExecutionQuotes(ctx, op)
	if err != nil || !manager {
		return false, err
	}
	return s.CanViewAllFactoryQuotes(ctx, op)
}

func (s *Service) ListVisibleExecutionSupplierQuotes(ctx context.Context, tenantID, requirementID int64, op Operator) ([]ExecutionSupplierQuote, error) {
	all, err := s.CanViewAllFactoryQuotes(ctx, op)
	if err != nil {
		return nil, err
	}
	quotes, err := s.ListExecutionSupplierQuotes(ctx, tenantID, requirementID)
	if err != nil || all {
		return quotes, err
	}
	return slices.DeleteFunc(quotes, func(q ExecutionSupplierQuote) bool {
		return q.CreatedByID != op.ID
	}), nil
}
