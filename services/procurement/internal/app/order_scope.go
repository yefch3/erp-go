package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// Who may see which purchase documents.
//
// Orders carry the numbers a trading company actually guards — supplier,
// price, payment terms — so they get their own scope module rather than
// riding on procurement_sourcing: pre-sale inquiry work and committed spend
// are different sensitivities, often held by different people.
//
// The ownership dimension is the buyer for orders and the clerk who entered
// it for invoices.
//
// Requirements got their owner in the 2026-08-22 product decision: a
// requirement split from a contract belongs to the contract's salesperson, a
// manual one to whoever raised it. SELF for a salesperson means "my own
// customers' purchase needs"; buyers work the whole queue and are widened to
// ALL on the roles page. owner_id = 0 marks pre-decision history — visible
// only to ALL, never leaked into a per-person view.
const orderScopeModule = "procurement_order"
const requirementScopeModule = "procurement_requirement"

func (s *Service) visibleRequirementsTo(ctx context.Context, op Operator) (Visibility, error) {
	if s.scopes == nil {
		return Visibility{All: true, ScopeType: "ALL"}, nil
	}
	return s.scopes.VisibleEmployees(ctx, op.ID, requirementScopeModule)
}

// AuthorizeRequirement refuses access to a requirement outside the caller's
// range. Not-found rather than forbidden, same as everywhere else: confirming
// that another salesperson's deal exists is itself information.
func (s *Service) AuthorizeRequirement(ctx context.Context, tenantID, requirementID int64, op Operator) error {
	var ownerID int64
	err := s.pool.QueryRow(ctx,
		`SELECT owner_id FROM purchase_requirements WHERE tenant_id=$1 AND id=$2`,
		tenantID, requirementID).Scan(&ownerID)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("PR_REQUIREMENT_NOT_FOUND", "采购需求不存在")
	}
	if err != nil {
		return err
	}
	visible, err := s.visibleRequirementsTo(ctx, op)
	if err != nil {
		return err
	}
	if !ownerVisible(visible, ownerID) {
		return apierr.NotFound("PR_REQUIREMENT_NOT_FOUND", "采购需求不存在")
	}
	return nil
}

func (s *Service) visibleOrdersTo(ctx context.Context, op Operator) (Visibility, error) {
	// Production always supplies IAM; the fallback keeps isolated app tests
	// and internal consumers that do not represent a user request working.
	if s.scopes == nil {
		return Visibility{All: true, ScopeType: "ALL"}, nil
	}
	return s.scopes.VisibleEmployees(ctx, op.ID, orderScopeModule)
}

func ownerVisible(v Visibility, ownerID int64) bool {
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

// AuthorizeOrder refuses access to an order outside the caller's range.
//
// Not-found rather than forbidden, same as the sourcing scope: confirming
// that another buyer's order exists is itself information.
func (s *Service) AuthorizeOrder(ctx context.Context, tenantID, orderID int64, op Operator) error {
	var buyerID int64
	err := s.pool.QueryRow(ctx,
		`SELECT buyer_id FROM purchase_orders WHERE tenant_id=$1 AND id=$2`,
		tenantID, orderID).Scan(&buyerID)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
	}
	if err != nil {
		return err
	}
	visible, err := s.visibleOrdersTo(ctx, op)
	if err != nil {
		return err
	}
	if !ownerVisible(visible, buyerID) {
		return apierr.NotFound("PO_ORDER_NOT_FOUND", "采购单不存在")
	}
	return nil
}

// AuthorizeSupplierInvoice does the same for the third leg of the match.
// Owner is whoever entered the paper.
func (s *Service) AuthorizeSupplierInvoice(ctx context.Context, tenantID, invoiceID int64, op Operator) error {
	var createdBy int64
	err := s.pool.QueryRow(ctx,
		`SELECT created_by_id FROM supplier_invoices WHERE tenant_id=$1 AND id=$2`,
		tenantID, invoiceID).Scan(&createdBy)
	if err == pgx.ErrNoRows {
		return apierr.NotFound("INV_NOT_FOUND", "发票不存在")
	}
	if err != nil {
		return err
	}
	visible, err := s.visibleOrdersTo(ctx, op)
	if err != nil {
		return err
	}
	if !ownerVisible(visible, createdBy) {
		return apierr.NotFound("INV_NOT_FOUND", "发票不存在")
	}
	return nil
}
