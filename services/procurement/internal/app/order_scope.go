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
// it for invoices. Purchase requirements are deliberately NOT scoped here:
// they have no owner column, and inventing one (the contract's salesperson?
// the future buyer?) is a product decision, not a refactor. Recorded in
// docs/开发计划.md rather than half-guessed.
const orderScopeModule = "procurement_order"

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
