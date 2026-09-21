package app

import (
	"context"

	"github.com/sgao19/erp-go/pkg/apierr"
)

type customerVisibility interface {
	VisibleIDs(context.Context, int64) ([]int64, bool, error)
}

func (s *Service) visibleCustomers(ctx context.Context, actor int64) ([]int64, bool, error) {
	if access, ok := s.customers.(customerVisibility); ok {
		return access.VisibleIDs(ctx, actor)
	}
	// Legacy in-process tests supply snapshots; production supplies the RPC adapter.
	return nil, true, nil
}
func (s *Service) checkCustomerAccess(ctx context.Context, customerID int64) error {
	if _, ok := s.customers.(customerVisibility); !ok || customerID == 0 {
		return nil
	}
	_, err := s.customers.Get(ctx, customerID)
	return err
}

func (s *Service) validateOfferCustomer(ctx context.Context, body *OfferBody) error {
	if _, ok := s.customers.(customerVisibility); !ok {
		return nil
	}
	if offerID(body.CustomerID) <= 0 {
		return apierr.Invalid("OFFER_CUSTOMER_REQUIRED", "请从有权访问的客户列表中选择客户")
	}
	customer, err := s.customers.Get(ctx, offerID(body.CustomerID))
	if err != nil {
		return err
	}
	if customer.Status != "ACTIVE" {
		return apierr.Invalid("MD_CUSTOMER_INACTIVE", "客户已停用")
	}
	body.Customer = customerDisplayName(customer)
	return nil
}
