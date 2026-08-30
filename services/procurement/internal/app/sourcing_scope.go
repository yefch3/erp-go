package app

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
	"github.com/sgao19/erp-go/services/procurement/internal/store"
)

// This scope is separate from procurement execution: company-wide purchase
// requirements must not silently reveal every salesperson's pre-sale work.
const sourcingScopeModule = "procurement_sourcing"

func (s *Service) visibleSourcingTo(ctx context.Context, op Operator) (Visibility, error) {
	// Production always supplies IAM. The fallback keeps isolated app tests
	// and internal consumers that do not represent a user request working.
	if s.scopes == nil {
		return Visibility{All: true, ScopeType: "ALL"}, nil
	}
	return s.scopes.VisibleEmployees(ctx, op.ID, sourcingScopeModule)
}

func allowedSourcingOwner(v Visibility, ownerID int64) bool {
	if v.All {
		return true
	}
	for _, id := range v.EmployeeIDs {
		if id == ownerID {
			return true
		}
	}
	return false
}

// AuthorizeSourcingCase returns not-found outside the caller's range because
// confirming that another employee's inquiry exists is itself information.
func (s *Service) AuthorizeSourcingCase(ctx context.Context, tenantID, caseID int64, op Operator) error {
	head, err := s.q.GetSourcingCase(ctx, store.GetSourcingCaseParams{TenantID: tenantID, ID: caseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	if err != nil {
		return err
	}
	visible, err := s.visibleSourcingTo(ctx, op)
	if err != nil {
		return err
	}
	if !allowedSourcingOwner(visible, head.OwnerID) {
		return apierr.NotFound("SC_CASE_NOT_FOUND", "询价案件不存在")
	}
	return nil
}

func (s *Service) AuthorizeFactoryRFQ(ctx context.Context, tenantID, rfqID int64, op Operator) error {
	caseID, err := s.q.FactoryRFQCase(ctx, store.FactoryRFQCaseParams{TenantID: tenantID, ID: rfqID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("SC_RFQ_NOT_FOUND", "工厂询价单不存在")
	}
	if err != nil {
		return err
	}
	return s.AuthorizeSourcingCase(ctx, tenantID, caseID, op)
}

func (s *Service) AuthorizeCostScenario(ctx context.Context, tenantID, scenarioID int64, op Operator) error {
	scenario, err := s.q.GetCostScenario(ctx, store.GetCostScenarioParams{TenantID: tenantID, ID: scenarioID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("SC_COST_NOT_FOUND", "成本方案不存在")
	}
	if err != nil {
		return err
	}
	return s.AuthorizeSourcingCase(ctx, tenantID, scenario.CaseID, op)
}

func (s *Service) AuthorizeProcurementPlan(ctx context.Context, tenantID, planID int64, op Operator) error {
	plan, err := s.q.GetProcurementPlan(ctx, store.GetProcurementPlanParams{TenantID: tenantID, ID: planID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apierr.NotFound("SC_PLAN_NOT_FOUND", "统一采购方案不存在")
	}
	if err != nil {
		return err
	}
	return s.AuthorizeSourcingCase(ctx, tenantID, plan.CaseID, op)
}
