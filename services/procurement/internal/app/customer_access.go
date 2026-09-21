package app

import (
	"context"
	"strings"

	"github.com/sgao19/erp-go/pkg/apierr"
)

// CustomerAccess delegates current ownership to masterdata; inquiry snapshots
// never grant customer access and owner changes never rewrite past documents.
type CustomerAccess interface {
	Check(context.Context, int64) (string, error)
	VisibleIDs(context.Context, int64) ([]int64, bool, error)
}

func (s *Service) checkInquiryCustomer(ctx context.Context, customerID int64) error {
	if s.customers == nil || customerID == 0 {
		return nil
	}
	_, err := s.customers.Check(ctx, customerID)
	return err
}
func (s *Service) validateInquiryCustomer(ctx context.Context, body *InquiryBody) error {
	if s.customers == nil {
		return nil
	}
	if inquiryID(body.CustomerID) <= 0 {
		if strings.TrimSpace(body.Customer) != "" {
			return apierr.Invalid("INQUIRY_CUSTOMER_REQUIRED", "请从有权访问的客户列表中选择客户")
		}
		return nil
	}
	name, err := s.customers.Check(ctx, inquiryID(body.CustomerID))
	if err != nil {
		return err
	}
	body.Customer = name
	return nil
}
