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

// CustomerNameResolver is optional so in-process fixtures and older adapters
// keep working. Production uses it to repair legacy inquiries which saved a
// customer name before customer_id became mandatory.
type CustomerNameResolver interface {
	ResolveByName(context.Context, string) (int64, string, error)
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

// linkInquiryCustomerByName repairs a legacy inquiry that saved a customer
// name before customer_id became mandatory, and returns the customer it is
// now linked to. Zero means it could not be linked and nothing changed.
func (s *Service) linkInquiryCustomerByName(ctx context.Context, tenant, id int64, name string) (int64, string, error) {
	resolver, ok := s.customers.(CustomerNameResolver)
	if !ok {
		return 0, "", nil
	}
	resolvedID, resolvedName, err := resolver.ResolveByName(ctx, name)
	if err != nil || resolvedID <= 0 {
		return 0, "", err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sourcing_cases SET customer_id=$3,customer_name=$4 WHERE tenant_id=$1 AND id=$2 AND customer_id=0`, tenant, id, resolvedID, resolvedName)
	if err != nil {
		return 0, "", err
	}
	if tag.RowsAffected() == 1 {
		return resolvedID, resolvedName, nil
	}
	// Somebody else linked it first; theirs is the link that stands.
	var currentID int64
	var currentName string
	if err := s.pool.QueryRow(ctx, `SELECT customer_id,customer_name FROM sourcing_cases WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&currentID, &currentName); err != nil {
		return 0, "", err
	}
	return currentID, currentName, nil
}

// CustomerNameBatch is optional like CustomerNameResolver. Production answers
// a whole list page in one masterdata call; fixtures that only implement
// Check are asked once per distinct customer instead.
type CustomerNameBatch interface {
	// CurrentNames returns each customer the caller can see; the ones it
	// cannot see, or that do not exist, are absent.
	CurrentNames(context.Context, []int64) (map[int64]CustomerCurrentName, error)
}

// CustomerCurrentName is a customer as a list shows it.
type CustomerCurrentName struct {
	Name   string
	Active bool
}

// currentCustomerNames is Check for many customers at once, with Check's
// outcomes: the current display name, "已停用" for an inactive customer and
// not-found for one the caller cannot see. The first failing id, in the order
// given, decides the error.
func (s *Service) currentCustomerNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	names := map[int64]string{}
	if len(ids) == 0 {
		return names, nil
	}
	batch, ok := s.customers.(CustomerNameBatch)
	if !ok {
		for _, id := range ids {
			if _, done := names[id]; done {
				continue
			}
			name, err := s.customers.Check(ctx, id)
			if err != nil {
				return nil, err
			}
			names[id] = name
		}
		return names, nil
	}
	found, err := batch.CurrentNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		c, ok := found[id]
		if !ok {
			return nil, apierr.NotFound("MD_CUSTOMER_NOT_FOUND", "客户不存在或无权访问")
		}
		if !c.Active {
			return nil, apierr.Invalid("MD_CUSTOMER_INACTIVE", "客户已停用")
		}
		names[id] = c.Name
	}
	return names, nil
}
